"""Check owned stdio descendants after core shutdown; uses only temporary state."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request


def upstream(run_dir):
    child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "child", run_dir],
                             stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                             stderr=subprocess.DEVNULL)
    Path(run_dir, f"owned-pids-{os.getpid()}.json").write_text(json.dumps({"upstream": os.getpid(), "descendant": child.pid}))
    for line in sys.stdin:
        msg = json.loads(line)
        if "id" not in msg:
            continue
        result = {}
        if msg.get("method") == "initialize":
            result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                      "serverInfo": {"name": "process-tree-probe", "version": "1"}}
        elif msg.get("method") == "tools/list":
            result = {"tools": [{"name": "echo", "description": "Controlled echo", "inputSchema": {"type": "object"}, "annotations": {"readOnlyHint": True}}]}
        elif msg.get("method") == "tools/call":
            if msg.get("params", {}).get("arguments", {}).get("hold"):
                Path(run_dir, f"call-started-{os.getpid()}").touch()
                time.sleep(60)
            result = {"content": [{"type": "text", "text": "completed"}]}
        print(json.dumps({"jsonrpc": "2.0", "id": msg["id"], "result": result}), flush=True)


def alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False


def probe(core_path, report_path, shutdown_mode):
    run_dir = Path(tempfile.mkdtemp(prefix="mcp-gateway-process-tree-", dir="/private/tmp"))
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    config_path = run_dir / "core.json"
    config_path.write_text(json.dumps({"listen": f"127.0.0.1:{port}",
        "api_key": "controlled-process-tree-probe", "require_mcp_auth": True,
        "quarantine_enabled": False, "telemetry": {"enabled": False}, "tokenizer": {"enabled": False},
        "docker_isolation": {"enabled": False}, "mcpServers": [{
            "name": "process-tree-probe", "protocol": "stdio", "command": sys.executable,
            "args": [str(Path(__file__).resolve()), "upstream", str(run_dir)],
            "enabled": True, "quarantined": False, "trust_mode": "auto", "session_mode": "isolated"}]}))
    config_path.chmod(0o600)
    env = dict(os.environ, CI="true", HEADLESS="true", MCPPROXY_TELEMETRY="false", MCPPROXY_KEYRING_WRITE="0")
    env.pop("MCPPROXY_API_KEY", None)
    owned = {}
    def load_owned():
        for path in run_dir.glob("owned-pids-*.json"):
            for name, pid in json.loads(path.read_text()).items():
                owned[f"{name}:{pid}"] = pid

    def request(path, data=None):
        request = urllib.request.Request(f"http://127.0.0.1:{port}" + path,
                                          headers={"X-API-Key": "controlled-process-tree-probe", "Content-Type": "application/json"},
                                          data=None if data is None else json.dumps(data).encode())
        with urllib.request.urlopen(request, timeout=20) as response:
            return json.load(response)["data"]

    with (run_dir / "core.log").open("w") as log:
        core = subprocess.Popen([core_path, "serve", "--config", str(config_path), "--data-dir", str(run_dir),
                                 "--log-dir", str(run_dir / "logs"), "--enable-socket=false"],
                                env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            connected = False
            for _ in range(100):
                if core.poll() is not None:
                    raise RuntimeError(f"Core exited {core.returncode}; inspect {run_dir}")
                try:
                    servers = request("/api/v1/servers")["servers"]
                    connected = any(s["name"] == "process-tree-probe" and s.get("connected") for s in servers)
                except (urllib.error.URLError, TimeoutError):
                    pass
                if connected:
                    break
                time.sleep(0.2)
            if not connected:
                raise RuntimeError(f"Upstream did not connect; inspect {run_dir}")
            call_outcome = {}
            if shutdown_mode == "force-active":
                def call():
                    try:
                        call_outcome["response"] = request("/api/v1/tools/call", {"tool_name": "call_tool_read", "full_result": True,
                            "arguments": {"name": "process-tree-probe:echo", "args": {"hold": True}}})
                    except Exception as exc:
                        call_outcome["error"] = str(exc)
                caller = threading.Thread(target=call, daemon=True)
                caller.start()
                for _ in range(75):
                    if list(run_dir.glob("call-started-*")) and request("/api/v1/gateway")["in_flight"] > 0:
                        break
                    time.sleep(0.2)
                else:
                    raise RuntimeError(f"Delayed call did not start: {call_outcome}; inspect {run_dir}")
            load_owned()
            before = {name: {"pid": pid, "pgid": os.getpgid(pid), "alive": alive(pid)} for name, pid in owned.items()}
            core_pgid = os.getpgid(core.pid)
            started = time.monotonic()
            stop_result = request("/api/v1/gateway/stop", {"force": True}) if shutdown_mode == "force-active" else None
            core.terminate()
            core.wait(timeout=20)
            if shutdown_mode == "force-active":
                caller.join(timeout=2)
            time.sleep(0.3)
            after = {name: alive(pid) for name, pid in owned.items()}
            report = {"scope": "controlled core lifecycle; not native app acceptance",
                      "core": str(Path(core_path).resolve()), "sha256": hashlib.sha256(Path(core_path).read_bytes()).hexdigest(),
                      "evidence_dir": str(run_dir), "core_pid": core.pid, "core_pgid": core_pgid,
                      "core_exit_code": core.returncode, "shutdown_seconds": round(time.monotonic() - started, 3),
                      "shutdown_mode": shutdown_mode, "stop_result": stop_result, "call_outcome": call_outcome,
                      "before": before, "alive_after_graceful_shutdown": after,
                      "passed": not any(after.values())}
            Path(report_path).write_text(json.dumps(report, indent=2) + "\n")
            print(json.dumps(report, indent=2))
            return report["passed"]
        finally:
            if core.poll() is None:
                core.kill()
                core.wait(timeout=5)
            load_owned()
            for pid in owned.values():
                if alive(pid):
                    os.kill(pid, signal.SIGKILL)


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "upstream":
        upstream(sys.argv[2])
    elif len(sys.argv) > 1 and sys.argv[1] == "child":
        time.sleep(120)
    else:
        parser = argparse.ArgumentParser()
        parser.add_argument("--core", required=True)
        parser.add_argument("--report", required=True)
        parser.add_argument("--shutdown-mode", choices=["graceful", "force-active"], default="graceful")
        args = parser.parse_args()
        sys.exit(0 if probe(args.core, args.report, args.shutdown_mode) else 1)
