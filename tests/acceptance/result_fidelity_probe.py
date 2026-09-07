"""Compare a real stdio baseline with the delivered core's three call surfaces.

Uses the same stdlib/JSON-RPC approach and canonical encoder as the catalog probe.
Only generated fixture data, a private temporary profile and loopback are used.
"""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import wave

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "patches/mcpproxy/probes"))
from benchmark_progressive import canonical


def payload(is_error):
    text = "MCP结果🧪\n" * 80000
    png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII="
    audio = io.BytesIO()
    with wave.open(audio, "wb") as output:
        output.setnchannels(1)
        output.setsampwidth(2)
        output.setframerate(8000)
        output.writeframes(b"\x00\x00" * 800)
    annotations = {"audience": ["assistant", "user"], "priority": 0.5,
                   "lastModified": "2026-09-07T00:00:00Z"}
    return {
        "content": [
            {"type": "text", "text": text, "annotations": annotations, "_meta": {"probe.block": "text"}},
            {"type": "image", "data": png, "mimeType": "image/png", "annotations": annotations,
             "_meta": {"probe.block": "image"}},
            {"type": "audio", "data": base64.b64encode(audio.getvalue()).decode(), "mimeType": "audio/wav",
             "annotations": annotations, "_meta": {"probe.block": "audio"}},
            {"type": "resource", "resource": {"uri": "probe://result/text", "mimeType": "text/plain",
                "text": "内嵌资源\n完整文本🧪", "_meta": {"probe.resource": "text"}},
             "annotations": annotations, "_meta": {"probe.block": "embedded-text"}},
            {"type": "resource", "resource": {"uri": "probe://result/blob", "mimeType": "application/octet-stream",
                "blob": base64.b64encode(bytes(range(256)) * 512).decode(), "_meta": {"probe.resource": "blob"}},
             "annotations": annotations, "_meta": {"probe.block": "embedded-blob"}},
            {"type": "resource_link", "uri": "probe://result/link", "name": "result-link", "title": "受控资源链接",
             "description": "No network fetch is required", "mimeType": "application/json", "size": 123,
             "annotations": annotations, "_meta": {"probe.block": "resource-link"}},
        ],
        "structuredContent": {"text": text, "numbers": [1, 2, 1.25],
            "nested": {"boolean": True, "null": None, "label": "受控保真"}},
        "isError": is_error,
        "_meta": {"probe.result": "metadata-preserved"},
    }


def fixture():
    for line in sys.stdin:
        message = json.loads(line)
        if "id" not in message:
            continue
        method = message.get("method")
        if method == "initialize":
            result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                      "serverInfo": {"name": "result-fidelity-fixture", "version": "1"}}
        elif method == "tools/list":
            result = {"tools": [{"name": "payload", "description": "Return generated result-fidelity samples",
                "inputSchema": {"type": "object", "properties": {"error": {"type": "boolean"}},
                                "required": ["error"], "additionalProperties": False},
                "annotations": {"readOnlyHint": True, "destructiveHint": False}}]}
        elif method == "tools/call":
            result = payload(message.get("params", {}).get("arguments", {}).get("error", False))
        else:
            result = {}
        sys.stdout.buffer.write(canonical({"jsonrpc": "2.0", "id": message["id"], "result": result}) + b"\n")
        sys.stdout.buffer.flush()


