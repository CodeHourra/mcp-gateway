"""Target only the dedicated native theme-review app; leave user app and settings alone."""
import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
import time

from wails_mcp import call

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--profile", type=Path, required=True)
parser.add_argument("--report", type=Path, required=True)
args = parser.parse_args()
assert args.profile.resolve().parent.name.startswith("theme-ui-check-") and args.profile.resolve().parent.parent == ROOT / ".cache"
endpoint = "http://127.0.0.1:19199/mcp"
bundle = ROOT / "artifacts/MCP Gateway UI Check.app"
report = {"executedAt": datetime.now().astimezone().isoformat(), "scope": "Isolated real WKWebView DOM, native window resize, HTML radio semantics, theme colors and state. No user app actions, OS keyboard, pixel screenshot, or system appearance change.", "profile": str(args.profile), "appSHA256": hashlib.sha256((bundle / "Contents/MacOS/mcp-gateway").read_bytes()).hexdigest(), "coreSHA256": hashlib.sha256((bundle / "Contents/MacOS/mcpproxy").read_bytes()).hexdigest(), "checks": [], "passed": False}


def js(source):
    return call(endpoint, "js_eval", {"window": "manager", "js": source, "timeout_ms": 15000})


def capture(name):
    value = js(r"""
const rect=e=>{const b=e.getBoundingClientRect();return {x:b.x,y:b.y,width:b.width,height:b.height,right:b.right,bottom:b.bottom}};
const bg=e=>{while(e){const c=getComputedStyle(e).backgroundColor;if(c!=='rgba(0, 0, 0, 0)'&&c!=='transparent')return c;e=e.parentElement}return getComputedStyle(document.documentElement).backgroundColor};
const radios=selector=>[...document.querySelectorAll(selector)].map(e=>({type:e.type,name:e.name,value:e.value,checked:e.checked,disabled:e.disabled,label:[...e.labels].map(l=>l.textContent.trim()).join(' '),position:getComputedStyle(e).position,clip:getComputedStyle(e).clip}));
const fieldset=document.querySelector('.theme-picker');
return {theme:document.documentElement.dataset.theme,localTheme:localStorage.getItem('mcp-gateway-theme'),systemDark:matchMedia('(prefers-color-scheme:dark)').matches,rootBackground:getComputedStyle(document.documentElement).backgroundColor,sidebarBackground:getComputedStyle(document.querySelector('.sidebar')).backgroundColor,toolbar:radios('.theme-picker input'),settings:radios('.theme-choice input'),fieldset:{tag:fieldset.tagName,legend:fieldset.querySelector('legend').textContent,selectCount:fieldset.querySelectorAll('select').length,borderStyle:getComputedStyle(fieldset).borderStyle,borderWidth:getComputedStyle(fieldset).borderWidth,boxShadow:getComputedStyle(fieldset).boxShadow},viewport:{width:innerWidth,height:innerHeight,documentWidth:document.documentElement.clientWidth,scrollWidth:document.documentElement.scrollWidth},bounds:Object.fromEntries(['.toolbar','.toolbar-title','.theme-picker','.gateway-state'].map(s=>[s,rect(document.querySelector(s))])),segments:[...document.querySelectorAll('.theme-segment>span')].map(e=>({text:e.textContent,rect:rect(e),foreground:getComputedStyle(e).color,background:bg(e),outline:getComputedStyle(e).outline,textWidth:(()=>{const r=document.createRange();r.selectNodeContents(e);return r.getBoundingClientRect().width})(),availableTextWidth:e.clientWidth-parseFloat(getComputedStyle(e).paddingLeft)-parseFloat(getComputedStyle(e).paddingRight)})),stylesheets:[...document.querySelectorAll('link[rel=stylesheet]')].map(e=>e.href),scripts:[...document.scripts].map(e=>e.src)};
""")
    report["checks"].append({"name": name, "value": value})
    print(name, flush=True)
    assert [item["label"] for item in value["toolbar"]] == ["浅色", "深色", "跟随系统"]
    assert [item["value"] for item in value["toolbar"]] == ["light", "dark", "system"]
    assert all(item["type"] == "radio" and item["name"] == "toolbar-theme" and not item["disabled"] for item in value["toolbar"])
    assert sum(item["checked"] for item in value["toolbar"]) == 1
    assert next(item["value"] for item in value["toolbar"] if item["checked"]) == value["theme"] == value["localTheme"]
    assert value["fieldset"] == {"tag": "FIELDSET", "legend": "主题", "selectCount": 0, "borderStyle": "solid", "borderWidth": "1px", "boxShadow": "none"}
    if value["settings"]:
        assert [item["label"] for item in value["settings"]] == ["浅色", "深色", "跟随系统"]
        assert all(item["type"] == "radio" and item["name"] == "theme" for item in value["settings"])
        assert sum(item["checked"] for item in value["settings"]) == 1
        assert next(item["value"] for item in value["settings"] if item["checked"]) == value["theme"]
    dark = value["theme"] == "dark" or value["theme"] == "system" and value["systemDark"]
    assert value["rootBackground"] == ("rgb(32, 33, 36)" if dark else "rgb(255, 255, 255)")
    assert value["sidebarBackground"] == ("rgb(39, 41, 45)" if dark else "rgb(244, 245, 247)")
    assert value["viewport"]["scrollWidth"] <= value["viewport"]["documentWidth"]
    boxes = value["bounds"]
    assert boxes[".toolbar-title"]["right"] <= boxes[".theme-picker"]["x"] and boxes[".theme-picker"]["right"] <= boxes[".gateway-state"]["x"]
    assert all(0 <= box["x"] and box["right"] <= value["viewport"]["width"] for box in boxes.values())
    assert all(item["textWidth"] <= item["availableTextWidth"] + .5 for item in value["segments"])
    def luminance(color):
        rgb=[float(n)/255 for n in re.findall(r"[\d.]+", color)[:3]]
        channels=[n/12.92 if n <= .04045 else ((n+.055)/1.055)**2.4 for n in rgb]
        return sum(n*w for n,w in zip(channels,(.2126,.7152,.0722)))
    for item in value["segments"]:
        a,b=sorted((luminance(item["foreground"]),luminance(item["background"])))
        item["contrastRatio"] = round((b+.05)/(a+.05),3)
        assert item["contrastRatio"] >= 4.5
    return value


