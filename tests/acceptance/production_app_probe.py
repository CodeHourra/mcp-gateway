import argparse, base64, hashlib, json, os, socket, subprocess, tempfile, time, urllib.request
from pathlib import Path
root = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description='Check an actual production bundle in an isolated profile.')
parser.add_argument('--app', type=Path, default=root / 'bin/MCP Gateway.app')
parser.add_argument('--report', type=Path, default=root / 'tests/acceptance/production-app-2026-09-07.json')
args = parser.parse_args()
app = args.app.resolve() / 'Contents/MacOS/mcp-gateway'
profile = Path(tempfile.mkdtemp(prefix='production-', dir=root / '.cache'))
with socket.socket() as reserve:
    reserve.bind(('127.0.0.1', 0))
    port = reserve.getsockname()[1]
settings = {'theme':'system','mode':'progressive','listenAddress':f'127.0.0.1:{port}','launchAtLogin':False,'logRetentionDays':1}
(profile / 'settings.json').write_text(json.dumps(settings))
(profile / 'settings.json').chmod(0o600)
account = hashlib.sha256(str(profile).encode()).hexdigest()[:16] + '-admin'
report = {'scope':'Actual production app startup, one owned core, second-instance activation, SIGTERM shutdown, and no debug listener; no menu or pixel claims', 'appSHA256':hashlib.sha256(app.read_bytes()).hexdigest(), 'coreSHA256':hashlib.sha256(app.with_name('mcpproxy').read_bytes()).hexdigest(), 'profile':str(profile)}
app_process = None
key = None
core_pid = None

def api(path):
    request = urllib.request.Request(f'http://127.0.0.1:{port}{path}', headers={'X-API-Key':key})
    with urllib.request.urlopen(request, timeout=2) as response:
        return json.load(response)

def children(pid):
    result = subprocess.run(['pgrep','-P',str(pid)], capture_output=True, text=True)
    return [int(line) for line in result.stdout.splitlines()]

try:
    with (profile/'app-output.log').open('w') as log:
        native_env = dict(os.environ, MCP_GATEWAY_DATA_DIR=str(profile), HEADLESS='true')
        native_env.pop('CI', None)  # CI disables the core's real macOS Keychain provider.
        app_process = subprocess.Popen([str(app)], env=native_env, stdout=log,stderr=log)
        deadline = time.monotonic()+45
        while time.monotonic()<deadline:
            if app_process.poll() is not None:
                raise RuntimeError('Production app exited before readiness')
            if key is None:
                result = subprocess.run(['security','find-generic-password','-w','-s','MCP Gateway','-a',account],capture_output=True,text=True)
                if result.returncode==0:
                    key=result.stdout.strip()
                    if key.startswith('go-keyring-base64:'):
                        key=base64.b64decode(key.removeprefix('go-keyring-base64:')).decode()
                    elif key.startswith('go-keyring-encoded:'):
                        key=bytes.fromhex(key.removeprefix('go-keyring-encoded:')).decode()
            if key:
                try:
                    api('/api/v1/servers')
                    break
                except Exception:
                    pass
            time.sleep(.25)
        else:
            raise RuntimeError('Production app did not become ready')
        owned = children(app_process.pid)
        names = {pid:subprocess.check_output(['ps','-p',str(pid),'-o','comm='],text=True).strip() for pid in owned}
        cores = [pid for pid,name in names.items() if name.endswith('/mcpproxy') or name=='mcpproxy']
        assert len(cores)==1, 'Expected exactly one owned core'
        core_pid=cores[0]
        report.update({'appPID':app_process.pid,'corePID':core_pid,'ready':True,'ownedCoreCount':1})
        second = subprocess.run([str(app)], env=native_env,stdout=log,stderr=log,timeout=15)
        assert second.returncode==0
        assert children(app_process.pid)==owned
        api('/api/v1/servers')
        report['secondInstanceExitCode']=second.returncode
        report['sameCoreAfterSecondLaunch']=True
        cfg=json.loads((profile/'core.json').read_text())
        assert not cfg.get('api_key'), 'Admin key persisted in ordinary configuration'
        report['adminKeyAbsentFromConfig']=True
        assert cfg['telemetry']['enabled'] is False
        assert cfg['docker_isolation']['enabled'] is False
        assert cfg['direct_tool_response_mode']=='full'
        report['explicitCoreDefaults']={'telemetry':False,'dockerIsolation':False,'directToolResponseMode':'full'}
        with socket.socket() as probe:
            report['debugPort19199Closed']=probe.connect_ex(('127.0.0.1',19199))!=0
        assert report['debugPort19199Closed']
        app_process.terminate()
        report['appExitCode']=app_process.wait(timeout=55)
        gone = subprocess.run(['kill','-0',str(core_pid)],capture_output=True).returncode!=0
        report['ownedCoreExited']=gone
        assert gone
        report['passed']=True
finally:
    if app_process is not None and app_process.poll() is None:
        app_process.terminate()
        try: app_process.wait(timeout=55)
        except subprocess.TimeoutExpired: report['cleanupPending']=True
    # This account is generated only by the fresh profile above; never touch other app entries.
    cleanup = subprocess.run(['security','delete-generic-password','-s','MCP Gateway','-a',account],capture_output=True)
    report['temporaryAdminKeyDeleted']=cleanup.returncode in (0,44)
    key=None
    args.report.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,indent=2))
