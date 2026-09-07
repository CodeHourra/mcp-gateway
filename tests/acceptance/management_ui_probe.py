"""Exercise management UI in an isolated real WKWebView; never uses real agent config."""
import argparse, hashlib, json, os, plistlib, secrets, shutil, socket, subprocess, sys, time
from pathlib import Path
from wails_mcp import call

root = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('stage', type=Path)
parser.add_argument('--hierarchy', action='store_true', help='Check the 0.1.8 collapsed hierarchy and grouped agents')
args = parser.parse_args()
stage = args.stage.resolve()
assert stage.parent == root / '.cache' and stage.name.startswith('management-check.')
profile, home = stage / 'profile', stage / 'home'
profile.mkdir(exist_ok=True); home.mkdir(exist_ok=True)
(profile/'core').mkdir(exist_ok=True)
bundle = stage / 'MCP Gateway Management Check.app'
shutil.copytree(root / 'bin/MCP Gateway.app', bundle, dirs_exist_ok=True)
shutil.copy2(stage / 'mcp-gateway', bundle / 'Contents/MacOS/mcp-gateway')
info_path = bundle / 'Contents/Info.plist'
info = plistlib.loads(info_path.read_bytes())
info['CFBundleIdentifier'] = 'com.mcp-gateway.management-check'
info['NSAppTransportSecurity'] = {'NSAllowsLocalNetworking': True}
info_path.write_bytes(plistlib.dumps(info))
subprocess.run(['codesign', '--force', '--deep', '--sign', '-', str(bundle)], check=True, capture_output=True)
fixture = stage / 'stdio_fixture.py'
fixture.write_text('''import json,sys
for line in sys.stdin:
 r=json.loads(line)
 if 'id' not in r: continue
 m=r.get('method')
 result={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'management-fixture','version':'1'}} if m=='initialize' else {'tools':[{'name':'echo','description':'Echo fixture for management acceptance','inputSchema':{'type':'object','properties':{}}}]} if m=='tools/list' else {'content':[{'type':'text','text':'ok'}]}
 print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':result}),flush=True)
''')
config = {'listen':'127.0.0.1:19245', 'mcpServers':[
 {'name':'saved-service','command':sys.executable,'args':[str(fixture)],'env':{'QA_PATH':'${env:PATH}'},'enabled':True,'protocol':'stdio','session_mode':'isolated','quarantined':False,'trust_mode':'auto'},
 {'name':'disabled-service','command':sys.executable,'args':[str(fixture)],'enabled':False,'protocol':'stdio','session_mode':'isolated','quarantined':False,'trust_mode':'auto'}],
 'telemetry':{'enabled':False},'docker_isolation':{'enabled':False},'enable_socket':False,'logging':{'enable_console':False,'enable_file':True,'log_dir':str(profile/'logs'),'filename':'core.log','json_format':True}}
(profile/'core.json').write_text(json.dumps(config))
(profile/'settings.json').write_text(json.dumps({'theme':'system','mode':'progressive','listenAddress':config['listen'],'launchAtLogin':False,'logRetentionDays':1}))
exe = str(bundle/'Contents/MacOS/mcp-gateway')
entry = {'type':'stdio','command':exe,'args':['connect','--client','cursor','--data-dir',str(profile)]}
(home/'.cursor').mkdir(exist_ok=True)
cursor_original = {'mcpServers':{'mcp-gateway':entry,'unrelated':{'command':'unchanged'}},'keep':'preserved'}
(home/'.cursor/mcp.json').write_text(json.dumps(cursor_original))
(home/'.codex').mkdir(exist_ok=True)
(home/'.codex/config.toml').write_text('[mcp_servers.mcp-gateway]\ncommand = "/old/MCP Gateway.app/Contents/MacOS/mcp-gateway"\nargs = '+json.dumps(['connect','--client','codex','--data-dir',str(profile)])+'\n')
(home/'.codebuddy').mkdir(exist_ok=True)
(home/'.codebuddy/mcp.json').write_text(json.dumps({'mcpServers':{'mcp-gateway':{'command':'other-service'}}}))
claude = dict(entry, args=['connect','--client','claude-code','--data-dir',str(profile)], disabled=True)
(home/'.claude.json').write_text(json.dumps({'mcpServers':{'mcp-gateway':claude}}))
env = dict(os.environ, HOME=str(home), CODEX_HOME=str(home/'.codex'), CI='true', HEADLESS='true', MCP_GATEWAY_DATA_DIR=str(profile), MCP_GATEWAY_QA_ADMIN_KEY=secrets.token_hex(32), WAILS_MCP_PORT='19199')
for name in ['PI_CODING_AGENT_DIR','OMP_PROFILE','PI_PROFILE','PI_CONFIG_DIR','CODEBUDDY_CONFIG_DIR']:
 env.pop(name, None)