def choose(scope, value):
    selector = scope + ' input[value="' + value + '"]'
    js("const e=document.querySelector(" + json.dumps(selector) + ");if(!e)throw Error('Radio missing');e.closest('label').click();await Promise.resolve();await Promise.resolve();return true")


settings_before = (args.profile / "settings.json").read_bytes()
baseline = capture("initial")
original_size = call(endpoint, "app_info", {})["windows"][0]
try:
    state=js("const s=await window.gateway.request('snapshot');return {settings:s.settings,serviceCount:s.services.length}")
    assert state["settings"]["listenAddress"] == "127.0.0.1:19241" and state["serviceCount"] == 0
    for mode in ("light", "dark", "system"):
        choose(".theme-picker", mode)
        capture("toolbar_" + mode)
    js("document.querySelector('.settings-nav').click();await Promise.resolve();return true")
    for mode in ("light", "dark", "system"):
        choose(".theme-picker", mode)
        capture("toolbar_to_settings_" + mode)
    for mode in ("light", "dark", "system"):
        choose(".theme-cards", mode)
        capture("settings_to_toolbar_" + mode)
    native_size=call(endpoint, "window_control", {"window": "manager", "action": "set_size", "width": 760, "height": 560})
    report["nativeMinimumSizeRequest"] = {"width":760,"height":560,"result":native_size}
    for _ in range(40):
        if abs(js("return innerWidth") - 760) <= 1: break
        time.sleep(.1)
    for mode in ("light", "dark", "system"):
        choose(".theme-picker", mode)
        narrow=capture("minimum_window_" + mode)
        assert native_size["width"] == 760 and abs(narrow["viewport"]["width"] - 760) <= 1
    report["focusEvidence"] = js("const e=document.querySelector('.theme-picker input:checked');e.focus({focusVisible:true});const rules=[...document.styleSheets].flatMap(s=>[...s.cssRules]).filter(r=>r.selectorText?.includes('.theme-segment input:focus-visible')).map(r=>({selector:r.selectorText,outline:r.style.outline,outlineOffset:r.style.outlineOffset}));return {focused:document.activeElement===e,focusVisibleMatched:e.matches(':focus-visible'),inputOutline:getComputedStyle(e).outline,segmentOutline:getComputedStyle(e.nextElementSibling).outline,selectorSupported:CSS.supports('selector(:focus-visible)'),rules,scope:'Programmatic focus and runtime CSS rules only; not actual OS Tab/arrow keys'}")
    assert report["focusEvidence"]["focused"] and report["focusEvidence"]["selectorSupported"]
    assert len(report["focusEvidence"]["rules"]) == 2
    assert any(item["outline"].startswith("2px solid") for item in report["focusEvidence"]["rules"])
    assert (args.profile / "settings.json").read_bytes() == settings_before
    report["backendSettingsUnchanged"] = True
    report["passed"] = True
finally:
    choose(".theme-picker", baseline["theme"])
    js("document.activeElement?.blur();document.querySelector('.nav-button').click();await Promise.resolve();return true")
    call(endpoint, "window_control", {"window":"manager","action":"set_size","width":original_size["width"],"height":original_size["height"]})
    report["restoredTheme"] = js("return document.documentElement.dataset.theme")
    args.report.write_text(json.dumps(report,ensure_ascii=False,indent=2)+"\n")