def digest(data):
    return {"bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def describe(result):
    blocks = []
    for index, block in enumerate(result.get("content", [])):
        details = {"index": index, "type": block.get("type"), "fields": sorted(block),
                   "canonical": digest(canonical(block))}
        if "text" in block:
            details["textUTF8"] = digest(block["text"].encode())
        if "data" in block:
            details["decodedBinary"] = digest(base64.b64decode(block["data"], validate=True))
        resource = block.get("resource", {})
        if "text" in resource:
            details["resourceTextUTF8"] = digest(resource["text"].encode())
        if "blob" in resource:
            details["resourceDecodedBinary"] = digest(base64.b64decode(resource["blob"], validate=True))
        blocks.append(details)
    return {"fields": sorted(result), "canonical": digest(canonical(result)), "content": blocks,
            "structuredContent": digest(canonical(result.get("structuredContent"))),
            "isError": result.get("isError", False)}


def normalize(result):
    # MCP's omitted false isError has the same semantics as an explicit false.
    result = dict(result)
    result.setdefault("isError", False)
    return result


def run(core_path, report_path):
    directory = Path(tempfile.mkdtemp(prefix="mcp-gateway-result-fidelity-", dir="/private/tmp"))
    report = {"scope": "Real stdio baseline versus final bundle core REST full_result, ordinary and progressive MCP",
              "core": str(core_path), "coreSHA256": hashlib.sha256(core_path.read_bytes()).hexdigest(),
              "evidenceDirectory": str(directory), "baseline": {}, "calls": [],
              "comparison": "Exact canonical JSON after defaulting only an omitted isError to false; binary decoded hashes also compared",
              "passed": False}
    command = [sys.executable, str(Path(__file__).resolve()), "--stdio"]
    baseline = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    expected = {}
    try:
        for ident, (method, params) in enumerate([
            ("initialize", {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "fidelity-baseline", "version": "1"}}),
            ("tools/list", {}),
            ("tools/call", {"name": "payload", "arguments": {"error": False}}),
            ("tools/call", {"name": "payload", "arguments": {"error": True}}),
        ], 1):
            baseline.stdin.write(canonical({"jsonrpc": "2.0", "id": ident, "method": method, "params": params}) + b"\n")
            baseline.stdin.flush()
            result = json.loads(baseline.stdout.readline())["result"]
            if method == "tools/call":
                label = "tool-error" if params["arguments"]["error"] else "success"
                expected[label] = normalize(result)
                report["baseline"][label] = describe(result)
                (directory / f"baseline-{label}.json").write_bytes(canonical(result))
    finally:
        baseline.stdin.close()
        baseline.wait(timeout=5)
    report["baselineExitCode"] = baseline.returncode

    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    config_path = directory / "core.json"
    config_path.write_bytes(canonical({"listen": f"127.0.0.1:{port}", "require_mcp_auth": True,
        "quarantine_enabled": False, "tokenizer": {"enabled": False}, "telemetry": {"enabled": False},
        "docker_isolation": {"enabled": False}, "tool_response_limit": 0,
        "tool_response_mode": "compact", "direct_tool_response_mode": "full",
        "mcpServers": [{"name": "fidelity", "protocol": "stdio", "command": command[0], "args": command[1:],
                        "enabled": True, "quarantined": False, "session_mode": "isolated"}]}))
    config_path.chmod(0o600)
    env = dict(os.environ, MCPPROXY_API_KEY="controlled-result-fidelity-management", CI="true", HEADLESS="true",
               MCPPROXY_TELEMETRY="false", MCPPROXY_KEYRING_WRITE="0")
    base = f"http://127.0.0.1:{port}"
    def request(path, value=None, headers=None, method=None):
        h = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
        h.update(headers or {"X-API-Key": env["MCPPROXY_API_KEY"]})
        req = urllib.request.Request(base + path, data=None if value is None else canonical(value), headers=h, method=method)
        with urllib.request.urlopen(req, timeout=30) as response:
            raw = response.read()
            if "text/event-stream" in response.headers.get("Content-Type", ""):
                events = [json.loads(line[6:]) for line in raw.decode().splitlines() if line.startswith("data: ")]
                body = next((event for event in events if isinstance(value, dict) and event.get("id") == value.get("id")), None)
            else:
                body = json.loads(raw) if raw else None
            return body, dict(response.headers), len(raw), response.status
    def rpc(path, method, params, headers, ident):
        body, response_headers, size, status = request(path, {"jsonrpc": "2.0", "id": ident, "method": method, "params": params}, headers)
        if not isinstance(body, dict) or "result" not in body:
            raise RuntimeError(f"RPC {method} failed at {path}")
        return body["result"], response_headers, size, status

    with (directory / "core.log").open("w") as log:
        core = subprocess.Popen([str(core_path), "serve", "--config", str(config_path), "--data-dir", str(directory),
                                 "--log-dir", str(directory / "logs"), "--enable-socket=false"], env=env, stdout=log, stderr=log)
        token_created = False
        try:
            for _ in range(150):
                if core.poll() is not None:
                    raise RuntimeError("Controlled core exited before ready")
                try:
                    servers = request("/api/v1/servers")[0]["data"]["servers"]
                    if len(servers) == 1 and servers[0].get("connected") and servers[0].get("tool_count") == 1:
                        break
                except (urllib.error.URLError, ConnectionError):
                    pass
                time.sleep(.2)
            else:
                raise RuntimeError("Fidelity fixture did not become ready")
            token = request("/api/v1/tokens", {"name": "fidelity-only", "allowed_servers": ["fidelity"],
                                                "permissions": ["read"], "expires_in": "1h"})[0]["data"]["token"]
            token_created = True
            for mode, path in (("rest-full", "/api/v1/tools/call"), ("ordinary", "/mcp/all"), ("progressive", "/mcp/call")):
                headers = None
                if mode != "rest-full":
                    headers = {"Authorization": "Bearer " + token}
                    initialized, returned_headers, _, _ = rpc(path, "initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                        "clientInfo": {"name": "fidelity-" + mode, "version": "1"}}, headers, 1)
                    headers.update({"Mcp-Session-Id": returned_headers["Mcp-Session-Id"], "MCP-Protocol-Version": initialized["protocolVersion"]})
                    request(path, {"jsonrpc": "2.0", "method": "notifications/initialized"}, headers)
                    rpc(path, "tools/list", {}, headers, 2)
                for number, (label, baseline_result) in enumerate(expected.items(), 3):
                    arguments = {"error": label == "tool-error"}
                    started = time.perf_counter()
                    if mode == "rest-full":
                        returned, _, response_size, status = request(path, {"tool_name": "call_tool_read", "full_result": True,
                            "arguments": {"name": "fidelity:payload", "args": arguments}})
                        result = returned["data"]
                    else:
                        params = {"name": "fidelity__payload", "arguments": arguments} if mode == "ordinary" else {
                            "name": "call_tool_read", "arguments": {"name": "fidelity:payload", "args": arguments}}
                        result, _, response_size, status = rpc(path, "tools/call", params, headers, number)
                    normalized = normalize(result)
                    equal = normalized == baseline_result
                    report["calls"].append({"mode": mode, "case": label, "httpStatus": status,
                        "httpBodyBytes": response_size, "elapsedMs": round((time.perf_counter() - started) * 1000, 3),
                        "received": describe(result), "normalizedResultSHA256": digest(canonical(normalized))["sha256"],
                        "baselineNormalizedSHA256": digest(canonical(baseline_result))["sha256"], "exactlyPreserved": equal})
                    (directory / f"{mode}-{label}.json").write_bytes(canonical(result))
                if headers:
                    request(path, headers=headers, method="DELETE")
            report["adminKeyAbsentFromConfig"] = not json.loads(config_path.read_text()).get("api_key")
            report["passed"] = len(report["calls"]) == 6 and all(item["exactlyPreserved"] for item in report["calls"])
        except Exception as exc:
            report["probeError"] = str(exc)
        finally:
            if token_created and core.poll() is None:
                try:
                    report["temporaryTokenRemoved"] = request("/api/v1/tokens/fidelity-only/permanent", method="DELETE")[3] == 204
                except Exception:
                    report["temporaryTokenRemoved"] = False
            core.terminate()
            try:
                core.wait(timeout=35)
            except subprocess.TimeoutExpired:
                core.kill()
                core.wait(timeout=5)
            report["coreExitCode"] = core.returncode
            report["passed"] = report["passed"] and core.returncode == 0 and report.get("temporaryTokenRemoved", False)
            report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"passed": report["passed"], "comparisons": len(report["calls"]), "report": str(report_path),
                      "probeError": report.get("probeError")}, ensure_ascii=False))
    return report["passed"]


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stdio", action="store_true")
    parser.add_argument("--core", type=Path)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    if args.stdio:
        fixture()
    else:
        if not args.core or not args.report:
            parser.error("--core and --report are required")
        raise SystemExit(0 if run(args.core.resolve(), args.report.resolve()) else 1)
