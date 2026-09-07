import json,os,socket,subprocess,tempfile,time,urllib.request
from pathlib import Path
with socket.socket() as s:
 s.bind(('127.0.0.1',0)); port=s.getsockname()[1]
d=Path(tempfile.mkdtemp(prefix='mcp-full-check-',dir='/private/tmp')); p=d/'core.json'
p.write_text(json.dumps({'listen':f'127.0.0.1:{port}','mcpServers':[],'api_key':'isolated-test-key','require_mcp_auth':True,'quarantine_enabled':False,'telemetry':{'enabled':False},'tokenizer':{'enabled':False},'docker_isolation':{'enabled':False}}))
env=dict(os.environ,CI='true',HEADLESS='true',MCPPROXY_TELEMETRY='false',MCPPROXY_KEYRING_WRITE='0');env.pop('MCPPROXY_API_KEY',None)
f=(d/'log').open('w'); proc=subprocess.Popen([str(Path(__file__).resolve().parents[2]/'bin/MCP Gateway.app/Contents/MacOS/mcpproxy'),'serve','--config',str(p),'--data-dir',str(d),'--log-dir',str(d/'logs'),'--enable-socket=false'],stdout=f,stderr=f,env=env)
def req(path,method='GET',body=None):
 r=urllib.request.Request(f'http://127.0.0.1:{port}/api/v1/'+path,method=method,headers={'X-API-Key':'isolated-test-key','Content-Type':'application/json'},data=None if body is None else json.dumps(body).encode())
 return json.load(urllib.request.urlopen(r,timeout=10))
try:
 for _ in range(100):
  try: req('servers');break
  except OSError: time.sleep(.1)
 cfg=json.loads(p.read_text()); cfg['mcpServers']=[{'name':'disabled-check','protocol':'stdio','command':'true','enabled':False,'session_mode':'isolated','quarantined':False}]
 v=req('config/validate','POST',cfg); assert v['data']['valid'],v
 req('config','PATCH',{'mcpServers':cfg['mcpServers']});assert len(json.loads(p.read_text())['mcpServers'])==1
 saved=json.loads(p.read_text()); saved['mcpServers']=[]
 v=req('config/validate','POST',saved);assert v['data']['valid'],v
 req('config','PATCH',{'mcpServers':[]});assert json.loads(p.read_text())['mcpServers']==[]
 assert json.loads(p.read_text())['listen']==f'127.0.0.1:{port}'
 print('PASS real core validate, add disabled service, empty-array replacement, non-MCP preservation', d)
finally:
 proc.terminate();proc.wait(timeout=20);f.close()
