"""Run real installed clients against a local counter through the app's stdio bridge.

No client SDK stands in for a real agent. Client auth uses the installed CLI's
normal authentication; no credentials are read, copied, or printed by this script.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
CLIENTS = {"claude": "claude", "codebuddy": "codebuddy"}


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
    path.chmod(0o600)


def upstream(run_dir):
    """Each stdio process owns its counter; two first calls meet at a file barrier."""
    run_dir = Path(run_dir)
    count, instance = 0, uuid.uuid4().hex
    audit = run_dir / f"fixture-{os.getpid()}.jsonl"
    for line in sys.stdin:
        message = json.loads(line)
        method = message.get("method")
        if "id" not in message:
            continue
        event = {"time": time.time(), "pid": os.getpid(), "instance": instance, "method": method}
        result = {}
        if method == "initialize":
            result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                      "serverInfo": {"name": "real-client-counter", "version": "1"}}
        elif method == "tools/list":
            result = {"tools": [{"name": "counter", "description": "Controlled acceptance counter. Call twice with your assigned marker; returns that marker, an instance ID, and process-local incrementing count. No files or network are accessible through this tool.",
                "inputSchema": {"type": "object", "properties": {"marker": {"type": "string", "enum": ["claude-real-acceptance", "codebuddy-real-acceptance"]}}, "required": ["marker"], "additionalProperties": False},
                "annotations": {"readOnlyHint": False, "destructiveHint": False, "idempotentHint": False, "openWorldHint": False}}]}
        elif method == "tools/call":
            params = message.get("params", {})
            marker = params.get("arguments", {}).get("marker")
            if params.get("name") != "counter" or marker not in ["claude-real-acceptance", "codebuddy-real-acceptance"]:
                result = {"isError": True, "content": [{"type": "text", "text": "Only the controlled counter and assigned markers are accepted."}]}
            else:
                count += 1
                if count == 1:
                    (run_dir / f"first-{marker}").touch(mode=0o600)
                    deadline = time.monotonic() + 40
                    while len(list(run_dir.glob("first-*-real-acceptance"))) < 2 and time.monotonic() < deadline:
                        time.sleep(0.1)
                payload = {"marker": marker, "count": count, "instance": instance,
                           "concurrentBarrierMet": len(list(run_dir.glob("first-*-real-acceptance"))) == 2}
                event["result"] = payload
                result = {"content": [{"type": "text", "text": json.dumps(payload)}], "structuredContent": payload, "isError": False}
        with audit.open("a") as log:
            log.write(json.dumps(event) + "\n")
        print(json.dumps({"jsonrpc": "2.0", "id": message["id"], "result": result}), flush=True)


def safe_text(text):
    text = text.replace(str(Path.home()), "$USER_HOME")
    text = re.sub(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}", "<redacted-email>", text)
    text = re.sub(r"(?i)(bearer\s+|(?:api[_-]?key|auth[_-]?token|access[_-]?token)[\s:=\"']+)[A-Za-z0-9._+/=-]{10,}", r"\1<redacted>", text)
    return text


def transcript(raw):
    """Retain tool discovery/calls/results and final text; discard account data and reasoning."""
    events = []
    for line in raw.splitlines():
        try:
            entry = json.loads(line)
        except ValueError:
            if line.strip():
                events.append({"type": "cli_message", "text": safe_text(line)})
            continue
        event = {"type": entry.get("type"), "subtype": entry.get("subtype")}
        if entry.get("type") == "system":
            for key in ("tools", "mcp_servers", "model", "permissionMode"):
                if key in entry:
                    event[key] = entry[key]
        if isinstance(entry.get("message"), dict):
            event["content"] = [part for part in entry["message"].get("content", [])
                                if isinstance(part, dict) and part.get("type") in ("tool_use", "tool_result", "text")]
        if entry.get("type") == "result":
            for key in ("is_error", "result", "num_turns", "duration_ms", "errors", "permission_denials"):
                if key in entry:
                    event[key] = entry[key]
        events.append(json.loads(safe_text(json.dumps(event, ensure_ascii=False))))
    return events


def config_fingerprints():
    files = [Path.home() / ".claude.json", Path.home() / ".claude/settings.json",
             Path.home() / ".codebuddy.json", Path.home() / ".codebuddy/settings.json",
             Path.home() / ".codebuddy/mcp.json", Path.home() / ".codebuddy/.mcp.json"]
    return {str(p).replace(str(Path.home()), "$USER_HOME"): hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None for p in files}


def self_check():
    """Verify only the fixture and redaction. This does not run a gateway or real client."""
    base = ROOT / ".cache" / "clients"
    base.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="fixture-check-", dir=base) as temp:
        def exercise(client):
            process = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "upstream", temp],
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
            def call(method, params=None):
                process.stdin.write(json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params or {}}) + "\n")
                process.stdin.flush()
                return json.loads(process.stdout.readline())["result"]
            try:
                assert call("initialize")["serverInfo"]["name"] == "real-client-counter"
                assert [tool["name"] for tool in call("tools/list")["tools"]] == ["counter"]
                return [call("tools/call", {"name": "counter", "arguments": {"marker": f"{client}-real-acceptance"}})["structuredContent"] for _ in range(2)]
            finally:
                process.stdin.close()
                process.wait(timeout=5)
        with ThreadPoolExecutor(max_workers=2) as pool:
            results = dict(zip(CLIENTS, pool.map(exercise, CLIENTS)))
    assert all([value["count"] for value in values] == [1, 2] for values in results.values())
    assert results["claude"][0]["instance"] != results["codebuddy"][0]["instance"]
    assert all(value["concurrentBarrierMet"] for values in results.values() for value in values)
    redacted = safe_text("Bearer controlled-test-token-long user@example.test " + str(Path.home()))
    assert "controlled-test-token-long" not in redacted and "user@example.test" not in redacted and str(Path.home()) not in redacted
    print(json.dumps({"scope": "fixture and redaction self-check only; no real clients, gateway, credentials, or network", "passed": True, "counters": results}, indent=2))


def run_client(client, run_dir, app, timeout):
    work = run_dir / client
    work.mkdir(mode=0o700)
    config = run_dir / f"{client}-mcp.json"
    write_json(config, {"mcpServers": {"gateway": {"type": "stdio", "command": str(app),
        "args": ["connect", "--client", f"acceptance-{client}", "--data-dir", str(run_dir / "profile")]}}})
    prompt = (f"This is a controlled MCP acceptance test. Use only the gateway MCP counter tool. "
              f"Your marker is {client}-real-acceptance. Discover the available counter tool and read its schema. "
              f"Call it exactly twice, sequentially, with that marker. The first call may wait up to 40 seconds for the other test client. "
              "Report the two exact tool results. Do not simulate a tool call or counter value. Do not use other tools, delegate, access files, browse, or run commands. On failure report the actual error and stop.")
    command = [shutil.which(CLIENTS[client]), "-p", prompt, "--strict-mcp-config", "--mcp-config", str(config),
        "--tools", "", "--allowedTools", "mcp__gateway__*", "--permission-mode", "dontAsk",
        "--no-session-persistence", "--output-format", "stream-json", "--verbose", "--setting-sources", "",
        "--settings", '{"disableAllHooks":true}', "--system-prompt", "You perform only the user's controlled MCP acceptance test."]
    if client == "claude":
        command += ["--disable-slash-commands", "--no-chrome"]
    else:
        command += ["--max-turns", "8"]
    env = dict(os.environ, DISABLE_AUTOUPDATER="1", DISABLE_TELEMETRY="1", DISABLE_ERROR_REPORTING="1",
        CLAUDE_CODE_DISABLE_AUTO_MEMORY="1", CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="1",
        CODEBUDDY_DISABLE_AUTO_MEMORY="1", CODEBUDDY_CODE_DISABLE_AUTO_MEMORY="1",
        CODEBUDDY_DISABLE_SHELL_SNAPSHOT="1", CODEBUDDY_DISABLE_CRON="1", CODEBUDDY_DISABLE_HOT_RELOAD="1")
    # The CLI selects its normal auth; do not copy credentials or override its auth directory.
    started = time.time()
    process = subprocess.Popen(command, cwd=work, env=env, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
    timed_out = False
    try:
        output, error = process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        os.killpg(process.pid, signal.SIGTERM)
        try:
            output, error = process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            output, error = process.communicate()
    result = {"client": client, "executable": command[0], "command": command, "started": started,
              "finished": time.time(), "exitCode": process.returncode, "timedOut": timed_out,
              "events": transcript(output), "stderr": safe_text(error)[-8000:]}
    write_json(run_dir / f"{client}-transcript.json", result)
    return result


def probe(args):
    run_dir = ROOT / ".cache" / "clients" / (time.strftime("%Y%m%d-%H%M%S") + "-" + uuid.uuid4().hex[:6])
    profile = run_dir / "profile"
    profile.mkdir(parents=True, mode=0o700)
    run_dir.chmod(0o700)
    with socket.socket() as reserve:
        reserve.bind(("127.0.0.1", 0))
        port = reserve.getsockname()[1]
    write_json(profile / "settings.json", {"theme": "system", "mode": "aggregate", "listenAddress": f"127.0.0.1:{port}", "launchAtLogin": False, "logRetentionDays": 1})
    write_json(profile / "core.json", {"listen": f"127.0.0.1:{port}", "data_dir": str(profile / "core"),
        "require_mcp_auth": True, "enable_socket": False, "routing_mode": "retrieve_tools", "enable_code_execution": False,
        "quarantine_enabled": False, "telemetry": {"enabled": False}, "tokenizer": {"enabled": False},
        "docker_isolation": {"enabled": False}, "mcpServers": [{"name": "real-client-counter", "protocol": "stdio", "command": sys.executable,
            "args": [str(Path(__file__).resolve()), "upstream", str(run_dir)], "enabled": True,
            "quarantined": False, "trust_mode": "auto", "session_mode": "isolated"}]})
    before = config_fingerprints()
    env = dict(os.environ, CI="true", HEADLESS="true", MCPPROXY_TELEMETRY="false")
    with (run_dir / "host.stderr").open("w") as error_log:
        host = subprocess.Popen([str(Path(args.host).resolve()), "--dir", str(profile), "--core", str(Path(args.core).resolve())],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=error_log, text=True, env=env)
        clients = []
        try:
            selector = selectors.DefaultSelector()
            selector.register(host.stdout, selectors.EVENT_READ)
            if not selector.select(timeout=45):
                raise RuntimeError(f"Acceptance host readiness timed out; {run_dir}")
            ready = host.stdout.readline()
            if not ready or not json.loads(ready).get("ready"):
                raise RuntimeError(f"Acceptance host did not start; {run_dir}")
            # The fixture's catalog must exist before each real client performs tools/list.
            deadline = time.monotonic() + 20
            while not list(run_dir.glob("fixture-*.jsonl")) and time.monotonic() < deadline:
                time.sleep(0.1)
            with ThreadPoolExecutor(max_workers=2) as pool:
                clients = list(pool.map(lambda name: run_client(name, run_dir, Path(args.app).resolve(), args.timeout), CLIENTS))
        finally:
            host.stdin.close()
            try:
                host.wait(timeout=65)
            except subprocess.TimeoutExpired:
                host.terminate()
                host.wait(timeout=5)
    audit = [json.loads(line) for path in run_dir.glob("fixture-*.jsonl") for line in path.read_text().splitlines()]
    calls = [event for event in audit if "result" in event]
    counts = {name: [e["result"]["count"] for e in sorted(calls, key=lambda e: e["time"]) if e["result"]["marker"] == f"{name}-real-acceptance"] for name in CLIENTS}
    instances = {name: sorted({e["instance"] for e in calls if e["result"]["marker"] == f"{name}-real-acceptance"}) for name in CLIENTS}
    same_instance = bool(set(instances["claude"]) & set(instances["codebuddy"]))
    config_unchanged = before == config_fingerprints()
    passed = (all(counts[name] == [1, 2] and len(instances[name]) == 1 for name in CLIENTS)
              and not same_instance and len(calls) == 4 and all(e["result"]["concurrentBarrierMet"] for e in calls)
              and all(c["exitCode"] == 0 and not c["timedOut"] for c in clients) and config_unchanged and host.returncode == 0)
    report = {"scope": "A05 partial: two real CLI agents, aggregate discovery, app stdio bridge, isolated upstream counters",
        "runDirectory": str(run_dir), "app": str(Path(args.app).resolve()), "core": str(Path(args.core).resolve()),
        "appSHA256": hashlib.sha256(Path(args.app).read_bytes()).hexdigest(), "coreSHA256": hashlib.sha256(Path(args.core).read_bytes()).hexdigest(),
        "clients": clients, "fixtureAudit": audit, "counts": counts, "instances": instances,
        "clientConfigUnchanged": config_unchanged, "hostExitCode": host.returncode, "passed": passed}
    write_json(Path(args.report), report)
    print(json.dumps({key: report[key] for key in ("runDirectory", "counts", "instances", "clientConfigUnchanged", "passed")}, indent=2))
    return passed


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "upstream":
        upstream(sys.argv[2])
    elif len(sys.argv) == 2 and sys.argv[1] == "--self-check":
        self_check()
    else:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument("--app", required=True)
        parser.add_argument("--core", required=True)
        parser.add_argument("--host", required=True)
        parser.add_argument("--report", required=True)
        parser.add_argument("--timeout", type=int, default=180)
        sys.exit(0 if probe(parser.parse_args()) else 1)
