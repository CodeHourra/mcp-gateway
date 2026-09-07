"""Loopback-only MCP endpoints for native authentication form acceptance."""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import signal
import threading
import time
import uuid


EXPECTED = {
    "/bearer/mcp": {"Authorization": "Bearer gateway-native-bearer-fixture"},
    "/api-key/mcp": {"X-API-Key": "gateway-native-api-fixture"},
    "/headers/mcp": {"X-API-Key": "gateway-native-api-fixture", "X-Tenant": "native-fixture-team", "X-Trace-Label": "native-form-proof"},
}


class Handler(BaseHTTPRequestHandler):
    sessions = {}
    lock = threading.Lock()
    evidence = None

    def log_message(self, *args):
        pass

    def record(self, method, matches, success, details=None):
        authorization = self.headers.get("Authorization", "")
        event = {"at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "path": self.path,
            "http_method": self.command, "rpc_method": method, "expected_headers_matched": matches,
            "authorization_shape": {"present": bool(authorization), "length": len(authorization),
                "contains_keyring_reference": "${keyring:" in authorization,
                "bearer_prefix": authorization.startswith("Bearer "),
                "double_bearer_prefix": authorization.startswith("Bearer Bearer ")},
            "authentication_accepted": success, **(details or {})}
        with self.lock:
            with self.evidence.open("a") as stream:
                stream.write(json.dumps(event) + "\n")

    def send(self, status, value=None, session=None):
        body = b"" if value is None else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        if session:
            self.send_header("Mcp-Session-Id", session)
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.send(405, {"error": "Use MCP POST on a configured fixture endpoint"})

    def do_POST(self):
        expected = EXPECTED.get(self.path)
        if expected is None:
            self.send(404, {"error": "Unknown fixture endpoint"})
            return
        message = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
        method = message.get("method")
        matches = {name: self.headers.get(name) == value for name, value in expected.items()}
        accepted = all(matches.values())
        self.record(method, matches, accepted)
        if not accepted:
            self.send(401, {"error": "Expected controlled test headers were not received"})
            return
        session = self.headers.get("Mcp-Session-Id")
        if method == "initialize":
            session = uuid.uuid4().hex
            self.sessions[session] = {"calls": 0, "path": self.path}
            result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                "serverInfo": {"name": "native-auth-fixture", "version": "1"}}
        elif method == "tools/list":
            result = {"tools": [{"name": "check_auth", "description": "Verify the configured test authentication headers reached this local fixture.",
                "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"], "additionalProperties": False},
                "annotations": {"readOnlyHint": True, "destructiveHint": False, "openWorldHint": False}}]}
        elif method == "tools/call":
            state = self.sessions.get(session, {"calls": 0})
            state["calls"] += 1
            value = {"fixture": self.path, "authentication_accepted": True,
                "expected_headers_matched": matches, "counter": state["calls"],
                "text": message.get("params", {}).get("arguments", {}).get("text", "")}
            result = {"content": [{"type": "text", "text": json.dumps(value)}], "structuredContent": value, "isError": False}
        else:
            result = {}
        if "id" not in message:
            self.send(202)
        else:
            self.send(200, {"jsonrpc": "2.0", "id": message["id"], "result": result}, session)

    def do_DELETE(self):
        self.sessions.pop(self.headers.get("Mcp-Session-Id"), None)
        self.send(200, {})


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", required=True)
    parser.add_argument("--port", type=int, default=0)
    args = parser.parse_args()
    directory = Path(args.directory).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    directory.chmod(0o700)
    Handler.evidence = directory / "headers.jsonl"
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    metadata = {"pid": os.getpid(), "port": server.server_port,
        "endpoints": {name: {"url": f"http://127.0.0.1:{server.server_port}{name}", "expected": headers} for name, headers in EXPECTED.items()},
        "evidence": str(Handler.evidence)}
    (directory / "native-auth-fixtures.json").write_text(json.dumps(metadata, indent=2) + "\n")
    print(json.dumps(metadata), flush=True)
    signal.signal(signal.SIGTERM, lambda *_: threading.Thread(target=server.shutdown).start())
    try:
        server.serve_forever()
    finally:
        server.server_close()
