"""Controlled protocol probe; never loads an existing user's MCP config."""
import json
import hashlib
from concurrent.futures import ThreadPoolExecutor
import uuid
import queue
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
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


call_counter = 0

def reply(msg):
    global call_counter
    method = msg.get("method")
    if "id" not in msg:
        return None
    if method == "initialize":
        result = {"protocolVersion": "2025-11-25", "capabilities": {"tools": {}},
                  "serverInfo": {"name": "controlled-core-probe", "version": "1"}}
    elif method == "tools/list":
        result = {"tools": [{"name": "echo", "description": "Echo a supplied text message", "inputSchema": {
            "type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"]},
            "annotations": {"readOnlyHint": True}}]}
    elif method == "tools/call":
        value = msg.get("params", {}).get("arguments", {}).get("text", "")
        call_counter += 1
        if value == "slow": time.sleep(10)
        result = {"content": [{"type": "text", "text": value}], "structuredContent": {"echo": value, "counter": call_counter, "pid": os.getpid(), "gateway_secret_in_env": bool(os.environ.get("MCPPROXY_API_KEY"))}, "isError": value == "upstream-error"}
    else:
        result = {}
    return {"jsonrpc": "2.0", "id": msg["id"], "result": result}


if len(sys.argv) > 1 and sys.argv[1] == "stdio":
    for line in sys.stdin:
        result = reply(json.loads(line))
        if result is not None:
            print(json.dumps(result), flush=True)
    sys.exit(0)


class Upstream(BaseHTTPRequestHandler):
    sessions = {}
    def log_message(self, *args): pass
    def send_json(self, status, result, session=None):
        body=json.dumps(result).encode() if result is not None else b""
        self.send_response(status)
        self.send_header("Content-Type","application/json")
        self.send_header("Content-Length",str(len(body)))
        if session: self.send_header("Mcp-Session-Id",session)
        self.end_headers()
        self.wfile.write(body)
    def do_POST(self):
        if self.headers.get("Authorization")!="Bearer controlled-upstream-secret":
            self.send_json(401,{"error":"authorization required"});return
        msg=json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        session=self.headers.get("Mcp-Session-Id")
        sse=self.path.startswith("/messages")
        if sse: session=urllib.parse.parse_qs(urllib.parse.urlsplit(self.path).query).get("session_id",[""])[0]
        elif msg.get("method")=="initialize":
            session=uuid.uuid4().hex
            self.sessions[session]={"counter":0,"queue":None}
        state=self.sessions.get(session)
        if not state: self.send_json(404,{"error":"unknown session"});return
        result=reply(msg)
        if msg.get("method")=="tools/call":
            state["counter"]+=1
            result["result"]["structuredContent"].update(counter=state["counter"],upstream_session=session)
        if sse:
            if result is not None: state["queue"].put(result)
            self.send_json(202,None)
        else: self.send_json(200 if result is not None else 202,result,session)
    def do_GET(self):
        if self.path!="/sse": self.send_json(405,{});return
        if self.headers.get("Authorization")!="Bearer controlled-upstream-secret": self.send_json(401,{});return
        session=uuid.uuid4().hex
        state={"counter":0,"queue":queue.Queue()}
        self.sessions[session]=state
        self.send_response(200)
        self.send_header("Content-Type","text/event-stream")
        self.send_header("Cache-Control","no-cache")
        self.end_headers()
        try:
            self.wfile.write(("event: endpoint\ndata: /messages?session_id="+session+"\n\n").encode());self.wfile.flush()
            while True:
                try: result=state["queue"].get(timeout=0.5)
                except queue.Empty:
                    self.wfile.write(b": heartbeat\n\n");self.wfile.flush();continue
                self.wfile.write(("event: message\ndata: "+json.dumps(result)+"\n\n").encode());self.wfile.flush()
        except (BrokenPipeError,ConnectionResetError): pass
        finally: self.sessions.pop(session,None)
    def do_DELETE(self):
        self.sessions.pop(self.headers.get("Mcp-Session-Id"),None)
        self.send_json(200,{})


