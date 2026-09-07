"""Exercise only the isolated acceptance app through its real WKWebView DOM."""
import argparse
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

from wails_mcp import call

ROOT = Path(__file__).resolve().parents[2]
PROFILE = ROOT / ".cache/native-profile"
ENDPOINT = "http://127.0.0.1:19199/mcp"
REPORT = {"surface": "real WKWebView DOM through official debug-only Wails MCP", "steps": []}


def js(source):
    return call(ENDPOINT, "js_eval", {"window": "manager", "js": source, "timeout_ms": 15000})


def record(name, value):
    REPORT["steps"].append({"name": name, "value": value})
    print(name, flush=True)
    return value


def click(selector):
    for _ in range(60):
        if js("const e=document.querySelector(" + json.dumps(selector) + ");return !!e&&!e.disabled"):
            break
        time.sleep(.2)
    return js("const e=document.querySelector(" + json.dumps(selector) + ");if(!e||e.disabled)throw Error('Missing or disabled control');e.click();await Promise.resolve();return true")


def fill(selector, value):
    return js("const e=document.querySelector(" + json.dumps(selector) + ");if(!e)throw Error('Missing field');e.value=" + json.dumps(value) + ";e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));await Promise.resolve();return true")


def click_text(text, scope="main"):
    return js("const e=[...document.querySelectorAll(" + json.dumps(scope + " button") + ")].find(e=>e.textContent.trim()===" + json.dumps(text) + ");if(!e||e.disabled)throw Error('Missing or disabled button');e.click();return true")


def snap():
    return js("const s=await window.gateway.request('snapshot');return {gateway:s.gateway,settings:s.settings,services:s.services,tools:s.tools}")


def idle():
    for _ in range(80):
        value = js("return {busy:!!document.querySelector('.section-heading .control:disabled,.inspector .inspect-top input:disabled,.tool-access input:disabled')||[...document.querySelectorAll('button')].some(e=>/正在保存|正在导入|正在检查|正在处理/.test(e.textContent)),errors:[...document.querySelectorAll('.error-feedback,.config-form .error-text')].map(e=>e.textContent),feedback:document.querySelector('.feedback:not(.error-feedback)')?.textContent||'',title:document.querySelector('h1')?.textContent}")
        if not value["busy"]:
            if value["errors"]:
                raise AssertionError(value)
            return value
        time.sleep(.2)
    raise TimeoutError("UI remained busy")


def wait_ready(names):
    for _ in range(80):
        value = snap()
        present = {s["id"]: s["status"] for s in value["services"]}
        if all(present.get(name) == "ready" for name in names):
            return value
        time.sleep(.25)
    raise AssertionError(present)


def wait_tool(tool_id, enabled):
    for _ in range(80):
        tool = next((t for t in snap()["tools"] if t["id"] == tool_id), None)
        if tool is not None and tool["enabled"] == enabled:
            return tool
        time.sleep(.25)
    raise AssertionError({"toolId": tool_id, "expectedEnabled": enabled, "lastTool": tool})


def wait_js(source):
    for _ in range(100):
        value = js(source)
        if value and value != "ok":
            return value
        time.sleep(.2)
    raise AssertionError("Expected WebView state did not arrive: " + source)


def profile_data():
    result = {}
    for name in ("core.json", "services.json", "settings.json"):
        path = PROFILE / name
        data = path.read_bytes() if path.exists() else b""
        result[name] = {"digest": hashlib.sha256(data).hexdigest(), "data": json.loads(data) if data else None}
    assert not result["core.json"]["data"].get("api_key"), "Test profile must not contain a plaintext admin key"
    return result