report = {'passed':False, 'scope':'Actual isolated WKWebView, unchanged frontend/backend except test app identity and admin key. Real bundled core, local stdio fixture and temporary agent configs. No real providers, client handshake, OS file-dialog or pixel verification.', 'appSHA256':hashlib.sha256(Path(exe).read_bytes()).hexdigest(), 'coreSHA256':hashlib.sha256((bundle/'Contents/MacOS/mcpproxy').read_bytes()).hexdigest()}
app = None
endpoint = 'http://127.0.0.1:19199/mcp'
def js(source): return call(endpoint,'js_eval',{'window':'manager','js':source,'timeout_ms':15000})
def until(source, check=lambda x: x is True):
 value = None
 for _ in range(150):
  try:
   value=js(source)
   if check(value): return value
  except Exception: pass
  time.sleep(.15)
 raise AssertionError('UI readiness failed: '+str(value))
def button(label):
 js('const e=[...document.querySelectorAll("button, .choice-option")].find(e=>e.offsetParent!==null && e.textContent.trim()==='+json.dumps(label)+');if(!e||e.disabled||e.querySelector("input:disabled"))throw new Error("button unavailable: '+label+'");e.click();return true')
def nav(label):
 js('const e=[...document.querySelectorAll(".nav-button")].find(e=>e.textContent.includes('+json.dumps(label)+'));e.click();return true')