if "MCP_GATEWAY_CORE_BINARY" not in os.environ:
    raise SystemExit("Set MCP_GATEWAY_CORE_BINARY to the patched core binary path")
if len(sys.argv)>1 and sys.argv[1]=="oauth" and "MCP_GATEWAY_OAUTH_FIXTURE_BINARY" not in os.environ:
    raise SystemExit("Set MCP_GATEWAY_OAUTH_FIXTURE_BINARY to the upstream OAuth fixture binary path")

upstream = ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
threading.Thread(target=upstream.serve_forever, daemon=True).start()
with socket.socket() as reserve:
    reserve.bind(("127.0.0.1", 0))
    core_port = reserve.getsockname()[1]
run_dir = Path(tempfile.mkdtemp(prefix="mcp-gateway-core-probe-", dir="/private/tmp"))
config_path = run_dir / "mcp_config.json"
config_path.write_text(json.dumps({"listen": f"127.0.0.1:{core_port}", "mcpServers": [],
    "require_mcp_auth": True, "quarantine_enabled": False,
    "tool_response_limit": 0, "tool_response_mode": "compact", "telemetry": {"enabled": False}, "tokenizer": {"enabled": False},
    "docker_isolation": {"enabled": False}}))
config_path.chmod(0o600)
log = (run_dir / "core.log").open("w")
env = dict(os.environ, CI="true", HEADLESS="true", MCPPROXY_TELEMETRY="false", MCPPROXY_KEYRING_WRITE="0")
env["MCPPROXY_API_KEY"]="controlled-management-key"
core = subprocess.Popen([os.environ["MCP_GATEWAY_CORE_BINARY"], "serve", "--config", str(config_path),
    "--data-dir", str(run_dir), "--log-dir", str(run_dir / "logs"), "--enable-socket=false"],
    stdout=log, stderr=subprocess.STDOUT, env=env)
base = f"http://127.0.0.1:{core_port}"
transcript = []
oauth_process = None


def request(path, method="GET", data=None, auth=True, headers=None):
    h = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
    if auth:
        h["X-API-Key"] = "controlled-management-key"
    h.update(headers or {})
    req = urllib.request.Request(base + path, method=method, headers=h,
                                 data=None if data is None else json.dumps(data).encode())
    try:
        response = urllib.request.urlopen(req, timeout=15)
    except urllib.error.HTTPError as exc:
        response = exc
    raw = response.read().decode()
    if raw.startswith("event:"):
        events=[line[6:] for line in raw.splitlines() if line.startswith("data: ")]
        raw=next((event for event in events if isinstance(data,dict) and json.loads(event).get("id")==data.get("id")),events[-1])
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        parsed = raw
    transcript.append({"method": method, "path": path, "request": data, "status": response.status, "response": parsed})
    return response.status, parsed, dict(response.headers)


def call_tool(name, text):
    return request("/api/v1/tools/call", "POST", {"tool_name": "call_tool_read", "arguments": {"name": name, "args": {"text": text}}})