def import_restore():
    baseline = snap()
    assert [s["id"] for s in baseline["services"]] == ["native-echo"], "Unexpected test profile services"
    before = profile_data()
    original = baseline["services"][0]
    connection = {"command": original["command"], "args": original["args"], "enabled": True}
    fixture = {"mcpServers": {
        "native-echo": {**connection, "args": original["args"] + ["conflict-variant"]},
        "native-echo-copy": connection,
        "native-skip": {**connection, "args": original["args"] + ["skip-variant"], "enabled": False},
        "native-added": {**connection, "args": original["args"] + ["added-variant"]},
    }}
    click(".nav-button:nth-of-type(3)")
    js("const e=[...document.querySelectorAll('main button')].find(e=>e.textContent==='继续导入');if(e)e.click();await Promise.resolve();return true")
    click(".source-choice:nth-of-type(2)")
    fill("textarea.json-input", json.dumps(fixture))
    click(".form-actions .control.primary")
    idle()
    preview = js("return [...document.querySelectorAll('.review-item')].map(e=>({name:e.querySelector('strong').textContent,status:e.querySelector('.badge').textContent,options:[...e.querySelectorAll('option')].map(o=>o.value),reason:e.textContent}))")
    record("import_preview", preview)
    assert len(preview) == 4
    statuses = {item["name"]: item["status"] for item in preview}
    assert statuses == {"native-added": "新增", "native-echo": "存在差异", "native-echo-copy": "相同连接", "native-skip": "新增"}, statuses
    decisions = {"native-echo": "keep_both", "native-echo-copy": "merge", "native-skip": "skip", "native-added": "add"}
    js("const d=" + json.dumps(decisions) + ";for(const row of document.querySelectorAll('.review-item')){const e=row.querySelector('select');e.value=d[row.querySelector('strong').textContent];e.dispatchEvent(new Event('change',{bubbles:true}));}await Promise.resolve();return true")
    click(".form-actions .control.primary")
    idle()
    result = record("import_result", js("return {text:document.querySelector('main').textContent,backupId:document.querySelector('.empty-state .note')?.textContent.match(/备份编号：(.*?)。/)?.[1]}"))
    assert "新增 2 项，合并 1 项，跳过 1 项" in result["text"], result
    assert result["backupId"]
    after = snap()
    ids = [s["id"] for s in after["services"]]
    assert len(ids) == 3 and "native-echo" in ids and "native-added" in ids and "native-skip" not in ids, ids
    original_after = next(s for s in after["services"] if s["id"] == "native-echo")
    assert original_after["args"] == original["args"] and original_after["sources"], original_after
    applied = profile_data()
    before_unrelated = {k: v for k, v in before["core.json"]["data"].items() if k != "mcpServers"}
    after_unrelated = {k: v for k, v in applied["core.json"]["data"].items() if k != "mcpServers"}
    preserved = all(after_unrelated.get(k) == v for k, v in before_unrelated.items())
    extra_fields = sorted(after_unrelated.keys() - before_unrelated.keys())
    record("apply_preservation", {"serviceIds": ids, "originalArgumentsPreserved": True, "sourceMerged": original_after["sources"], "existingUnrelatedCoreFieldsEqual": preserved, "addedCoreDefaultFields": extra_fields, "settingsEqual": before["settings.json"]["data"] == applied["settings.json"]["data"]})
    assert preserved and set(extra_fields).issubset({"features"})
    assert before["settings.json"]["data"] == applied["settings.json"]["data"]
    click(".settings-nav")
    click(".section-heading .control")
    idle()
    js("const id=" + json.dumps(result["backupId"]) + ";const row=[...document.querySelectorAll('.settings-page .wide-row')].find(e=>e.textContent.includes(id));if(!row)throw Error('Import backup missing from UI');row.querySelector('button').click();await Promise.resolve();return {dialog:document.querySelector('dialog[open]')?.textContent}")
    click("dialog[open] .control.danger")
    idle()
    restored = wait_ready(["native-echo"])
    restored_data = profile_data()
    checks = {name: {"semanticEqual": before[name]["data"] == restored_data[name]["data"], "exactBytesEqual": before[name]["digest"] == restored_data[name]["digest"]} for name in before}
    record("restored_configuration", {"files": checks, "serviceIds": [s["id"] for s in restored["services"]], "settings": restored["settings"]})
    assert all(item["semanticEqual"] for item in checks.values()), checks
    assert [s["id"] for s in restored["services"]] == ["native-echo"]
    assert restored["settings"] == baseline["settings"]


def invoke(tool_id, text):
    return js("try{return {result:await window.gateway.request('callTool',{toolId:" + json.dumps(tool_id) + ",arguments:{text:" + json.dumps(text) + "}})}}catch(e){return {error:String(e)}}")


