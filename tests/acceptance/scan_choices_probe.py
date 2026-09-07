"""Exercise scan and radio controls only in the dedicated 0.1.2 native QA app."""
import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import time

from wails_mcp import call

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--stage", type=Path, required=True)
parser.add_argument("--report", type=Path, required=True)
args = parser.parse_args()
stage = args.stage.resolve()
assert stage.parent == ROOT / ".cache" and stage.name.startswith("scan-choice-check.")
home, profile = stage / "home", stage / "profile"
bundle = ROOT / "artifacts/MCP Gateway UI Check.app"
endpoint = "http://127.0.0.1:19199/mcp"
markers = ["SCAN_CHOICES_STATIC_SECRET", "SCAN_CHOICES_PARSE_SECRET", "SCAN_CHOICES_COMMENT_SECRET", "SCAN_CHOICES_BUDDY_SECRET", "SCAN_CHOICES_UPDATED_SECRET"]
report = {"executedAt": datetime.now().astimezone().isoformat(), "scope": "Actual isolated WKWebView, public native bridge, label clicks and native window resize. QA-only overlay supplies the admin key from a dedicated environment value; CI=true disables core Keychain access. Production scan/import/UI source is unchanged. No Keychain acceptance, import application, fixture execution, real client configuration, model calls, OS keyboard events or pixel screenshots.", "stage": str(stage), "appSHA256": hashlib.sha256((bundle / "Contents/MacOS/mcp-gateway").read_bytes()).hexdigest(), "coreSHA256": hashlib.sha256((bundle / "Contents/MacOS/mcpproxy").read_bytes()).hexdigest(), "checks": [], "passed": False}
assert report["appSHA256"] == "284f2ea27e6dcdc1f9ee07773fee238dc7e6721624dacea81ac99027d0b3292e"
original_main = (ROOT / "main.go").read_text()
expected_main = original_main.replace('Name: "MCP Gateway",', 'Name: "MCP Gateway UI Check",', 1).replace('UniqueID: "com.mcp-gateway.desktop"', 'UniqueID: "com.mcp-gateway.ui-review"', 1).replace('Title: "MCP Gateway",', 'Title: "MCP Gateway UI Check",', 1)
assert (stage / "main.go").read_text() == expected_main
original_manager = (ROOT / "internal/gateway/manager.go").read_text()
anchor = 'func (m *Manager) Key(name string) (string, error) {\n'
injection = '\tif name == "admin" {\n\t\tif value := os.Getenv("MCP_GATEWAY_QA_ADMIN_KEY"); value != "" {\n\t\t\treturn value, nil\n\t\t}\n\t}\n'
assert original_manager.count(anchor) == 1 and (stage / "manager.go").read_text() == original_manager.replace(anchor, anchor + injection, 1)
report["verifiedTestOverlay"] = {"mainChanges": ["app name", "single-instance ID", "window title"], "managerChange": "Only Key(admin) uses the QA environment value when nonempty; other source bytes equal production.", "hashes": {name: hashlib.sha256((stage / name).read_bytes()).hexdigest() for name in ("main.go", "manager.go", "overlay.json")}, "coreKeychainDisabledByCoordinator": True}


def js(source):
    return call(endpoint, "js_eval", {"window": "manager", "js": source, "timeout_ms": 15000})


def wait_for(expression):
    for _ in range(60):
        if js("return Boolean(" + expression + ")"):
            return
        time.sleep(.1)
    raise AssertionError("Native UI condition timed out: " + expression)


def click_text(text, selector="button"):
    js("const e=[...document.querySelectorAll(" + json.dumps(selector) + ")].find(e=>e.textContent.trim()===" + json.dumps(text) + ");if(!e||e.matches(':disabled'))throw Error('Enabled button missing');e.click();await Promise.resolve();return true")


def navigate(label):
    js("const e=[...document.querySelectorAll('.nav-button')].find(e=>e.textContent.includes(" + json.dumps(label) + "));if(!e)throw Error('Navigation missing');e.click();await Promise.resolve();return true")
    wait_for("document.querySelector('.toolbar-title')?.textContent===" + json.dumps(label))