try:
    for attempt in range(80):
        if core.poll() is not None:
            raise RuntimeError(f"core exited: {core.returncode}; see {run_dir}")
        try:
            request("/api/v1/status")
            break
        except (urllib.error.URLError, ConnectionError):
            time.sleep(0.25)
    else:
        raise RuntimeError(f"core did not listen within 20 seconds; see {run_dir}")
    assert request("/api/v1/servers", auth=False)[0] == 401
    for name, connection in [
        ("stdio-probe", {"protocol": "stdio", "command": sys.executable, "args": [str(Path(__file__).resolve()), "stdio"]}),
        ("sse-probe", {"protocol":"sse", "url":f"http://127.0.0.1:{upstream.server_port}/sse", "headers":{"Authorization":"Bearer controlled-upstream-secret"}}),
        ("http-probe", {"protocol": "http", "url": f"http://127.0.0.1:{upstream.server_port}/mcp", "headers": {"Authorization": "Bearer controlled-upstream-secret"}}),
    ]:
        assert request("/api/v1/servers", "POST", {"name": name, "enabled": True, "quarantined": False, **connection})[0] == 200
    for attempt in range(80):
        response = request("/api/v1/servers")[1]
        if all(server["connected"] and server.get("tool_count",0)>0 for server in response["data"]["servers"]):
            break
        time.sleep(0.25)
    print("RUN_DIR", run_dir, flush=True)
    print("SERVERS", json.dumps(response), flush=True)
    for name in ["stdio-probe", "http-probe", "sse-probe"]:
        request(f"/api/v1/servers/{name}/tools")
        result = call_tool(name + ":echo", "hello core")
        assert result[0] == 200 and "hello core" in json.dumps(result[1]), result
    request("/api/v1/tools")
    request("/api/v1/routing")
    request("/api/v1/config")
    for path in ["/mcp/all", "/mcp/call"]:
        msg = {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "controlled-probe", "version": "1"}}}
        status, data, headers = request(path, "POST", msg)
        session = {"Mcp-Session-Id": headers.get("Mcp-Session-Id", ""), "MCP-Protocol-Version": "2025-11-25"}
        request(path, "POST", {"jsonrpc": "2.0", "method": "notifications/initialized"}, headers=session)
        request(path, "POST", {"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, headers=session)
        if path == "/mcp/all":
            result = request(path, "POST", {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "stdio-probe__echo", "arguments": {"text": "direct works"}}}, headers=session)
            assert result[0] == 200 and "direct works" in json.dumps(result[1]), result
        else:
            request(path, "POST", {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "retrieve_tools", "arguments": {"query": "echo text"}}}, headers=session)
            request(path, "POST", {"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": {"name": "describe_tool", "arguments": {"tool_ids": ["stdio-probe:echo"]}}}, headers=session)
    full = request("/api/v1/tools/call", "POST", {"tool_name":"call_tool_read", "full_result":True, "arguments":{"name":"stdio-probe:echo","args":{"text":"full-result"}}})
    assert full[0] == 200 and full[1]["data"]["structuredContent"]["echo"] == "full-result", full
    assert not full[1]["data"].get("isError", False), full
    error_result=request("/api/v1/tools/call", "POST", {"tool_name":"call_tool_read", "full_result":True, "arguments":{"name":"stdio-probe:echo","args":{"text":"upstream-error"}}})
    assert error_result[0] == 200 and error_result[1]["data"]["isError"] and error_result[1]["data"]["structuredContent"]["echo"]=="upstream-error", error_result
    management_pid = full[1]["data"]["structuredContent"]["pid"]
    assert not full[1]["data"]["structuredContent"]["gateway_secret_in_env"], "management key inherited by stdio"
    token = request("/api/v1/tokens", "POST", {"name":"controlled-agent", "allowed_servers":["*"], "permissions":["read","write","destructive"], "expires_in":"30d"})
    assert token[0] == 201, token
    token_value = token[1]["data"].get("token", token[1]["data"].get("raw_token"))
    assert token_value, token
    agent_headers={"Authorization":"Bearer "+token_value}
    assert request("/api/v1/gateway", auth=False, headers=agent_headers)[0] == 403
    agent_sessions=[]
    pids=[]
    for i in range(2):
        initial = request("/mcp/call", "POST", {"jsonrpc":"2.0", "id":100+i, "method":"initialize", "params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"isolation-agent","version":"1"}}}, auth=False, headers=agent_headers)
        headers=dict(agent_headers, **{"Mcp-Session-Id":initial[2]["Mcp-Session-Id"],"MCP-Protocol-Version":"2025-11-25"})
        agent_sessions.append(headers)
        listed=request("/mcp/call", "POST", {"jsonrpc":"2.0","id":200+i,"method":"tools/list"}, auth=False, headers=headers)
        names=[tool["name"] for tool in listed[1]["result"]["tools"]]
        assert set(names)=={"call_tool_read","call_tool_write","call_tool_destructive","retrieve_tools","describe_tool","read_cache"}, names
        result=request("/mcp/call", "POST", {"jsonrpc":"2.0","id":300+i,"method":"tools/call","params":{"name":"call_tool_read","arguments":{"name":"stdio-probe:echo","args":{"text":"isolated"}}}}, auth=False, headers=headers)
        structured=result[1]["result"]["structuredContent"]
        assert structured["counter"]==1, result
        pids.append(structured["pid"])
    assert len(set(pids+[management_pid]))==3, (pids,management_pid)
    again=request("/mcp/call", "POST", {"jsonrpc":"2.0","id":400,"method":"tools/call","params":{"name":"call_tool_read","arguments":{"name":"stdio-probe:echo","args":{"text":"same session"}}}}, auth=False, headers=agent_sessions[0])
    assert again[1]["result"]["structuredContent"]["counter"]==2, again
    for upstream_name in ["http-probe","sse-probe"]:
        ids=[]
        for headers in agent_sessions:
            result=request("/mcp/call", "POST", {"jsonrpc":"2.0","id":450,"method":"tools/call","params":{"name":"call_tool_read","arguments":{"name":upstream_name+":echo","args":{"text":"network isolation"}}}}, auth=False, headers=headers)
            value=result[1]["result"]["structuredContent"]
            assert value["counter"]==1, result
            ids.append(value["upstream_session"])
        assert len(set(ids))==2, ids
        repeated=request("/mcp/call", "POST", {"jsonrpc":"2.0","id":451,"method":"tools/call","params":{"name":"call_tool_read","arguments":{"name":upstream_name+":echo","args":{"text":"network persistence"}}}}, auth=False, headers=agent_sessions[0])
        assert repeated[1]["result"]["structuredContent"]["counter"]==2, repeated
        print("NETWORK_ISOLATION",upstream_name,ids,flush=True)
    pause=request("/api/v1/gateway", "POST", {"paused":True})
    assert pause[1]["data"]["paused"] is True, pause
    blocked=request("/api/v1/tools/call", "POST", {"tool_name":"call_tool_read","full_result":True,"arguments":{"name":"stdio-probe:echo","args":{"text":"paused"}}})
    assert blocked[1]["data"].get("isError") is True and "GATEWAY_PAUSED" in json.dumps(blocked), blocked
    request("/api/v1/gateway", "POST", {"paused":False})
    added=request("/api/v1/servers", "POST", {"name":"shared-probe","protocol":"stdio","command":sys.executable,"args":[str(Path(__file__).resolve()),"stdio"],"session_mode":"shared","enabled":True,"quarantined":False})
    assert added[0]==200,added
    for attempt in range(80):
        shared=next(server for server in request("/api/v1/servers")[1]["data"]["servers"] if server["name"]=="shared-probe")
        if shared["connected"] and shared.get("tool_count",0)>0: break
        time.sleep(.1)
    assert shared["session_mode"]=="shared",shared
    shared_pids=[]
    for i,headers in enumerate(agent_sessions):
        result=request("/mcp/call", "POST", {"jsonrpc":"2.0","id":470+i,"method":"tools/call","params":{"name":"call_tool_read","arguments":{"name":"shared-probe:echo","args":{"text":"shared opt-in"}}}}, auth=False, headers=headers)
        value=result[1]["result"]["structuredContent"]
        shared_pids.append(value["pid"])
        assert value["counter"]==i+1,result
    assert len(set(shared_pids))==1,shared_pids
    print("SHARED_OPT_IN",shared_pids,flush=True)
    count_before=request("/api/v1/gateway")[1]["data"]["isolated_sessions"]
    for headers in agent_sessions: request("/mcp/call", "DELETE", auth=False, headers=headers)
    for attempt in range(50):
        count_after=request("/api/v1/gateway")[1]["data"]["isolated_sessions"]
        if count_after == count_before-6: break
        time.sleep(.1)
    assert count_after==count_before-6,(count_before,count_after)
    print("DOWNSTREAM_SESSION_CLEANUP",count_before,count_after,flush=True)
    switched=request("/api/v1/servers/shared-probe", "PATCH", {"session_mode":"isolated"})
    assert switched[0]==200,switched
    request("/api/v1/servers/shared-probe/restart","POST",{})
    for attempt in range(80):
        shared=next(server for server in request("/api/v1/servers")[1]["data"]["servers"] if server["name"]=="shared-probe")
        if shared["connected"] and shared["session_mode"]=="isolated": break
        time.sleep(.1)
    assert shared["session_mode"]=="isolated",shared
    switched_call=request("/api/v1/tools/call", "POST", {"tool_name":"call_tool_read","full_result":True,"arguments":{"name":"shared-probe:echo","args":{"text":"switched isolated"}}})
    assert switched_call[1]["data"]["structuredContent"]["pid"]!=shared_pids[0],switched_call
    assert next(server for server in json.loads(config_path.read_text())["mcpServers"] if server["name"]=="shared-probe")["session_mode"]=="isolated"
    print("SESSION_MODE_SWITCH_PERSISTED",flush=True)
    assert not json.loads(config_path.read_text()).get("api_key"), "management key persisted in config"
    print("ENVIRONMENT_MANAGEMENT_KEY_NOT_PERSISTED",flush=True)
    print("PATCHED_ISOLATION", json.dumps({"pids":pids,"management_pid":management_pid,"tools":names}), flush=True)
    request("/api/v1/servers/stdio-probe/tools/echo/enabled", "POST", {"enabled": False})
    result = call_tool("stdio-probe:echo", "blocked")
    assert result[0] != 200 or result[1]["data"].get("isError"), result
    request("/api/v1/servers/stdio-probe/tools/echo/enabled", "POST", {"enabled": True})
    request("/api/v1/servers/stdio-probe/disable", "POST", {})
    result = call_tool("stdio-probe:echo", "blocked")
    assert result[0] != 200 or result[1]["data"].get("isError"), result
    request("/api/v1/servers/stdio-probe/enable", "POST", {})
    request("/api/v1/servers/http-probe", "PATCH", {"headers": {"X-Test": "probe"}})
    request("/api/v1/servers/http-probe/restart", "POST", {})
    request("/api/v1/activity?limit=10")
    request("/api/v1/servers/http-probe", "DELETE")
    if len(sys.argv) > 1 and sys.argv[1] == "oauth":
        with socket.socket() as reserve:
            reserve.bind(("127.0.0.1", 0))
            oauth_port = reserve.getsockname()[1]
        oauth_process = subprocess.Popen([os.environ["MCP_GATEWAY_OAUTH_FIXTURE_BINARY"], "-port", str(oauth_port), "-access-token-ttl", "30s"], stdout=log, stderr=subprocess.STDOUT, env=env)
        for attempt in range(60):
            try:
                urllib.request.urlopen(f"http://127.0.0.1:{oauth_port}/.well-known/oauth-authorization-server", timeout=2).close()
                break
            except urllib.error.URLError:
                time.sleep(.25)
        servers = json.loads(config_path.read_text())["mcpServers"]
        servers.append({"name": "oauth-probe", "protocol": "http", "url": f"http://127.0.0.1:{oauth_port}/mcp", "oauth": {}, "enabled": True, "quarantined": False})
        result = request("/api/v1/config", "PATCH", {"mcpServers": servers})
        assert result[0] == 200, result
        time.sleep(1)
        result = request("/api/v1/servers/oauth-probe/login", "POST", {})
        assert result[0] == 200 and result[1]["data"]["auth_url"], result
        cancelled_auth_url = result[1]["data"]["auth_url"]
        cancelled = request("/api/v1/servers/oauth-probe/oauth/cancel", "POST", {})
        assert cancelled[0] == 200 and cancelled[1]["data"]["cancelled"] is True, cancelled
        stale_fields = urllib.parse.parse_qs(urllib.parse.urlsplit(cancelled_auth_url).query)
        stale_fields.update(username=["testuser"], password=["testpass"], consent=["on"], action=["approve"])
        try:
            stale = urllib.request.urlopen(urllib.request.Request(cancelled_auth_url.split("?")[0], data=urllib.parse.urlencode(stale_fields,doseq=True).encode(), headers={"Content-Type":"application/x-www-form-urlencoded"}),timeout=15)
            assert stale.status >= 400, stale.status
        except urllib.error.HTTPError as exc:
            assert exc.code >= 400, exc.code
        result = request("/api/v1/servers/oauth-probe/login", "POST", {})
        assert result[0] == 200 and result[1]["data"]["auth_url"], result
        auth_url = result[1]["data"]["auth_url"]
        fields = urllib.parse.parse_qs(urllib.parse.urlsplit(auth_url).query)
        fields.update(username=["testuser"], password=["testpass"], consent=["on"], action=["approve"])
        form = urllib.parse.urlencode(fields, doseq=True).encode()
        authorization = urllib.request.urlopen(urllib.request.Request(auth_url.split("?")[0], data=form, headers={"Content-Type": "application/x-www-form-urlencoded"}), timeout=15)
        print("OAUTH_CALLBACK", authorization.status, flush=True)
        for attempt in range(80):
            servers = request("/api/v1/servers")[1]["data"]["servers"]
            oauth_server = next(s for s in servers if s["name"] == "oauth-probe")
            if oauth_server["connected"] and oauth_server["authenticated"]:
                break
            time.sleep(.25)
        assert oauth_server["connected"] and oauth_server["authenticated"], oauth_server
        key_account = "oauth-master:"+hashlib.sha256(str(run_dir.resolve()).encode()).hexdigest()[:32]
        key_present = subprocess.run(["/usr/bin/security","find-generic-password","-s","com.mcp-gateway.credentials","-a",key_account],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        assert key_present.returncode == 0, "test-only OAuth master key absent"
        print("OAUTH_KEYCHAIN_PRESENT_AND_CANCELLED_CALLBACK_REJECTED",flush=True)
        refreshed = request("/api/v1/servers/oauth-probe/oauth/refresh", "POST", {})
        assert refreshed[0] == 200 and refreshed[1]["data"]["refreshed"], refreshed
        oauth_server = next(s for s in request("/api/v1/servers")[1]["data"]["servers"] if s["name"] == "oauth-probe")
        oauth_sessions=[]
        for i in range(2):
            initial=request("/mcp/call","POST",{"jsonrpc":"2.0","id":800+i,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"concurrent-oauth-agent","version":"1"}}},auth=False,headers=agent_headers)
            oauth_sessions.append(dict(agent_headers,**{"Mcp-Session-Id":initial[2]["Mcp-Session-Id"],"MCP-Protocol-Version":"2025-11-25"}))
        def oauth_call(i):
            return request("/mcp/call","POST",{"jsonrpc":"2.0","id":900+i,"method":"tools/call","params":{"name":"call_tool_read","arguments":{"name":"oauth-probe:echo","args":{"message":"concurrent OAuth works"}}}},auth=False,headers=oauth_sessions[i%2])
        with ThreadPoolExecutor(max_workers=8) as pool:
            refreshes=[pool.submit(request,"/api/v1/servers/oauth-probe/oauth/refresh","POST",{}) for _ in range(6)]
            calls=[pool.submit(oauth_call,i) for i in range(2)]
            for future in refreshes:
                value=future.result();assert value[0]==200 and value[1]["data"].get("refreshed"),value
            for future in calls:
                value=future.result();assert value[0]==200 and not value[1]["result"].get("isError") and "concurrent OAuth works" in json.dumps(value[1]),value
        print("ROTATING_REFRESH_CONCURRENT_SESSIONS_PASSED",flush=True)
        oauth_server=next(server for server in request("/api/v1/servers")[1]["data"]["servers"] if server["name"]=="oauth-probe")
        initial_expiry = oauth_server["oauth"]["token_expires_at"]
        result = request("/api/v1/tools/call", "POST", {"tool_name": "call_tool_read", "arguments": {"name": "oauth-probe:echo", "args": {"message": "OAuth works"}}})
        assert result[0] == 200 and "OAuth works" in json.dumps(result[1]), result
        print("OAUTH_CONNECTED_EXPIRY", initial_expiry, flush=True)
        for attempt in range(45):
            time.sleep(1)
            oauth_server = next(s for s in request("/api/v1/servers")[1]["data"]["servers"] if s["name"] == "oauth-probe")
            if oauth_server.get("oauth", {}).get("token_expires_at", "") > initial_expiry:
                break
        assert oauth_server.get("oauth", {}).get("token_expires_at", "") > initial_expiry, "automatic refresh did not update expiry"
        print("OAUTH_REFRESHED_EXPIRY", oauth_server["oauth"]["token_expires_at"], flush=True)
        for i in range(2):
            value=oauth_call(i);assert value[0]==200 and not value[1]["result"].get("isError"),value
        for headers in oauth_sessions:request("/mcp/call","DELETE",auth=False,headers=headers)
        core.send_signal(signal.SIGTERM)
        core.wait(timeout=35)
        core = subprocess.Popen(core.args, stdout=log, stderr=subprocess.STDOUT, env=env)
        for attempt in range(80):
            try:
                oauth_server = next(s for s in request("/api/v1/servers")[1]["data"]["servers"] if s["name"] == "oauth-probe")
                if oauth_server["connected"] and oauth_server["authenticated"]:
                    break
            except (urllib.error.URLError, ConnectionError, StopIteration):
                pass
            time.sleep(.25)
        assert oauth_server["connected"] and oauth_server["authenticated"], oauth_server
        print("OAUTH_RESTART_RECOVERED", flush=True)
        result = request("/api/v1/servers/oauth-probe/logout", "POST", {})
        assert result[0] == 200, result
        oauth_server = next(s for s in request("/api/v1/servers")[1]["data"]["servers"] if s["name"] == "oauth-probe")
        assert not oauth_server["authenticated"] and not oauth_server["connected"], oauth_server
        print("OAUTH_LOGOUT_CLEARED", flush=True)
finally:
    (run_dir / "transcript.json").write_text(json.dumps(transcript, indent=2))
    core.send_signal(signal.SIGTERM)
    try:
        core.wait(timeout=35)
    except subprocess.TimeoutExpired:
        core.kill()
        core.wait()
    upstream.shutdown()
    if oauth_process is not None:
        oauth_process.terminate()
        oauth_process.wait(timeout=5)
    if len(sys.argv)>1 and sys.argv[1]=="oauth":
        key_account="oauth-master:"+hashlib.sha256(str(run_dir.resolve()).encode()).hexdigest()[:32]
        removed=subprocess.run(["/usr/bin/security","delete-generic-password","-s","com.mcp-gateway.credentials","-a",key_account],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        print("TEST_KEYCHAIN_ITEM_REMOVAL",removed.returncode,flush=True)
    log.close()
    print("CORE_EXIT", core.returncode, "TRANSCRIPT", run_dir / "transcript.json", flush=True)