def service_tools():
    baseline = snap()
    assert {s["id"] for s in baseline["services"]}.issubset({"native-echo", "native-crud"})
    source = next(s for s in baseline["services"] if s["id"] == "native-echo")
    click(".nav-button:nth-of-type(1)")
    if any(s["id"] == "native-crud" for s in baseline["services"]):
        js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-crud').click();return true")
        click('.tab-bar [role=tab]:nth-of-type(2)')
        click_text("移除服务", ".panel-body")
        click("dialog[open] .control.danger")
        idle()
        baseline = snap()
    if not any(s["id"] == "native-crud" for s in baseline["services"]):
        click(".page-head .control.primary")
        fill('input[placeholder="例如：work-docs"]', "native-crud")
        fill('input[placeholder="例如：npx 或可执行文件的完整路径"]', source["command"])
        fill(".config-form textarea", json.dumps(source["args"]))
        click(".config-form .form-actions .control.primary")
        idle()
    js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-crud').click();return true")
    ready = wait_ready(["native-echo", "native-crud"])
    tool_id = next(t["id"] for t in ready["tools"] if t["serviceId"] == "native-crud")
    record("created_service", next(s for s in ready["services"] if s["id"] == "native-crud"))
    click('.tab-bar [role=tab]:nth-of-type(2)')
    for label in ("测试连接", "重新连接"):
        click_text(label, ".panel-body")
        feedback = idle()
        wait_ready(["native-crud"])
        record(label, feedback)
    click_text("编辑配置", ".panel-body")
    fill(".config-form textarea", json.dumps(source["args"] + ["edited-by-native-ui"]))
    fill('input[placeholder="/Users/…/project"]', str(ROOT))
    click(".config-form .form-actions .control.primary")
    idle()
    value = wait_ready(["native-crud"])
    edited = next(s for s in value["services"] if s["id"] == "native-crud")
    assert edited["args"] == source["args"] + ["edited-by-native-ui"] and edited["cwd"] == str(ROOT)
    record("edited_service", edited)
    click(".inspect-top input[type=checkbox]")
    idle()
    disabled = next(s for s in snap()["services"] if s["id"] == "native-crud")
    denial = invoke(tool_id, "should-be-denied-service-disabled")
    record("service_disabled_cached_call", {"service": disabled, "call": denial})
    assert not disabled["enabled"] and (denial.get("error") or denial.get("result", {}).get("isError")), denial
    click(".inspect-top input[type=checkbox]")
    idle()
    wait_ready(["native-crud"])
    click('.tab-bar [role=tab]:nth-of-type(1)')
    click(".tool-summary")
    click(".tool-access input[type=checkbox]")
    idle()
    disabled_tool = wait_tool(tool_id, False)
    denied_tool = invoke(tool_id, "should-be-denied-tool-disabled")
    call_button_disabled = wait_js("return document.querySelector('.tool-content form button')?.disabled===true")
    record("tool_disabled_cached_call", {"tool": disabled_tool, "call": denied_tool, "callButtonDisabled": call_button_disabled})
    assert not disabled_tool["enabled"] and call_button_disabled and (denied_tool.get("error") or denied_tool.get("result", {}).get("isError")), denied_tool
    click(".tool-access input[type=checkbox]")
    idle()
    wait_tool(tool_id, True)
    enabled_call = record("tool_reenabled_call", invoke(tool_id, "native-reenabled-tool"))
    assert enabled_call["result"]["structuredContent"]["echo"] == "native-reenabled-tool"
    click(".nav-button:nth-of-type(2)")
    fill('input[aria-label="搜索全部工具"]', "no-such-native-acceptance-tool-2026")
    no_tools = record("tool_search_empty", js("return {text:document.querySelector('main').textContent,toolRows:document.querySelectorAll('.tool-entry').length}"))
    assert not no_tools["toolRows"] and "没有匹配的工具" in no_tools["text"]
    fill('input[aria-label="搜索全部工具"]', "")
    click(".nav-button:nth-of-type(1)")
    fill('input[aria-label="搜索 MCP 服务"]', "no-such-native-acceptance-service-2026")
    no_services = record("service_search_empty", js("return {text:document.querySelector('main').textContent,serviceRows:document.querySelectorAll('.service-row').length}"))
    assert not no_services["serviceRows"] and "没有匹配的服务" in no_services["text"]
    fill('input[aria-label="搜索 MCP 服务"]', "")
    js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-crud').click();return true")
    click('.tab-bar [role=tab]:nth-of-type(2)')
    click_text("移除服务", ".panel-body")
    click("dialog[open] .control.danger")
    idle()
    final = wait_ready(["native-echo"])
    assert [s["id"] for s in final["services"]] == ["native-echo"]
    deleted_call = invoke(tool_id, "should-be-denied-deleted-service")
    record("service_deleted", {"serviceIds": [s["id"] for s in final["services"]], "cachedCall": deleted_call})
    assert deleted_call.get("error") or deleted_call.get("result", {}).get("isError"), deleted_call