def choose(legend, value):
    js("const g=[...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent===" + json.dumps(legend) + ");const e=[...g.querySelectorAll('input[type=radio]')].find(e=>e.value===" + json.dumps(str(value)) + ");if(!e||e.matches(':disabled'))throw Error('Enabled radio missing');e.closest('label').click();await Promise.resolve();await Promise.resolve();return true")
    wait_for("[...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent===" + json.dumps(legend) + ")?.querySelector('input:checked')?.value===" + json.dumps(str(value)))


def capture(name):
    value = js("""
const rect=e=>{const r=e.getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,right:r.right,bottom:r.bottom}};
return {theme:document.documentElement.dataset.theme,nativeSelectCount:document.querySelectorAll('select').length,viewport:{width:innerWidth,height:innerHeight,clientWidth:document.documentElement.clientWidth,scrollWidth:document.documentElement.scrollWidth},groups:[...document.querySelectorAll('.choice-group')].map(g=>({legend:g.querySelector('legend').textContent,disabled:g.disabled,bounds:rect(g),boxBorder:getComputedStyle(g.querySelector('.choice-options')).borderWidth,boxShadow:getComputedStyle(g.querySelector('.choice-options')).boxShadow,options:[...g.querySelectorAll('input[type=radio]')].map(e=>({type:e.type,name:e.name,value:e.value,checked:e.checked,disabled:e.matches(':disabled'),label:e.closest('label').querySelector('span').textContent,position:getComputedStyle(e).position,clip:getComputedStyle(e).clip,bounds:rect(e.nextElementSibling),foreground:getComputedStyle(e.nextElementSibling).color,background:getComputedStyle(e.nextElementSibling).backgroundColor}))})),scanRows:[...document.querySelectorAll('.scan-source')].map(e=>({name:e.querySelector('h3').textContent,path:e.querySelector('.address').textContent,badge:e.querySelector('.badge').textContent,text:e.textContent,disabled:e.querySelector('button').disabled})),reviewRows:[...document.querySelectorAll('.review-item')].map(e=>({name:e.querySelector('strong').textContent,text:e.textContent,selected:e.querySelector('input:checked')?.value,disabled:e.querySelector('fieldset').disabled})),scripts:[...document.scripts].map(e=>e.src),stylesheets:[...document.querySelectorAll('link[rel=stylesheet]')].map(e=>e.href)};
""")
    assert value["nativeSelectCount"] == 0
    assert value["viewport"]["scrollWidth"] <= value["viewport"]["clientWidth"]
    names = []
    for group in value["groups"]:
        assert group["legend"] and group["options"]
        assert sum(option["checked"] for option in group["options"]) == 1
        assert len({option["name"] for option in group["options"]}) == 1
        names.append(group["options"][0]["name"])
        assert group["boxBorder"] == "1px" and group["boxShadow"] == "none"
        for option in group["options"]:
            assert option["type"] == "radio" and option["label"] and option["position"] == "absolute"
            assert option["disabled"] == group["disabled"]
            assert option["bounds"]["width"] > 0 and option["bounds"]["height"] > 0
            assert -.5 <= option["bounds"]["x"] and option["bounds"]["right"] <= value["viewport"]["width"] + .5
            assert option["bounds"]["x"] >= group["bounds"]["x"] - .5 and option["bounds"]["right"] <= group["bounds"]["right"] + .5
    assert len(names) == len(set(names))
    assert not any(marker in json.dumps(value) for marker in markers)
    report["checks"].append({"name": name, "value": value})
    print(name, flush=True)
    return value


def three_themes(name):
    for theme in ("light", "dark", "system"):
        choose("主题", theme)
        capture(name + "_" + theme)


def group_values(legend):
    return js("const g=[...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent===" + json.dumps(legend) + ");return [...g.querySelectorAll('input')].map(e=>e.value)")


