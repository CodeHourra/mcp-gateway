"""Use a temporary client token to probe only the isolated native acceptance app."""
import argparse
import json
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import uuid

from wails_mcp import call as wails_call


def run(profile, management, debug):
    if profile.name != "native-profile" or profile.parent.name != ".cache":
        raise ValueError("Use only the explicitly isolated .cache/native-profile")
    for endpoint in (management, debug):
        parsed = urllib.parse.urlparse(endpoint)
        if parsed.scheme != "http" or parsed.hostname != "127.0.0.1":
            raise ValueError("Acceptance endpoints must be IPv4 loopback HTTP")
    # This build is already known to persist the management key in its test
    # profile. Never return the key, temporary token, or session identifier.
    admin = json.loads((profile / "core.json").read_text()).get("api_key")
    if not admin:
        raise ValueError("No test-profile management key available; no keychain fallback")
    report = {"profile": str(profile), "management": management, "steps": []}
    name = "native-acceptance-" + uuid.uuid4().hex[:10]
    token = None
    session = {}
    closed = False

    def request(path, data=None, *, method="POST", client=False):
        headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
        if client:
            headers["Authorization"] = "Bearer " + token
            headers.update(session)
        else:
            headers["X-API-Key"] = admin
        req = urllib.request.Request(management + path, method=method, headers=headers,
                                     data=None if data is None else json.dumps(data).encode())
        try:
            response = urllib.request.urlopen(req, timeout=15)
        except urllib.error.HTTPError as exc:
            response = exc
        raw = response.read().decode()
        if raw.startswith("event:"):
            raw = next(line[6:] for line in raw.splitlines() if line.startswith("data: "))
        parsed = json.loads(raw) if raw else None
        return response.status, parsed, dict(response.headers)

    def rpc(method, params, ident):
        message = {"jsonrpc": "2.0", "method": method, "params": params}
        if ident is not None:
            message["id"] = ident
        status, body, headers = request("/mcp/call", message, client=True)
        if status not in (200, 202) or (isinstance(body, dict) and body.get("error")):
            raise RuntimeError(f"MCP {method} failed with status {status}: {body}")
        return body, headers

    try:
        status, created, _ = request("/api/v1/tokens", {"name": name,
            "allowed_servers": ["native-echo"], "permissions": ["read"], "expires_in": "1h"})
        token = (created or {}).get("data", {}).get("token")
        if status not in (200, 201) or not token:
            raise RuntimeError(f"Temporary token creation failed with HTTP {status}")
        body, headers = rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
            "clientInfo": {"name": "native-acceptance-http-client", "version": "1"}}, 1)
        session = {"Mcp-Session-Id": headers.get("Mcp-Session-Id", ""), "MCP-Protocol-Version": "2025-11-25"}
        report["steps"].append({"name": "initialize", "response": body})
        rpc("notifications/initialized", {}, None)
        listed, _ = rpc("tools/list", {}, 2)
        report["steps"].append({"name": "tools/list", "response": listed})
        retrieved, _ = rpc("tools/call", {"name": "retrieve_tools", "arguments": {"query": "echo text"}}, 3)
        report["steps"].append({"name": "retrieve_tools", "response": retrieved})
        described, _ = rpc("tools/call", {"name": "describe_tool", "arguments": {"tool_ids": ["native-echo:echo"]}}, 4)
        report["steps"].append({"name": "describe_tool", "response": described})

        report["close"] = wails_call(debug, "window_control", {"window": "manager", "action": "close"})
        closed = True
        report["appAfterClose"] = wails_call(debug, "app_info", {})
        body, _ = rpc("tools/call", {"name": "call_tool_read", "arguments": {
            "name": "native-echo:echo", "args": {"text": "native-background-acceptance"}}}, 5)
        report["steps"].append({"name": "call_after_window_close", "response": body})
        result = (body or {}).get("result", {})
        report["passed"] = not result.get("isError", False) and "native-background-acceptance" in json.dumps(result)
    finally:
        try:
            if closed:
                report["show"] = wails_call(debug, "window_control", {"window": "manager", "action": "show"})
        finally:
            if token:
                try:
                    request("/mcp/call", method="DELETE", client=True)
                finally:
                    status, _, _ = request("/api/v1/tokens/" + name + "/permanent", method="DELETE")
                    report["temporaryTokenRemoved"] = status == 204
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", type=Path, required=True)
    parser.add_argument("--management", default="http://127.0.0.1:19240")
    parser.add_argument("--debug", default="http://127.0.0.1:19199/mcp")
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    result = run(args.profile.resolve(), args.management, args.debug)
    args.report.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"passed": result.get("passed"), "temporaryTokenRemoved": result.get("temporaryTokenRemoved"),
                      "report": str(args.report)}, ensure_ascii=False))
    raise SystemExit(0 if result.get("passed") and result.get("temporaryTokenRemoved") else 1)
