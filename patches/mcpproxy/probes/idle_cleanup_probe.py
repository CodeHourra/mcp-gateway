"""Verify the real 15-minute idle timeout with only an owned local fixture."""
import argparse
import hashlib
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


def run(core_path, report_path):
    directory = Path(tempfile.mkdtemp(prefix="mcp-gateway-idle-probe-", dir="/private/tmp"))
    with socket.socket() as port_socket:
        port_socket.bind(("127.0.0.1", 0))
        port = port_socket.getsockname()[1]
    fixture = Path(__file__).with_name("protocol_probe.py").resolve()
    config_path = directory / "core.json"
    config_path.write_text(json.dumps({"listen": f"127.0.0.1:{port}", "require_mcp_auth": True,
        "quarantine_enabled": False, "tokenizer": {"enabled": False}, "telemetry": {"enabled": False},
        "docker_isolation": {"enabled": False}, "tool_response_limit": 0,
        "mcpServers": [{"name": "idle-probe", "protocol": "stdio", "command": sys.executable,
            "args": [str(fixture), "stdio"], "enabled": True, "quarantined": False,
            "session_mode": "isolated"}]}))
    config_path.chmod(0o600)
    env = dict(os.environ, MCPPROXY_API_KEY="controlled-idle-management-key", CI="true", HEADLESS="true", MCPPROXY_TELEMETRY="false")
    def request(path, payload=None):
        req = urllib.request.Request(f"http://127.0.0.1:{port}" + path,
            data=None if payload is None else json.dumps(payload).encode(),
            headers={"Content-Type": "application/json", "X-API-Key": env["MCPPROXY_API_KEY"]})
        with urllib.request.urlopen(req, timeout=15) as response:
            return json.load(response)["data"]
    with (directory / "core.log").open("w") as log:
        core = subprocess.Popen([core_path, "serve", "--config", str(config_path), "--data-dir", str(directory),
            "--log-dir", str(directory / "logs"), "--enable-socket=false"], env=env, stdout=log, stderr=subprocess.STDOUT)
        report = {"evidence_directory": str(directory), "core": core_path,
            "core_sha256": hashlib.sha256(Path(core_path).read_bytes()).hexdigest(), "configured_idle_seconds": 900, "passed": False}
        try:
            for _ in range(100):
                try:
                    ready = request("/api/v1/servers")["servers"]
                    if ready and ready[0].get("connected") and ready[0].get("tool_count", 0):
                        break
                except (urllib.error.URLError, ConnectionError):
                    pass
                time.sleep(.2)
            else:
                raise RuntimeError("fixture failed to connect")
            result = request("/api/v1/tools/call", {"tool_name": "call_tool_read", "full_result": True,
                "arguments": {"name": "idle-probe:echo", "args": {"text": "idle-session"}}})
            pid = result["structuredContent"]["pid"]
            before = request("/api/v1/gateway")
            assert before["isolated_sessions"] == 1, before
            started = time.monotonic()
            report.update(session_pid=pid, state_before=before, started_at=time.strftime("%Y-%m-%dT%H:%M:%S%z"))
            print("IDLE_PROBE_STARTED", json.dumps(report), flush=True)
            for _ in range(190):
                time.sleep(5)
                state = request("/api/v1/gateway")
                if state["isolated_sessions"] == 0:
                    break
            elapsed = time.monotonic() - started
            try:
                os.kill(pid, 0)
                alive = True
            except ProcessLookupError:
                alive = False
            report.update(elapsed_seconds=round(elapsed, 3), state_after=state, session_process_alive=alive,
                passed=state["isolated_sessions"] == 0 and not alive and elapsed >= 890)
            assert report["passed"], report
        finally:
            core.terminate()
            try:
                core.wait(timeout=35)
            except subprocess.TimeoutExpired:
                core.kill(); core.wait()
            report["core_exit"] = core.returncode
            Path(report_path).write_text(json.dumps(report, indent=2) + "\n")
            print("IDLE_PROBE_RESULT", json.dumps(report), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--core", required=True)
    parser.add_argument("--report", required=True)
    args = parser.parse_args()
    run(args.core, args.report)