try:
 for port in (19199,19245):
  with socket.socket() as probe: assert probe.connect_ex(('127.0.0.1',port)) != 0, 'Test port already used'
 with (stage/'output.log').open('w') as log:
  app=subprocess.Popen([exe],env=env,stdout=log,stderr=log)
  until('return !!window.gateway')
  if args.hierarchy:
   until("return document.querySelectorAll('.service-group .tool-entry').length>0")
   report['collapsedDefault']=js("return {groups:document.querySelectorAll('.service-group').length,visibleTools:[...document.querySelectorAll('.service-tools-panel')].filter(e=>e.offsetParent!==null).length,inspector:!!document.querySelector('.inspector'),buttons:[...document.querySelectorAll('.service-row')].map(e=>e.getAttribute('aria-expanded'))}")
   assert report['collapsedDefault']=={'groups':2,'visibleTools':0,'inspector':False,'buttons':['false','false']}
   js("[...document.querySelectorAll('.service-row')].find(e=>e.textContent.includes('saved-service')).click();return true")
   report['expandedTools']=until("const p=[...document.querySelectorAll('.service-tools-panel')].find(e=>e.offsetParent!==null);return {visible:!!p,heading:p?.querySelector('h3')?.textContent,grouped:!!p?.querySelector('.tools-list-grouped'),inspector:!!document.querySelector('.inspector')}",lambda v:v.get('visible') and v.get('grouped'))
   assert 'saved-service' in report['expandedTools']['heading'] and not report['expandedTools']['inspector']
   js("const p=document.querySelector('.service-tools-panel .tool-entry');p.querySelector('summary').click();const e=p.querySelector('textarea');e.value='{\"retained\":true}';e.dispatchEvent(new Event('input',{bubbles:true}));[...document.querySelectorAll('.service-row')].find(e=>e.textContent.includes('saved-service')).click();return true")
   js("[...document.querySelectorAll('.service-row')].find(e=>e.textContent.includes('saved-service')).click();return true")
   report['toolDraftPreserved']=js("return document.querySelector('.service-tools-panel .tool-entry textarea').value.includes('retained')")
   assert report['toolDraftPreserved']
   call(endpoint,'window_control',{'window':'manager','action':'set_size','width':1040,'height':780})
   js("[...document.querySelectorAll('.service-config-button')].find(e=>e.getAttribute('aria-label').includes('saved-service')).click();return true")
   report['desktopDetail']=until("const e=document.querySelector('.inspector');return {exists:!!e,focused:document.activeElement===e,tab:document.querySelector('.tab-bar [aria-selected=true]')?.textContent,overflow:document.documentElement.scrollWidth>document.documentElement.clientWidth}",lambda v:v.get('exists') and v.get('focused'))
   assert report['desktopDetail']['tab']=='连接与认证' and not report['desktopDetail']['overflow']
   call(endpoint,'window_control',{'window':'manager','action':'set_size','width':760,'height':560})
  else:
   report['defaultTools']=until("return {groups:document.querySelectorAll('.service-group').length,tools:document.querySelectorAll('.service-group .tool-entry').length,inspector:!!document.querySelector('.inspector'),text:document.body.textContent.slice(-2000)} ",lambda v:v.get('groups')==2 and v.get('tools',0)>0)
   assert not report['defaultTools']['inspector']
   assert '已禁用' in report['defaultTools']['text'] and 'idle' not in report['defaultTools']['text']
   call(endpoint,'window_control',{'window':'manager','action':'set_size','width':760,'height':560})
   js("document.querySelector('.service-row').click();return true")
  report['expandedDetail']=until("return {exists:!!document.querySelector('.inspector'),focused:document.activeElement===document.querySelector('.inspector')} ",lambda v:v.get('exists') and v.get('focused'))
  button('收起详情');until("return !document.querySelector('.inspector')")
  button('JSON 配置')
  until("return document.querySelector('.full-json')?.value.includes('saved-service')")
  report['maskedJSON']=js("const s=document.querySelector('.full-json').value;return {stored:s.includes('${stored:'),noRawRef:!s.includes('${env:PATH}')} ")
  assert all(report['maskedJSON'].values())
  js("const e=document.querySelector('.full-json');window.__qaOriginal=e.value;e.value='{';e.dispatchEvent(new Event('input',{bubbles:true}));return true")
  button('校验并预览变更');until("return !!document.querySelector('.full-config-editor .error-text')?.textContent")
  report['invalidDraftRetained']=js("return document.querySelector('.full-json').value==='{' ");assert report['invalidDraftRetained']
  nav('Agent 接入');until("return [...document.querySelectorAll('.wide-row, .agent-card')].some(e=>e.textContent.includes('Cursor'))")
  report['narrowLayout']=js("return {width:innerWidth,scrollWidth:document.documentElement.scrollWidth,clientWidth:document.documentElement.clientWidth,agentNameWidths:[...document.querySelectorAll('.wide-row .grow, .agent-card')].map(e=>e.getBoundingClientRect().width)}")
  assert report['narrowLayout']['scrollWidth']<=report['narrowLayout']['clientWidth']
  report['agentStatus']=js("return [...document.querySelectorAll('.wide-row, .agent-card')].map(e=>({name:e.querySelector('strong, h3')?.textContent,status:e.querySelector('.badge')?.textContent}))")
  states={v['name']:v['status'] for v in report['agentStatus']}
  assert states['Cursor']=='已配置接入' and ('需更新' in states['Codex']) and states['CodeBuddy CLI']=='配置冲突' and '客户端已禁用' in states['Claude Code']
  if args.hierarchy:
   report['agentGroups']=js("return [...document.querySelectorAll('.agent-section')].map(e=>({title:e.querySelector('h2').textContent,agents:[...e.querySelectorAll('.agent-card')].map(c=>c.dataset.agentId)}))")
   assert [v['agents'] for v in report['agentGroups']]==[['cursor'],['claude-code','codebuddy','codex'],['omp']]
  js("const r=[...document.querySelectorAll('.wide-row, .agent-card')].find(e=>e.textContent.includes('Cursor'));[...r.querySelectorAll('button')].find(e=>e.textContent==='解除接入').click();return true")
  until("return document.body.textContent.includes('解除接入预览')")
  button('备份并解除接入');until("return [...document.querySelectorAll('.wide-row, .agent-card')].find(e=>e.textContent.includes('Cursor'))?.querySelector('.badge')?.textContent==="+json.dumps('未接入' if args.hierarchy else '尚未配置'))
  after=json.loads((home/'.cursor/mcp.json').read_text());assert after=={'mcpServers':{'unrelated':{'command':'unchanged'}},'keep':'preserved'}
  report['agentDisconnectPreservedOtherConfig']=True
  nav('MCP 服务');report['draftSurvivedNavigation']=until("return document.querySelector('.full-json').value==='{' ");assert report['draftSurvivedNavigation']
  js("const e=document.querySelector('.full-json');const d=JSON.parse(window.__qaOriginal);d.mcpServers=d.mcpServers.filter(s=>s.name!=='disabled-service');e.value=JSON.stringify(d,null,2);e.dispatchEvent(new Event('input',{bubbles:true}));return true")
  button('校验并预览变更');until("return !!document.querySelector('.full-config-editor .preview-panel')")
  report['removalGuard']=js("return [...document.querySelectorAll('.full-config-editor button')].find(e=>e.textContent==='备份并保存全部配置').disabled")
  assert report['removalGuard']
  js("document.querySelector('.full-config-editor .check-row input').click();return true")
  button('备份并保存全部配置');until("return document.querySelector('.full-config-editor')?.textContent.includes('备份编号')")
  saved=json.loads((profile/'core.json').read_text())
  assert [v['name'] for v in saved['mcpServers']]==['saved-service'] and saved['listen']==config['listen']
  assert saved['mcpServers'][0]['env']['QA_PATH']=='${env:PATH}'
  report['fullConfigSavedWithReferenceAndSettings']=True
  nav('导入配置');button('选择配置文件')
  until("return !!document.querySelector('.file-drop input')")
  report['fileControl']=js("const i=document.querySelector('.file-drop input');i.focus();return {clipped:getComputedStyle(i).clip!=='auto',focused:document.activeElement===i,chinese:document.querySelector('.file-drop').textContent.includes('尚未选择文件')} ")
  assert all(report['fileControl'].values())
  report['fileSelection']=js("const i=document.querySelector('.file-drop input'),d=new DataTransfer();d.items.add(new File(['{\"mcpServers\":{}}'],'示例配置.json',{type:'application/json'}));i.files=d.files;i.dispatchEvent(new Event('change',{bubbles:true}));return true")
  until("return document.querySelector('.file-name')?.textContent==='示例配置.json'")
  button('深色')
  report['darkTheme']=js("return document.documentElement.dataset.theme==='dark' && getComputedStyle(document.documentElement).colorScheme==='dark'");assert report['darkTheme']
  report['passed']=True
finally:
 if app is not None and app.poll() is None:
  children=subprocess.run(['pgrep','-P',str(app.pid)],capture_output=True,text=True).stdout.split()
  app.terminate();report['exitCode']=app.wait(timeout=55)
  report['ownedChildrenGone']=all(subprocess.run(['kill','-0',p],capture_output=True).returncode!=0 for p in children)
 (root/('tests/acceptance/hierarchy-ui-0.1.8-2026-09-07.json' if args.hierarchy else 'tests/acceptance/management-ui-0.1.7-2026-09-07.json')).write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps(report,ensure_ascii=False,indent=2))