def source_button(name):
    js("const r=[...document.querySelectorAll('.scan-source')].find(e=>e.querySelector('h3').textContent===" + json.dumps(name) + ");if(!r||r.querySelector('button').disabled)throw Error('Source unavailable');r.querySelector('button').click();return true")
    wait_for("document.querySelectorAll('.review-item').length>0")


def config_state():
    return {name: hashlib.sha256((profile / name).read_bytes()).hexdigest() if (profile / name).exists() else None for name in ("settings.json", "core.json", "services.json")}


listener = subprocess.check_output(["/usr/sbin/lsof", "-nP", "-iTCP:19199", "-sTCP:LISTEN", "-t"], text=True).splitlines()
assert len(set(listener)) == 1
report["appPID"] = int(listener[0])
identity = subprocess.check_output(["ps", "-p", listener[0], "-o", "comm="], text=True).strip()
assert identity == str(bundle / "Contents/MacOS/mcp-gateway")
report["verifiedExecutable"] = identity
adapters = js("return await window.gateway.request('listAgentAdapters')")
assert len(adapters) == 5 and all(Path(a["configPath"]).is_relative_to(home) for a in adapters)
report["verifiedClientPaths"] = {a["id"]: a["configPath"] for a in adapters}
baseline = js("return await window.gateway.request('snapshot')")
assert baseline["settings"]["listenAddress"] == "127.0.0.1:19242"
assert len(baseline["services"]) == 8 and all(not s["enabled"] for s in baseline["services"])
client_before = {p: p.read_bytes() for p in home.rglob("*") if p.is_file() and any(p.is_relative_to(home / d) for d in (".omp", ".cursor", ".codebuddy"))}
client_before[home / ".claude.json"] = (home / ".claude.json").read_bytes()
original_size = call(endpoint, "app_info", {})["windows"][0]
original_theme = js("return document.documentElement.dataset.theme")
before_config = config_state()
new_buddy_path = home / ".codebuddy/.mcp.json"
assert not new_buddy_path.exists() and not (stage / "must-not-execute").exists()
js("""
if(window.__scanChoicesQA)throw Error('QA recorder already installed');
const original=window.gateway.request;
window.__scanChoicesQA={original,counts:{},scans:[],previews:[],settings:[]};
window.gateway.request=async function(method,params){const q=window.__scanChoicesQA;q.counts[method]=(q.counts[method]||0)+1;const result=await original.call(this,method,params);if(method==='scanImportSources')q.scans.push(result);if(method==='previewScannedImport')q.previews.push({params,result});if(method==='saveSettings')q.settings.push({params,retentionType:typeof params.logRetentionDays});return result};return true;
""")
try:
    capture("initial")
    navigate("导入配置")
    wait_for("document.querySelectorAll('.scan-source').length===5")
    first = capture("automatic_scan")
    expected = {"OMP": ("发现配置", False), "Claude Code": ("无法解析", True), "Cursor": ("没有 MCP 服务", True), "CodeBuddy CLI": ("发现配置", False), "Codex": ("暂不可用", True)}
    assert {r["name"]: (r["badge"], r["disabled"]) for r in first["scanRows"]} == expected
    assert group_values("导入方式") == ["scan", "file", "paste"]
    wait_for("(window.__scanChoicesQA.counts.snapshot||0)>=1")
    time.sleep(5.2)
    observation = js("return {counts:{...window.__scanChoicesQA.counts},result:window.__scanChoicesQA.scans[0]}")
    assert observation["counts"]["scanImportSources"] == 1
    omp = next(item for item in observation["result"]["items"] if item["id"] == "omp")
    assert omp["serviceCount"] == 6 and omp["blockedCount"] == 2
    assert all(set(item) == {"id", "name", "path", "status", "serviceCount", "blockedCount", "message"} for item in observation["result"]["items"])
    report["automaticScanAndPolling"] = observation
    resized = call(endpoint, "window_control", {"window": "manager", "action": "set_size", "width": 760, "height": 560})
    wait_for("Math.abs(innerWidth-760)<=1")
    report["nativeMinimumSize"] = resized
    three_themes("scan_minimum")

    new_buddy_path.write_text('// changed after scan\n{"mcpServers":{"buddy-after":{"url":"https://updated.example.invalid/mcp","headers":{"Authorization":"Bearer SCAN_CHOICES_UPDATED_SECRET"},"disabled":true,},},}\n')
    new_buddy_path.chmod(0o600)
    source_button("CodeBuddy CLI")
    wait_for("document.querySelector('.review-item strong')?.textContent==='buddy-after'")
    assert group_values("buddy-after 的导入处理方式") == ["add", "skip"]
    choose("buddy-after 的导入处理方式", "skip")
    assert js("return [...document.querySelectorAll('button')].find(e=>e.textContent.includes('确认导入'))?.disabled")
    choose("buddy-after 的导入处理方式", "add")
    three_themes("fresh_jsonc_preview")
    preview = js("return window.__scanChoicesQA.previews.at(-1)")
    assert preview["params"] == {"sourceId": "codebuddy"} and preview["result"]["source"] == "CodeBuddy"
    assert [item["name"] for item in preview["result"]["items"]] == ["buddy-after"]
    report["freshPreview"] = preview
    click_text("返回修改")
    source_button("OMP")
    wait_for("document.querySelectorAll('.review-item').length===6")
    omp_preview = js("return window.__scanChoicesQA.previews.at(-1).result")
    items = {item["name"]: item for item in omp_preview["items"]}
    for name in ("blocked-helper", "recursive-entry"):
        assert items[name]["blockedReason"] and items[name]["action"] == "skip"
    assert items["mcp-gateway"]["status"] == "new" and not items["mcp-gateway"]["blockedReason"]
    assert items["alias-existing"]["status"] == "duplicate"
    conflict = "choice-02-long-service-name-for-wrap-check"
    assert items[conflict]["status"] == "conflict"
    assert group_values("alias-existing 的导入处理方式") == ["merge", "keep_both", "skip"]
    assert group_values(conflict + " 的导入处理方式") == ["keep_both", "skip"]
    for value in ("keep_both", "skip", "merge"):
        choose("alias-existing 的导入处理方式", value)
    for value in ("skip", "keep_both"):
        choose(conflict + " 的导入处理方式", value)
    blocked = js("const g=[...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent==='recursive-entry 的导入处理方式');g.querySelector('label').click();await Promise.resolve();return {disabled:g.disabled,value:g.querySelector('input:checked').value}")
    assert blocked == {"disabled": True, "value": "skip"}
    report["ompPreview"] = omp_preview
    three_themes("import_decisions")
    click_text("返回修改")
    (home / ".claude.json").write_text('{"mcpServers":{}}\n')
    click_text("重新扫描")
    wait_for("window.__scanChoicesQA.scans.length===2")
    rescanned = capture("explicit_rescan")
    assert next(r for r in rescanned["scanRows"] if r["name"] == "Claude Code")["badge"] == "没有 MCP 服务"
    assert next(r for r in rescanned["scanRows"] if r["name"] == "CodeBuddy CLI")["path"] == str(new_buddy_path)
    choose("导入方式", "paste")
    assert group_values("配置来源") == ["自动识别", "OMP", "Claude Code", "Cursor", "CodeBuddy", "Codex", "通用 MCP JSON"]
    for source in group_values("配置来源"):
        choose("配置来源", source)
    choose("配置来源", "自动识别")
    three_themes("manual_source")
    choose("导入方式", "file")
    assert js("return !!document.querySelector('input[type=file]')")
    choose("导入方式", "scan")
    assert config_state() == before_config and not (stage / "must-not-execute").exists()
    report["scanAndPreviewConfigUnchanged"] = True

    navigate("工具")
    assert len(group_values("工具来源")) == 9
    for value in (group_values("工具来源")[-1], ""):
        choose("工具来源", value)
    three_themes("dynamic_tool_sources")
    navigate("MCP 服务")
    click_text("＋ 添加服务")
    wait_for("document.querySelector('.config-form')")
    assert group_values("传输方式") == ["stdio", "http", "sse"]
    assert group_values("认证方式") == ["none", "env"]
    choose("认证方式", "env")
    choose("传输方式", "http")
    wait_for("[...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent==='认证方式').querySelector('input:checked').value==='none'")
    assert group_values("认证方式") == ["none", "bearer", "api_key", "headers", "oauth"]
    for value in ("bearer", "api_key", "headers", "oauth"):
        choose("认证方式", value)
        capture("http_auth_" + value)
    choose("传输方式", "sse")
    assert js("return [...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent==='认证方式').querySelector('input:checked').value") == "oauth"
    capture("sse_retains_oauth")
    choose("传输方式", "stdio")
    wait_for("[...document.querySelectorAll('.choice-group')].find(e=>e.querySelector('legend').textContent==='认证方式').querySelector('input:checked').value==='none'")
    assert group_values("认证方式") == ["none", "env"]
    three_themes("transport_and_auth")
    click_text("取消", ".config-form button")
    navigate("设置")
    for value in ("aggregate", "progressive"):
        choose("工具发现模式", value)
    for value in (1, 7, 30, 14):
        choose("日志保留", value)
    three_themes("settings_choices")
    choose("工具发现模式", baseline["settings"]["mode"])
    choose("日志保留", baseline["settings"]["logRetentionDays"])
    choose("主题", baseline["settings"]["theme"])
    click_text("保存运行设置")
    wait_for("window.__scanChoicesQA.settings.length===1")
    saved = js("return window.__scanChoicesQA.settings[0]")
    assert saved["retentionType"] == "number" and saved["params"]["logRetentionDays"] == baseline["settings"]["logRetentionDays"]
    final = js("return await window.gateway.request('snapshot')")
    assert final["settings"] == baseline["settings"]
    assert len(final["services"]) == 8 and all(not service["enabled"] for service in final["services"])
    report["savedNumericSettings"] = saved
    report["focusEvidence"] = js("const e=document.querySelector('.choice-group input:checked');e.focus({focusVisible:true});const rules=[...document.styleSheets].flatMap(s=>[...s.cssRules]).filter(r=>r.selectorText?.includes('.choice-option input:focus-visible')).map(r=>({selector:r.selectorText,outline:r.style.outline,outlineOffset:r.style.outlineOffset}));return {focused:document.activeElement===e,focusVisibleMatched:e.matches(':focus-visible'),selectorSupported:CSS.supports('selector(:focus-visible)'),rules,scope:'Programmatic focus and loaded rules; not actual OS keyboard focus or pixel evidence'}")
    assert report["focusEvidence"]["focused"] and len(report["focusEvidence"]["rules"]) == 2
    recorder = js("const q=window.__scanChoicesQA;return {counts:q.counts,scans:q.scans,previews:q.previews,settings:q.settings}")
    assert not any(marker in json.dumps(recorder) for marker in markers)
    assert not any(recorder["counts"].get(name, 0) for name in ("applyImport", "saveService", "testService", "reconnectService", "applyAgentConfig"))
    assert not (stage / "must-not-execute").exists()
    report["bridgeObservations"] = recorder
    report["noImportAppliedOrFixtureExecuted"] = True
    report["passed"] = True
finally:
    for path, content in client_before.items():
        path.write_bytes(content)
    if new_buddy_path.exists():
        new_buddy_path.unlink()
    report["clientFixtureBytesRestored"] = all(path.read_bytes() == content for path, content in client_before.items()) and not new_buddy_path.exists()
    try:
        choose("主题", original_theme)
        navigate("MCP 服务")
        js("window.gateway.request=window.__scanChoicesQA.original;delete window.__scanChoicesQA;return true")
        call(endpoint, "window_control", {"window": "manager", "action": "set_size", "width": original_size["width"], "height": original_size["height"]})
        report["restoredNativeSize"] = {"width": original_size["width"], "height": original_size["height"]}
    finally:
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