def auth_forms():
    baseline = snap()
    assert {s["id"] for s in baseline["services"]}.issubset({"native-echo", "native-bearer", "native-api-key", "native-headers"})
    fixtures = [
        {"name": "native-bearer", "path": "bearer", "type": "bearer", "value": "gateway-native-bearer-fixture"},
        {"name": "native-api-key", "path": "api-key", "type": "api_key", "value": "gateway-native-api-fixture"},
        {"name": "native-headers", "path": "headers", "type": "headers", "pairs": [["X-API-Key", "gateway-native-api-fixture"], ["X-Tenant", "native-fixture-team"], ["X-Trace-Label", "native-form-proof"]]},
    ]
    for fixture in fixtures:
        click(".nav-button:nth-of-type(1)")
        existing = next((s for s in snap()["services"] if s["id"] == fixture["name"]), None)
        if existing:
            assert existing["url"] == "http://127.0.0.1:63832/" + fixture["path"] + "/mcp" and existing["auth"]["type"] == fixture["type"]
            js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent===" + json.dumps(fixture["name"]) + ").click();return true")
            record(fixture["name"] + "_existing_saved_fixture", {"id": existing["id"], "authType": existing["auth"]["type"]})
        else:
            click(".page-head .control.primary")
            fill('input[placeholder="例如：work-docs"]', fixture["name"])
            fill(".config-form .split-fields select", "http")
            fill('input[placeholder="https://example.com/mcp"]', "http://127.0.0.1:63832/" + fixture["path"] + "/mcp")
            fill(".config-form .form-section select", fixture["type"])
            if fixture["type"] == "headers":
                for index, (key, value) in enumerate(fixture["pairs"]):
                    click_text("＋ 添加 Header", ".config-form")
                    fill(f".pair-row:nth-of-type({index + 1}) input:not([type=password])", key)
                    fill(f".pair-row:nth-of-type({index + 1}) input[type=password]", value)
            else:
                if fixture["type"] == "api_key":
                    fill('input[placeholder="X-API-Key"]', "X-API-Key")
                fill(".config-form input[type=password]", fixture["value"])
            click(".config-form .form-actions .control.primary")
            idle()
        wait_ready([fixture["name"]])
        click('.tab-bar [role=tab]:nth-of-type(1)')
        click(".tool-summary")
        fill(".tool-content textarea", json.dumps({"text": "native form proof"}))
        click(".tool-content form button")
        response = wait_js("const p=document.querySelector('.result-box pre');return p?JSON.parse(p.textContent):null")
        record(fixture["name"] + "_tool_result", response)
        assert response.get("isError") is not True and response.get("structuredContent"), response
        assert response["structuredContent"]["authentication_accepted"] is True
        assert all(response["structuredContent"]["expected_headers_matched"].values())
        assert response["structuredContent"]["text"] == "native form proof"
        click('.tab-bar [role=tab]:nth-of-type(2)')
        click_text("编辑配置", ".panel-body")
        stored = js("return {passwords:[...document.querySelectorAll('.config-form input[type=password]')].map(e=>({empty:e.value==='',placeholder:e.placeholder,required:e.required})),authType:document.querySelector('.config-form .form-section select').value}")
        record(fixture["name"] + "_saved_credential_form", stored)
        assert stored["authType"] == fixture["type"] and stored["passwords"]
        assert all(p["empty"] and "已保存" in p["placeholder"] and not p["required"] for p in stored["passwords"])
        click_text("取消", ".config-form")
    record("saved_auth_services", [{"id": s["id"], "authType": s["auth"]["type"], "status": s["status"]} for s in snap()["services"]])


def big_diagnostics():
    click(".nav-button:nth-of-type(1)")
    js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-echo').click();return true")
    click('.tab-bar [role=tab]:nth-of-type(1)')
    click(".tool-summary")
    js("const e=document.querySelector('.tool-content textarea');e.value=JSON.stringify({text:'A'.repeat(1048576)+'NATIVE_UI_END_20260907'});e.dispatchEvent(new Event('input',{bubbles:true}));return true")
    click(".tool-content form button")
    result = wait_js("const e=document.querySelector('.result-box pre');if(!e)return null;const v=JSON.parse(e.textContent);if(!v.structuredContent?.echo?.endsWith('NATIVE_UI_END_20260907'))return null;e.scrollIntoView({block:'end'});e.scrollTop=e.scrollHeight;e.scrollLeft=e.scrollWidth;const c=getComputedStyle(e);return {textLength:v.content[0].text.length,structuredTextLength:v.structuredContent.echo.length,textTailPresent:v.content[0].text.endsWith('NATIVE_UI_END_20260907'),structuredTailPresent:true,renderedJSONBytes:new TextEncoder().encode(e.textContent).length,scrollWidth:e.scrollWidth,clientWidth:e.clientWidth,scrollLeft:e.scrollLeft,scrollHeight:e.scrollHeight,clientHeight:e.clientHeight,scrollTop:e.scrollTop,overflowX:c.overflowX,overflowY:c.overflowY,whiteSpace:c.whiteSpace}")
    record("large_result_rendered_and_scrolled", result)
    assert result["textLength"] == 1048576 + len("NATIVE_UI_END_20260907") and result["structuredTextLength"] == result["textLength"] and result["textTailPresent"]
    diagnostics_file()


