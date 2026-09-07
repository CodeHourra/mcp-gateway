"""0.1.4 isolated WKWebView and AppKit activation-policy check; no user service/config access."""
import json,os,secrets,subprocess,sys,time,hashlib,shutil,plistlib
from pathlib import Path
from wails_mcp import call
root=Path(__file__).resolve().parents[2]
stage=Path(sys.argv[1]).resolve()
assert stage.parent==root/'.cache' and stage.name.startswith('path-dock-check.')
bundle=stage/'MCP Gateway UX Check.app'
shutil.copytree(root/'bin/MCP Gateway.app',bundle,dirs_exist_ok=True)
shutil.copy2(stage/'mcp-gateway',bundle/'Contents/MacOS/mcp-gateway')
info=bundle/'Contents/Info.plist'
d=plistlib.loads(info.read_bytes());d['CFBundleIdentifier']='com.mcp-gateway.ux-check';d['NSAppTransportSecurity']={'NSAllowsLocalNetworking':True};info.write_bytes(plistlib.dumps(d))
subprocess.run(['codesign','--force','--deep','--sign','-',str(bundle)],check=True,capture_output=True)
profile=stage/'profile';home=stage/'home'
(profile/'core').mkdir(exist_ok=True)
fixture=home/'.local/bin/gateway-uvx-test'
fixture.write_text('#!'+sys.executable+'\n'+'''import json,sys
for line in sys.stdin:
 r=json.loads(line)
 if 'id' not in r: continue
 m=r.get('method')
 result={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'local-path-fixture','version':'1'}} if m=='initialize' else {'tools':[]} if m=='tools/list' else {}
 print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':result}),flush=True)
''');fixture.chmod(0o700)
(profile/'settings.json').write_text(json.dumps({'theme':'system','mode':'progressive','listenAddress':'127.0.0.1:19243','launchAtLogin':False,'logRetentionDays':1}))
(profile/'core.json').write_text(json.dumps({'listen':'127.0.0.1:19243','mcpServers':[{'name':'path-fixture','command':'gateway-uvx-test','protocol':'stdio','enabled':True},{'name':'missing-fixture','command':'gateway-missing-test','protocol':'stdio','enabled':True}],'telemetry':{'enabled':False},'docker_isolation':{'enabled':False},'direct_tool_response_mode':'full'}))
env=dict(os.environ,HOME=str(home),PATH='/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin',SHELL='/bin/zsh',CI='true',HEADLESS='true',MCP_GATEWAY_DATA_DIR=str(profile),MCP_GATEWAY_QA_ADMIN_KEY=secrets.token_hex(32),WAILS_MCP_PORT='19199')
endpoint='http://127.0.0.1:19199/mcp'
report={'scope':'Isolated actual WKWebView and AppKit activation policy. Test-only app identity/admin overlay; no real credentials, providers, OS pixel or menu clicks.','passed':False,'appSHA256':hashlib.sha256((bundle/'Contents/MacOS/mcp-gateway').read_bytes()).hexdigest(),'coreSHA256':hashlib.sha256((bundle/'Contents/MacOS/mcpproxy').read_bytes()).hexdigest()}
app=None

def js(source): return call(endpoint,'js_eval',{'window':'manager','js':source,'timeout_ms':15000})
def until(action,check):
 for _ in range(100):
  try:
   value=action()
   if check(value):return value
  except Exception:pass
  time.sleep(.3)
 raise AssertionError('readiness condition failed: '+str(value))
try:
 with (stage/'output.log').open('w') as log:
  app=subprocess.Popen([str(bundle/'Contents/MacOS/mcp-gateway')],env=env,stdout=log,stderr=log)
  until(lambda:js('return !!window.gateway'),lambda v:v is True)
  snapshot=until(lambda:js("return await window.gateway.request('snapshot')"),lambda v:any(s['name']=='path-fixture' and s['status']=='ready' for s in v.get('services',[])) and any(s.get('statusDetail') for s in v.get('services',[]) if s['name']=='missing-fixture'))
  report['services']=[{k:s.get(k) for k in ('name','status','statusMessage','statusDetail')} for s in snapshot['services']]
  bad=next(s for s in snapshot['services'] if s['name']=='missing-fixture')
  assert '找不到启动命令' in bad['statusMessage'] and 'gateway-missing-test' in bad['statusDetail'] and len(bad['statusDetail'])>50
  until(lambda:js("const e=[...document.querySelectorAll('.service-row')].find(e=>e.textContent.includes('missing-fixture'));if(!e)return false;e.click();return true"),lambda v:v is True)
  until(lambda:js("return !!document.querySelector('.connection-detail')"),lambda v:v is True)
  report['detail']=js("const d=document.querySelector('.connection-detail');d.querySelector('summary').click();return {open:d.open,text:d.querySelector('pre').textContent}")
  assert report['detail']['open'] and report['detail']['text']==bad['statusDetail']
  def policy():return int(subprocess.check_output([str(stage/'policy'),str(app.pid)],text=True))
  report['activationPolicyOpen']=policy();assert report['activationPolicyOpen']==1
  report['closeResult']=call(endpoint,'window_control',{'window':'manager','action':'close'})
  time.sleep(.5)
  assert app.poll() is None
  report['activationPolicyClosed']=policy();assert report['activationPolicyClosed']==1
  report['backendAfterClose']=js("const s=await window.gateway.request('snapshot');return s.services.find(s=>s.name==='path-fixture').status")
  assert report['backendAfterClose']=='ready'
  second=subprocess.run([str(bundle/'Contents/MacOS/mcp-gateway')],env=env,stdout=log,stderr=log,timeout=15);assert second.returncode==0
  report['secondInstanceExit']=second.returncode
  report['reopenWindow']=call(endpoint,'windows_list',{})
  assert len(report['reopenWindow'])==1 and report['reopenWindow'][0]['visible'] and report['reopenWindow'][0]['focused']
  report['passed']=True
finally:
 if app is not None and app.poll() is None:
  children=subprocess.run(['pgrep','-P',str(app.pid)],capture_output=True,text=True).stdout.split()
  app.terminate();report['exitCode']=app.wait(timeout=55)
  report['ownedChildrenGone']=all(subprocess.run(['kill','-0',p],capture_output=True).returncode!=0 for p in children)
 (root/'tests/acceptance/path-dock-0.1.4-2026-09-07.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps(report,ensure_ascii=False,indent=2))
