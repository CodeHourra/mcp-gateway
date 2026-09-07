"""Compare real /mcp/all and /mcp/call traffic using a controlled local catalog.

Python standard library only. No user MCP configuration or credentials are read.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import socket
import statistics
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def canonical(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode("utf-8")


def fixture(server, count):
    catalog = [{"name": f"lookup_{number:02d}",
        "description": f"Look up a controlled benchmark record in {server}, collection {number:02d}. "
            + ("benchmarkneedle identifies this exact collection. " if server == "catalog00" and number == 0 else "")
            + "Return a deterministic record with selected fields, optional history and pagination metadata. This local fixture reads no external data.",
        "inputSchema": {"type": "object", "additionalProperties": False,
            "properties": {"record_id": {"type": "string", "description": "Exact record identifier to retrieve", "minLength": 1},
                "fields": {"type": "array", "description": "Optional result field selection", "items": {"type": "string", "enum": ["title", "status", "updated_at"]}},
                "include_history": {"type": "boolean", "description": "Whether to include the change history", "default": False},
                "page": {"type": "object", "properties": {"limit": {"type": "integer", "minimum": 1, "maximum": 100},
                    "cursor": {"type": "string"}}, "additionalProperties": False}}, "required": ["record_id"]},
        "annotations": {"readOnlyHint": True, "destructiveHint": False, "idempotentHint": True, "openWorldHint": False}}
        for number in range(count)]
    for line in sys.stdin:
        message = json.loads(line)
        if "id" not in message:
            continue
        method = message.get("method")
        if method == "initialize":
            result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                "serverInfo": {"name": server, "version": "1"}}
        elif method == "tools/list":
            result = {"tools": catalog}
        elif method == "tools/call":
            params = message.get("params", {})
            arguments = params.get("arguments", {})
            if params.get("name") not in {tool["name"] for tool in catalog} or not arguments.get("record_id"):
                result = {"isError": True, "content": [{"type": "text", "text": "Unknown tool or missing record_id"}]}
            else:
                record = {"server": server, "tool": params["name"], "record_id": arguments["record_id"],
                    "title": "Controlled benchmark record", "status": "ready"}
                result = {"content": [{"type": "text", "text": json.dumps(record)}], "structuredContent": record, "isError": False}
        else:
            result = {}
        print(json.dumps({"jsonrpc": "2.0", "id": message["id"], "result": result}), flush=True)


def run_scale(core_path, server_count, tools_per_server, samples):
    directory = Path(tempfile.mkdtemp(prefix="mcp-gateway-benchmark-", dir="/private/tmp"))
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    config_path = directory / "core.json"
    config_path.write_bytes(canonical({"listen": f"127.0.0.1:{port}", "require_mcp_auth": True,
        "quarantine_enabled": False, "tokenizer": {"enabled": False}, "telemetry": {"enabled": False},
        "docker_isolation": {"enabled": False}, "tool_response_limit": 0,
        "tool_response_mode": "compact", "direct_tool_response_mode": "full",
        "mcpServers": [{"name": f"catalog{number:02d}", "protocol": "stdio", "command": sys.executable,
            "args": [str(Path(__file__).resolve()), "--stdio", f"catalog{number:02d}", str(tools_per_server)],
            "enabled": True, "quarantined": False, "session_mode": "isolated"} for number in range(server_count)]}))
    config_path.chmod(0o600)
    env = dict(os.environ, MCPPROXY_API_KEY="controlled-benchmark-management-key", CI="true", HEADLESS="true", MCPPROXY_TELEMETRY="false")
    base = f"http://127.0.0.1:{port}"
    def request(path, payload=None, headers=None, method=None):
        request_headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
        request_headers.update(headers or {"X-API-Key": env["MCPPROXY_API_KEY"]})
        req = urllib.request.Request(base + path, data=None if payload is None else canonical(payload),
            headers=request_headers, method=method)
        started = time.perf_counter()
        with urllib.request.urlopen(req, timeout=30) as response:
            raw = response.read()
            elapsed_ms = (time.perf_counter() - started) * 1000
            data = raw.decode()
            if "text/event-stream" in response.headers.get("Content-Type", ""):
                events = [json.loads(line[6:]) for line in data.splitlines() if line.startswith("data: ")]
                parsed = next((event for event in events if isinstance(payload, dict) and event.get("id") == payload.get("id")), None)
            else:
                parsed = json.loads(data) if data else None
            return parsed, dict(response.headers), {"elapsed_ms": round(elapsed_ms, 3), "http_body_bytes": len(raw)}
    def rpc(path, method, params, headers, request_id):
        payload = {"jsonrpc": "2.0", "id": request_id, "method": method}
        if params is not None:
            payload["params"] = params
        response, returned_headers, measured = request(path, payload, headers)
        assert response and "result" in response, response
        measured.update(method=method, params=params, response=response)
        return response["result"], returned_headers, measured
    report = {"servers": server_count, "tools_per_server": tools_per_server,
        "upstream_tool_count": server_count * tools_per_server, "evidence_directory": str(directory),
        "modes": {"ordinary": {"endpoint": "/mcp/all", "samples": []}, "progressive": {"endpoint": "/mcp/call", "samples": []}}}
    with (directory / "core.log").open("w") as log:
        core = subprocess.Popen([core_path, "serve", "--config", str(config_path), "--data-dir", str(directory),
            "--log-dir", str(directory / "logs"), "--enable-socket=false"], env=env, stdout=log, stderr=subprocess.STDOUT)
        try:
            for _ in range(150):
                if core.poll() is not None:
                    raise RuntimeError(f"Core exited early; inspect {directory}")
                try:
                    servers = request("/api/v1/servers")[0]["data"]["servers"]
                    if len(servers) == server_count and all(server.get("connected") and server.get("tool_count") == tools_per_server for server in servers):
                        break
                except (urllib.error.URLError, ConnectionError):
                    pass
                time.sleep(.2)
            else:
                raise RuntimeError(f"Controlled catalog did not connect; inspect {directory}")
            tokens = {}
            for mode in report["modes"]:
                token = request("/api/v1/tokens", {"name": "benchmark-" + mode,
                    "allowed_servers": ["*"], "permissions": ["read", "write", "destructive"], "expires_in": "30d"})[0]["data"]
                tokens[mode] = token.get("token", token.get("raw_token"))
                assert tokens[mode]
            assert tokens["ordinary"] != tokens["progressive"]
            for sample_number in range(samples):
                order = ["ordinary", "progressive"] if sample_number % 2 == 0 else ["progressive", "ordinary"]
                for mode in order:
                    mode_report = report["modes"][mode]
                    path = mode_report["endpoint"]
                    headers = {"Authorization": "Bearer " + tokens[mode]}
                    initialized, returned_headers, init_measure = rpc(path, "initialize", {
                        "protocolVersion": "2025-11-25", "capabilities": {},
                        "clientInfo": {"name": "controlled-benchmark-" + mode, "version": "1"}}, headers, 1)
                    headers.update({"Mcp-Session-Id": returned_headers["Mcp-Session-Id"], "MCP-Protocol-Version": initialized["protocolVersion"]})
                    request(path, {"jsonrpc": "2.0", "method": "notifications/initialized"}, headers)
                    listed, _, list_measure = rpc(path, "tools/list", None, headers, 2)
                    tools = sorted(listed["tools"], key=lambda tool: tool["name"])
                    names = [tool["name"] for tool in tools]
                    sample = {"sample": sample_number + 1, "initial_tool_count": len(tools),
                        "initial_definition_bytes": len(canonical(tools)),
                        "initial_input_schema_bytes": len(canonical([tool.get("inputSchema") for tool in tools])),
                        "initial_tool_names": names, "initialize": init_measure, "tools_list": list_measure,
                        "additional_discovery_rounds": 0, "discovery": []}
                    if mode == "ordinary":
                        expected = {f"catalog{server:02d}__lookup_{tool:02d}" for server in range(server_count) for tool in range(tools_per_server)} | {"describe_tool"}
                        assert set(names) == expected, names
                        invoke = {"name": "catalog00__lookup_00", "arguments": {"record_id": "fixture-001", "include_history": False}}
                    else:
                        assert set(names) == {"call_tool_read", "call_tool_write", "call_tool_destructive", "retrieve_tools", "describe_tool", "read_cache"}, names
                        assert b"record_id" not in canonical(tools), "Initial progressive list leaked the upstream parameter schema"
                        discovered, _, discovery = rpc(path, "tools/call", {"name": "retrieve_tools", "arguments": {"query": "benchmarkneedle", "limit": 1}}, headers, 3)
                        assert not discovered.get("isError") and "catalog00:lookup_00" in json.dumps(discovered), discovered
                        discovered_entry = json.loads(discovered["content"][0]["text"])["tools"]
                        assert len(discovered_entry) == 1 and discovered_entry[0]["lossy"] and "inputSchema" not in discovered_entry[0], discovered_entry
                        described, _, describe = rpc(path, "tools/call", {"name": "describe_tool", "arguments": {"tool_ids": ["catalog00:lookup_00"]}}, headers, 4)
                        assert not described.get("isError") and "record_id" in json.dumps(described) and "inputSchema" in json.dumps(described), described
                        sample.update(additional_discovery_rounds=2, discovery=[discovery, describe])
                        invoke = {"name": "call_tool_read", "arguments": {"name": "catalog00:lookup_00", "args": {"record_id": "fixture-001", "include_history": False}}}
                    called, _, call_measure = rpc(path, "tools/call", invoke, headers, 5)
                    assert not called.get("isError") and called.get("structuredContent", {}).get("record_id") == "fixture-001", called
                    sample.update(call_succeeded=True, call=call_measure,
                        discovery_elapsed_ms=round(sum(step["elapsed_ms"] for step in sample["discovery"]), 3),
                        post_list_rpc_elapsed_ms=round(sum(step["elapsed_ms"] for step in sample["discovery"]) + call_measure["elapsed_ms"], 3))
                    mode_report["samples"].append(sample)
                    request(path, headers=headers, method="DELETE")
            for ordinary, progressive in zip(report["modes"]["ordinary"]["samples"], report["modes"]["progressive"]["samples"]):
                direct_definition = next(tool for tool in ordinary["tools_list"]["response"]["result"]["tools"] if tool["name"] == "catalog00__lookup_00")
                described_definition = json.loads(progressive["discovery"][1]["response"]["result"]["content"][0]["text"])["definitions"][0]
                assert direct_definition["inputSchema"] == described_definition["inputSchema"], "Modes exposed different schemas for the same target"
                assert ordinary["call"]["response"]["result"]["structuredContent"] == progressive["call"]["response"]["result"]["structuredContent"], "Modes returned different records"
            report.update(target_schema_identical=True, target_result_identical=True, progressive_schema_deferred=True)
            for mode_report in report["modes"].values():
                values = mode_report["samples"]
                mode_report["summary"] = {field: values[0][field] for field in ["initial_tool_count", "initial_definition_bytes", "initial_input_schema_bytes", "additional_discovery_rounds"]}
                for field in ["initial_tool_count", "initial_definition_bytes", "initial_input_schema_bytes"]:
                    assert len({sample[field] for sample in values}) == 1, (field, values)
                mode_report["summary"].update(call_successes=sum(sample["call_succeeded"] for sample in values), samples=samples,
                    tools_list_median_ms=round(statistics.median(sample["tools_list"]["elapsed_ms"] for sample in values), 3),
                    discovery_median_ms=round(statistics.median(sample["discovery_elapsed_ms"] for sample in values), 3),
                    call_median_ms=round(statistics.median(sample["call"]["elapsed_ms"] for sample in values), 3),
                    post_list_rpc_median_ms=round(statistics.median(sample["post_list_rpc_elapsed_ms"] for sample in values), 3))
            assert not json.loads(config_path.read_text()).get("api_key"), "Management API key persisted"
        finally:
            core.terminate()
            try:
                core.wait(timeout=35)
            except subprocess.TimeoutExpired:
                core.kill(); core.wait()
            report["core_exit"] = core.returncode
            (directory / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    assert core.returncode == 0, report
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--core")
    parser.add_argument("--report", default="benchmark-progressive.json")
    parser.add_argument("--samples", type=int, default=3)
    parser.add_argument("--stdio", nargs=2, metavar=("SERVER", "TOOL_COUNT"))
    args = parser.parse_args()
    if args.stdio:
        fixture(args.stdio[0], int(args.stdio[1]))
        sys.exit(0)
    if not args.core or args.samples < 1:
        parser.error("--core and a positive --samples are required")
    binary = Path(args.core).resolve()
    result = {"started_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "core_binary": str(binary),
        "core_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "platform": platform.platform(),
        "python": platform.python_version(), "token_measurement": None,
        "byte_method": "UTF-8 length of json.dumps(value, ensure_ascii=False, separators=(',', ':'), sort_keys=True); tools sorted by name; schemas is an array of each inputSchema in the same order; excludes JSON-RPC envelope and HTTP/SSE framing",
        "scales": []}
    for count, per_server in [(2, 3), (8, 12)]:
        scale = run_scale(str(binary), count, per_server, args.samples)
        result["scales"].append(scale)
        print("BENCHMARK_SCALE", json.dumps({"servers": count, "tools_per_server": per_server,
            "results": {mode: value["summary"] for mode, value in scale["modes"].items()}}), flush=True)
    progressive_sizes = {scale["modes"]["progressive"]["summary"]["initial_definition_bytes"] for scale in result["scales"]}
    assert len(progressive_sizes) == 1, "Progressive initial tool definitions grew with the upstream catalog"
    result["passed"] = True
    Path(args.report).write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print("BENCHMARK_REPORT", str(Path(args.report).resolve()), flush=True)