def diagnostics_file():
    click(".nav-button:nth-of-type(5)")
    previous_feedback = js("return document.querySelector('.feedback:not(.error-feedback) > span')?.textContent||''")
    markers = ["gateway-native-bearer-fixture", "gateway-native-api-fixture", "native-fixture-team", "native-form-proof", "native-env-one", "native env two = with spaces", "原生环境变量三"]
    exports = []
    for index in range(2):
        click(".page-head .control")
        idle()
        feedback = wait_js("const t=document.querySelector('.feedback:not(.error-feedback) > span')?.textContent||'';return t.startsWith('诊断已保存：')&&t!==" + json.dumps(previous_feedback) + "?t:null")
        path = Path(feedback.removeprefix("诊断已保存：").strip())
        assert path.is_absolute() and path.resolve().parent == (PROFILE / "diagnostics").resolve(), path
        assert path.name.startswith("mcp-gateway-diagnostics-") and path.suffix == ".json", path
        assert not path.is_symlink() and path.is_file(), path
        data = path.read_bytes()
        value = json.loads(data)
        assert set(value) == {"version", "exportedAt", "gateway", "services"}, value.keys()
        assert all(set(s) == {"id", "transport", "status", "toolCount", "catalogStatus"} for s in value["services"])
        file_mode, directory_mode = path.stat().st_mode & 0o777, path.parent.stat().st_mode & 0o777
        assert file_mode == 0o600 and directory_mode == 0o700, (file_mode, directory_mode)
        assert all(marker.encode() not in data for marker in markers)
        assert b"NATIVE_UI_END_20260907" not in data and b"native-final-status-projection" not in data
        item = {"path": str(path), "filename": path.name, "feedback": feedback, "feedbackContainsSavedPath": str(path) in feedback, "fileBytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), "fileMode": oct(file_mode), "directoryMode": oct(directory_mode), "json": value, "syntheticCredentialMarkersAbsent": True, "toolPayloadMarkersAbsent": True}
        record("diagnostics_file_export_" + str(index + 1), item)
        exports.append(item)
        previous_feedback = feedback
    assert exports[0]["path"] != exports[1]["path"]
    assert all(hashlib.sha256(Path(item["path"]).read_bytes()).hexdigest() == item["sha256"] for item in exports)
    record("repeated_export_preserved_both_files", {"distinctPaths": True, "bothOriginalHashesUnchanged": True})
    files = [PROFILE / "core.json", PROFILE / "services.json", PROFILE / "settings.json"]
    files.extend(p for p in (PROFILE / "logs").glob("*") if p.is_file())
    scan = [{"file": str(p.relative_to(PROFILE)), "credentialMarkerAbsent": all(marker.encode() not in p.read_bytes() for marker in markers)} for p in files if p.exists()]
    record("test_profile_credential_scan", scan)
    assert all(item["credentialMarkerAbsent"] for item in scan)
    record("final_test_profile", {"ownership": owned_core(), "settings": snap()["settings"]})


def env_form():
    source = next(s for s in snap()["services"] if s["id"] == "native-echo")
    click(".nav-button:nth-of-type(1)")
    if any(s["id"] == "native-env" for s in snap()["services"]):
        js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-env').click();return true")
        click('.tab-bar [role=tab]:nth-of-type(2)')
        click_text("移除服务", ".panel-body")
        click("dialog[open] .control.danger")
        idle()
    click(".page-head .control.primary")
    fill('input[placeholder="例如：work-docs"]', "native-env")
    fill('input[placeholder="例如：npx 或可执行文件的完整路径"]', source["command"])
    fill(".config-form textarea", json.dumps([str(ROOT / "patches/mcpproxy/probes/native_env_fixture.py"), "--evidence", "/private/tmp/mcp-gateway-native-auth-fixtures-20260907/native-env.jsonl"]))
    fill(".config-form .form-section select", "env")
    for index, (key, value) in enumerate([("MCP_GATEWAY_NATIVE_ENV_ALPHA", "native-env-one"), ("MCP_GATEWAY_NATIVE_ENV_BETA", "native env two = with spaces"), ("MCP_GATEWAY_NATIVE_ENV_GAMMA", "原生环境变量三")]):
        click_text("＋ 添加环境变量", ".config-form")
        fill(f".pair-row:nth-of-type({index + 1}) input:not([type=password])", key)
        fill(f".pair-row:nth-of-type({index + 1}) input[type=password]", value)
    click(".config-form .form-actions .control.primary")
    idle()
    wait_ready(["native-env"])
    click('.tab-bar [role=tab]:nth-of-type(1)')
    click(".tool-summary")
    fill(".tool-content textarea", "{}")
    click(".tool-content form button")
    response = wait_js("const p=document.querySelector('.result-box pre');return p?JSON.parse(p.textContent):null")
    record("three_environment_values_reached_stdio", response)
    assert response["structuredContent"]["all_environment_matched"] is True
    assert response["structuredContent"]["management_key_not_inherited"] is True
    click('.tab-bar [role=tab]:nth-of-type(2)')
    click_text("编辑配置", ".panel-body")
    stored = js("return [...document.querySelectorAll('.config-form input[type=password]')].map(e=>({empty:e.value==='',stored:e.placeholder.includes('已保存'),required:e.required}))")
    record("environment_form_saved_values_hidden", stored)
    assert len(stored) == 3 and all(p["empty"] and p["stored"] and not p["required"] for p in stored)
    click_text("取消", ".config-form")


def oauth_form():
    click(".nav-button:nth-of-type(1)")
    existing = next((s for s in snap()["services"] if s["id"] == "native-oauth"), None)
    if existing:
        assert existing["url"] == "http://127.0.0.1:63949/mcp" and existing["auth"]["type"] == "oauth"
        js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-oauth').click();return true")
    else:
        click(".page-head .control.primary")
        fill('input[placeholder="例如：work-docs"]', "native-oauth")
        fill(".config-form .split-fields select", "http")
        fill('input[placeholder="https://example.com/mcp"]', "http://127.0.0.1:63949/mcp")
        fill(".config-form .form-section select", "oauth")
        record("oauth_configuration_fields", js("return {authType:document.querySelector('.config-form .form-section select').value,labels:[...document.querySelectorAll('.config-form .form-section label')].map(e=>e.textContent),publicFieldsBlank:[...document.querySelectorAll('.config-form .form-section input')].every(e=>!e.value)}"))
        click(".config-form .form-actions .control.primary")
        idle()
    click('.tab-bar [role=tab]:nth-of-type(2)')
    click_text("清除本地授权", ".panel-body")
    record("clear_authorization_confirmation", wait_js("return document.querySelector('dialog[open]')?.textContent||null"))
    click("dialog[open] .control.danger")
    idle()
    cleared = next(s for s in snap()["services"] if s["id"] == "native-oauth")
    record("local_oauth_cleared", {"serviceStatus": cleared["status"], "message": js("return document.querySelector('.panel-body .callout')?.textContent")})
    assert cleared["status"] != "ready"
    js("window.__qaOAuth={};window.__qaOriginalRequest=window.gateway.request;window.gateway.request=async function(method,params){const value=await window.__qaOriginalRequest(method,params);if(['startOAuth','cancelOAuth','refreshOAuth'].includes(method))window.__qaOAuth[method]=value;return value};return true")
    try:
        click_text("在浏览器中登录", ".panel-body")
        idle()
        first = wait_js("return window.__qaOAuth.startOAuth||null")
        assert first["status"] == "pending"
        click_text("取消登录", ".panel-body")
        idle()
        record("oauth_cancelled_from_ui", wait_js("return window.__qaOAuth.cancelOAuth||null"))
        time.sleep(6)  # The fixture's real browser page submits after five seconds.
        cancelled = next(s for s in snap()["services"] if s["id"] == "native-oauth")
        record("cancelled_flow_remains_unauthorized_after_fixture_autosubmit", {"serviceStatus": cancelled["status"]})
        assert cancelled["status"] != "ready"
        js("delete window.__qaOAuth.startOAuth;return true")
        click_text("在浏览器中登录", ".panel-body")
        idle()
        record("oauth_native_browser_login_started", wait_js("return window.__qaOAuth.startOAuth||null"))
        ready = wait_ready(["native-oauth"])
        record("oauth_authorized_service", {"service": next(s for s in ready["services"] if s["id"] == "native-oauth"), "consentMechanism": "The actual browser loads the official loopback fixture page, whose login.html submits its public test account and consent after five seconds. No agent POST or manual consent click is claimed."})
        click_text("刷新授权", ".panel-body")
        idle()
        record("oauth_refresh_from_ui", wait_js("return window.__qaOAuth.refreshOAuth||null"))
        ready = wait_ready(["native-oauth"])
        tool = next(t for t in ready["tools"] if t["serviceId"] == "native-oauth" and t["name"] == "echo")
        response = js("return await window.gateway.request('callTool',{toolId:" + json.dumps(tool["id"]) + ",arguments:{message:'native OAuth form proof'}})")
        record("authorized_oauth_tool_call", response)
        assert response.get("isError") is not True and "native OAuth form proof" in json.dumps(response)
    finally:
        js("if(window.__qaOriginalRequest)window.gateway.request=window.__qaOriginalRequest;delete window.__qaOriginalRequest;delete window.__qaOAuth;return true")


def listener_pid(port):
    output = subprocess.check_output(["lsof", "-nP", "-t", "-iTCP:" + str(port), "-sTCP:LISTEN"], text=True)
    pids = {int(line) for line in output.splitlines()}
    assert len(pids) == 1, pids
    return pids.pop()


def owned_core():
    state = snap()
    port = int(state["settings"]["listenAddress"].rsplit(":", 1)[1])
    app_pid, core_pid = listener_pid(19199), listener_pid(port)
    app_path = subprocess.check_output(["ps", "-p", str(app_pid), "-o", "comm="], text=True).strip()
    core_line = subprocess.check_output(["ps", "-p", str(core_pid), "-o", "ppid=,comm="], text=True).strip().split(None, 1)
    bundle = ROOT / "artifacts/MCP Gateway Test.app/Contents/MacOS"
    assert Path(app_path).resolve() == bundle / "mcp-gateway"
    assert int(core_line[0]) == app_pid and Path(core_line[1]).resolve() == bundle / "mcpproxy"
    return {"appPID": app_pid, "corePID": core_pid, "listenAddress": state["settings"]["listenAddress"], "ownershipVerified": True}


def echo_after_restart(label):
    state = wait_ready(["native-echo", "native-bearer", "native-oauth"])
    echo = next(t for t in state["tools"] if t["serviceId"] == "native-echo")
    result = invoke(echo["id"], label)
    assert result["result"]["structuredContent"]["echo"] == label
    oauth = next(t for t in state["tools"] if t["serviceId"] == "native-oauth" and t["name"] == "echo")
    oauth_result = js("return await window.gateway.request('callTool',{toolId:" + json.dumps(oauth["id"]) + ",arguments:{message:" + json.dumps(label) + "}})")
    assert oauth_result.get("isError") is not True and label in json.dumps(oauth_result)
    return {"echoSucceeded": True, "oauthToolSucceededWithoutNewLogin": True, "serviceStates": {s["id"]: s["status"] for s in state["services"]}}


def port_recovery():
    original = owned_core()
    record("before_core_failure", original)
    assert snap()["gateway"]["activeCalls"] == 0
    original_port = int(original["listenAddress"].rsplit(":", 1)[1])
    os.kill(original["corePID"], signal.SIGKILL)
    record("core_failure_visible", wait_js("return document.querySelector('.connection-banner[role=alert]')?.textContent||null"))
    sentinel = None
    try:
        with tempfile.TemporaryDirectory(prefix="mcp-gateway-native-port-") as temporary:
            evidence = Path(temporary) / "sentinel.json"
            sentinel = subprocess.Popen([sys.executable, str(ROOT / "tests/acceptance/port_sentinel.py"), str(original_port), str(evidence)], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            for _ in range(80):
                if evidence.exists():
                    break
                assert sentinel.poll() is None, "Port sentinel failed to start: " + sentinel.stderr.read().decode()
                time.sleep(.05)
            record("external_port_owner", json.loads(evidence.read_text()))
            with urllib.request.urlopen("http://" + original["listenAddress"], timeout=2) as response:
                assert response.read() == b"sentinel"
            click('.connection-banner[role=alert] .control')
            error = wait_js("return document.querySelector('.error-feedback')?.textContent||null")
            record("occupied_port_error_visible", error)
            assert "address already in use" in error or "占用" in error
            assert snap()["settings"]["listenAddress"] == original["listenAddress"]
            with socket.socket() as reservation:
                reservation.bind(("127.0.0.1", 0))
                safe_address = "127.0.0.1:" + str(reservation.getsockname()[1])
            click(".settings-nav")
            fill(".settings-page .address-input", safe_address)
            click(".settings-page form .control.primary")
            idle()
            recovered = owned_core()
            assert recovered["appPID"] == original["appPID"] and recovered["corePID"] != original["corePID"] and recovered["listenAddress"] == safe_address
            with urllib.request.urlopen("http://" + original["listenAddress"], timeout=2) as response:
                sentinel_untouched = response.read() == b"sentinel" and sentinel.poll() is None
            assert sentinel_untouched
            record("offline_settings_recovered_on_safe_port", {**recovered, "externalPortOwnerUntouched": sentinel_untouched, "calls": echo_after_restart("native-port-recovery")})
    finally:
        if sentinel is not None and sentinel.poll() is None:
            sentinel.terminate()
            sentinel.wait(timeout=5)
        current = snap()
        if current["settings"]["listenAddress"] != original["listenAddress"]:
            click(".settings-nav")
            fill(".settings-page .address-input", original["listenAddress"])
            click(".settings-page form .control.primary")
            idle()
            record("original_port_restored", {**owned_core(), "calls": echo_after_restart("native-original-port-restored")})


def retention_settings():
    baseline = snap()["settings"]
    assert baseline["logRetentionDays"] == 14
    click(".settings-nav")
    prior = owned_core()
    for days in (7, 14):
        js("const e=[...document.querySelectorAll('.settings-page form select')].find(e=>[...e.options].some(o=>o.textContent==='14 天'));e.value=" + json.dumps(str(days)) + ";e.dispatchEvent(new Event('change',{bubbles:true}));return true")
        click(".settings-page form .control.primary")
        idle()
        current = owned_core()
        cfg = json.loads((PROFILE / "core.json").read_text())
        assert current["appPID"] == prior["appPID"] and current["corePID"] != prior["corePID"]
        assert cfg["activity_retention_days"] == days and cfg["logging"]["max_age"] == days
        assert snap()["settings"]["logRetentionDays"] == days
        record("retention_" + str(days) + "_days_applied", {**current, "activityRetentionDays": cfg["activity_retention_days"], "logArchiveMaxAge": cfg["logging"]["max_age"], "calls": echo_after_restart("native-retention-" + str(days))})
        prior = current
    for theme in ("light", baseline["theme"]):
        fill(".theme-picker select", theme)
        click(".settings-page form .control.primary")
        idle()
        current = owned_core()
        assert current["corePID"] == prior["corePID"] and snap()["settings"]["theme"] == theme
        record("theme_only_save_" + theme, {**current, "coreNotRestarted": True})
    assert snap()["settings"] == baseline


def final_status_catalog():
    ready = wait_ready(["native-echo", "native-bearer", "native-oauth"])
    oauth = next(s for s in ready["services"] if s["id"] == "native-oauth")
    assert oauth["authStatus"] == "authenticated", oauth
    record("existing_oauth_restored_after_final_bundle_restart", {"ownership": owned_core(), "oauthStatus": oauth["authStatus"], "calls": echo_after_restart("native-final-bundle-restart")})
    click(".nav-button:nth-of-type(1)")
    js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-oauth').click();return true")
    click('.tab-bar [role=tab]:nth-of-type(2)')
    auth_text = wait_js("const d=[...document.querySelectorAll('.detail-list div')].find(e=>e.querySelector('dt')?.textContent==='认证状态');const text=d?.querySelector('dd')?.textContent;return text==='已登录'?text:null")
    record("oauth_authenticated_label", auth_text)
    js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-echo').click();return true")
    click('.tab-bar [role=tab]:nth-of-type(1)')
    live = js("return {inspector:document.querySelector('.inspect-name h2')?.textContent,offlineNotice:[...document.querySelectorAll('.panel-body .callout')].some(e=>e.textContent.includes('连接恢复后'))}")
    record("live_catalog_has_no_offline_notice", live)
    assert live["inspector"] == "native-echo" and not live["offlineNotice"]
    click(".inspect-top input[type=checkbox]")
    idle()
    cached = wait_js("const e=document.querySelector('.panel-body .callout');return e?.textContent.includes('缓存目录')?e.textContent:null")
    record("disabled_service_keeps_cached_notice", cached)
    click(".inspect-top input[type=checkbox]")
    idle()
    wait_ready(["native-echo"])
    wait_js("return document.querySelector('.inspect-name h2')?.textContent==='native-echo'&&!document.querySelector('.panel-body .callout')")
    record("reconnected_live_catalog_notice_removed", True)
    js("[...document.querySelectorAll('.service-row')].find(e=>e.querySelector('strong').textContent==='native-oauth').click();return true")
    click('.tab-bar [role=tab]:nth-of-type(2)')
    click_text("清除本地授权", ".panel-body")
    click("dialog[open] .control.danger")
    idle()
    cleared = next(s for s in snap()["services"] if s["id"] == "native-oauth")
    assert cleared["authStatus"] == "none" and cleared["status"] != "ready", cleared
    clear_label = wait_js("const d=[...document.querySelectorAll('.detail-list div')].find(e=>e.querySelector('dt')?.textContent==='认证状态');const text=d?.querySelector('dd')?.textContent;return text==='需要登录'?text:null")
    record("oauth_cleared_label", {"authStatus": cleared["authStatus"], "label": clear_label, "message": js("return document.querySelector('.panel-body .callout')?.textContent")})
    click_text("在浏览器中登录", ".panel-body")
    idle()
    wait_ready(["native-oauth"])
    record("oauth_relogin_and_final_calls", echo_after_restart("native-final-status-projection"))
    cfg = json.loads((PROFILE / "core.json").read_text())
    assert not cfg.get("api_key")
    record("final_test_profile", {"ownership": owned_core(), "settings": snap()["settings"], "plaintextAdminKeyAbsent": True})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("scenario", choices=["import-restore", "service-tools", "auth-forms", "big-diagnostics", "env-form", "oauth-form", "port-recovery", "retention-settings", "final-status-catalog", "diagnostics-file"])
    parser.add_argument("--report", required=True)
    args = parser.parse_args()
    REPORT["scenario"] = args.scenario
    REPORT["executedAt"] = datetime.now().astimezone().isoformat()
    bundle = ROOT / "artifacts/MCP Gateway Test.app/Contents/MacOS"
    REPORT["appSHA256"] = hashlib.sha256((bundle / "mcp-gateway").read_bytes()).hexdigest()
    REPORT["coreSHA256"] = hashlib.sha256((bundle / "mcpproxy").read_bytes()).hexdigest()
    try:
        {"import-restore": import_restore, "service-tools": service_tools, "auth-forms": auth_forms, "big-diagnostics": big_diagnostics, "env-form": env_form, "oauth-form": oauth_form, "port-recovery": port_recovery, "retention-settings": retention_settings, "final-status-catalog": final_status_catalog, "diagnostics-file": diagnostics_file}[args.scenario]()
        REPORT["passed"] = True
    except Exception as exc:
        REPORT["passed"] = False
        REPORT["error"] = str(exc)
        raise
    finally:
        Path(args.report).write_text(json.dumps(REPORT, ensure_ascii=False, indent=2) + "\n")
