"""Verify first paint with the real core held behind a test-only startup gate."""
import argparse,hashlib,json,os,plistlib,secrets,shutil,socket,subprocess,sys,time
from pathlib import Path
from wails_mcp import call
root=Path(__file__).resolve().parents[2]
parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('stage',type=Path);args=parser.parse_args();stage=args.stage.resolve()
assert stage.parent==root/'.cache' and stage.name.startswith('startup-check.')
profile=stage/'profile';home=stage/'home';bundle=stage/'MCP Gateway Startup Check.app'
shutil.copytree(root/'bin/MCP Gateway.app',bundle,dirs_exist_ok=True);shutil.copy2(stage/'mcp-gateway',bundle/'Contents/MacOS/mcp-gateway')
p=bundle/'Contents/Info.plist';info=plistlib.loads(p.read_bytes());info['CFBundleIdentifier']='com.mcp-gateway.startup-check';info['NSAppTransportSecurity']={'NSAllowsLocalNetworking':True};p.write_bytes(plistlib.dumps(info));subprocess.run(['codesign','--force','--deep','--sign','-',str(bundle)],check=True,capture_output=True)
fixture=stage/'stdio_fixture.py';fixture.write_text('''import sys,json
for line in sys.stdin:
 r=json.loads(line)
 if 'id' not in r:continue
 result={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'startup-fixture','version':'1'}} if r['method']=='initialize' else {'tools':[]} if r['method']=='tools/list' else {}
 print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':result}),flush=True)
''')
(profile/'settings.json').write_text(json.dumps({'theme':'system','mode':'progressive','listenAddress':'127.0.0.1:19244','launchAtLogin':False,'logRetentionDays':1}))
(profile/'core.json').write_text(json.dumps({'mcpServers':[{'name':'saved-service','command':sys.executable,'args':[str(fixture)],'enabled':True,'protocol':'stdio'},{'name':'disabled-service','command':sys.executable,'args':[str(fixture)],'enabled':False,'protocol':'stdio'}],'listen':'127.0.0.1:19244','enable_socket':False,'telemetry':{'enabled':False},'docker_isolation':{'enabled':False},'logging':{'enable_console':False,'enable_file':True,'log_dir':str(profile/'logs'),'filename':'core.log','json_format':True}}))
assert not (profile/'release-startup').exists()
report={'passed':False,'scope':'Actual isolated WKWebView using unchanged frontend and InitialSnapshot/Snapshot implementations. Test-only overlay changes app identity/admin key and blocks cmd.Start until a gate file exists. No real client configs, credentials, providers or OS pixel claims.','appSHA256':hashlib.sha256((bundle/'Contents/MacOS/mcp-gateway').read_bytes()).hexdigest(),'coreSHA256':hashlib.sha256((bundle/'Contents/MacOS/mcpproxy').read_bytes()).hexdigest()}
endpoint='http://127.0.0.1:19199/mcp';app=None

def js(source):return call(endpoint,'js_eval',{'window':'manager','js':source,'timeout_ms':15000})
def wait(action,condition):
 for _ in range(120):
  try:
   value=action()
   if condition(value):return value
  except Exception:pass
  time.sleep(.1)
 raise AssertionError('UI readiness failed')
try:
 with (stage/'output.log').open('w') as log:
  env=dict(os.environ,HOME=str(home),CI='true',HEADLESS='true',MCP_GATEWAY_DATA_DIR=str(profile),MCP_GATEWAY_QA_ADMIN_KEY=secrets.token_hex(32),WAILS_MCP_PORT='19199')
  started=time.monotonic();app=subprocess.Popen([str(bundle/'Contents/MacOS/mcp-gateway')],env=env,stdout=log,stderr=log)
  before=wait(lambda:js("return {rows:[...document.querySelectorAll('.service-row')].map(e=>e.textContent),empty:document.body.textContent.includes('从第一个 MCP 服务开始'),text:document.querySelector('.toolbar')?.textContent}"),lambda v:len(v.get('rows',[]))==2)
  report['rowsObservedAfterLaunchMs']=round((time.monotonic()-started)*1000,1);report['beforeCoreStart']=before;assert not before['empty']
  with socket.socket() as probe:report['corePortStillClosed']=probe.connect_ex(('127.0.0.1',19244))!=0
  assert report['corePortStillClosed'] and '启动中' in before['text']
  report['localRequest']=js("const t=performance.now();const s=await window.gateway.request('initialSnapshot');return {durationMs:performance.now()-t,source:s.servicesSource,states:s.services.map(v=>({name:v.name,status:v.status})),gatewayStatus:s.gateway.status}")
  assert report['localRequest']['source']=='config' and report['localRequest']['gatewayStatus']=='starting'
  assert any(v['name']=='saved-service' and v['status']=='loading' for v in report['localRequest']['states'])
  release=time.monotonic();(profile/'release-startup').write_text('release')
  after=wait(lambda:js("return {rows:[...document.querySelectorAll('.service-row')].map(e=>e.textContent),text:document.querySelector('.toolbar')?.textContent,banner:document.body.textContent.includes('服务配置已加载，连接状态和工具目录将在后台更新。')}"),lambda v:len(v.get('rows',[]))==2 and '运行中' in v.get('text','') and not v['banner'] and any('可用' in row for row in v['rows']))
  report['runtimeUIAfterReleaseMs']=round((time.monotonic()-release)*1000,1);report['afterCoreStart']=after;report['passed']=True
finally:
 if app is not None and app.poll() is None:
  children=subprocess.run(['pgrep','-P',str(app.pid)],capture_output=True,text=True).stdout.split();app.terminate();report['exitCode']=app.wait(timeout=55);report['ownedChildrenGone']=all(subprocess.run(['kill','-0',p],capture_output=True).returncode!=0 for p in children)
 (root/'tests/acceptance/startup-snapshot-0.1.6-2026-09-07.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report,ensure_ascii=False,indent=2))
