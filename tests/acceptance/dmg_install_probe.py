"""Check the local DMG and reuse the production probe after copying it off the image."""
import argparse
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import plistlib
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--dmg", type=Path, default=ROOT / "bin/MCP-Gateway-0.1.0-arm64.dmg")
parser.add_argument("--report", type=Path, default=ROOT / "tests/acceptance/dmg-install-2026-09-07.json")
parser.add_argument("--production-report", type=Path, default=ROOT / "tests/acceptance/dmg-production-app-2026-09-07.json")
args = parser.parse_args()


def run(*command):
    result = subprocess.run(command, capture_output=True, check=True)
    return result.stdout


def inventory(folder):
    entries = {}
    for path in [folder, *sorted(folder.rglob("*"))]:
        meta = path.lstat()
        item = {"mode": oct(stat.S_IMODE(meta.st_mode))}
        if path.is_symlink():
            item.update(type="symlink", target=os.readlink(path))
        elif path.is_dir():
            item["type"] = "directory"
        elif path.is_file():
            item.update(type="file", bytes=meta.st_size, sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        else:
            raise AssertionError("Unexpected bundle entry: " + str(path))
        entries[str(path.relative_to(folder))] = item
    return entries


report = {"executedAt": datetime.now().astimezone().isoformat(), "scope": "Local DMG integrity, mounted contents, complete bundle bytes/symlinks/modes, guide, and copied app startup after eject. No /Applications writes, user configuration, Gatekeeper, Finder drag, or OS menu claims.", "dmg": str(args.dmg.resolve()), "passed": False}
workspace = Path(tempfile.mkdtemp(prefix="dmg-acceptance-", dir=ROOT / ".cache"))
mountpoint = workspace / "mounted"
mountpoint.mkdir()
owned_device = None
try:
    expected_hash, expected_name = args.dmg.with_suffix(".dmg.sha256").read_text().split()
    actual_hash = hashlib.sha256(args.dmg.read_bytes()).hexdigest()
    assert expected_name == args.dmg.name and actual_hash == expected_hash
    report.update(dmgSHA256=actual_hash, dmgBytes=args.dmg.stat().st_size, adjacentChecksumMatches=True)
    run("bash", "-n", str(ROOT / "scripts/package-dmg.sh"))
    report["packagingScriptSyntaxValid"] = True
    report["hdiutilVerify"] = run("hdiutil", "verify", str(args.dmg)).decode().strip()
    attached = plistlib.loads(run("hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen", "-mountpoint", str(mountpoint), "-plist", str(args.dmg)))
    entities = attached["system-entities"]
    owned_device = next(item["dev-entry"] for item in entities if "mount-point" in item)
    assert next(item["mount-point"] for item in entities if "mount-point" in item) == str(mountpoint)
    report["mount"] = {"device": owned_device, "path": str(mountpoint), "readOnlyRequested": True}
    report["volumeEntries"] = sorted(path.name for path in mountpoint.iterdir())
    assert {path.name for path in mountpoint.iterdir() if not path.name.startswith(".")} == {"MCP Gateway.app", "Applications", "安装与测试说明.txt"}
    assert (mountpoint / "Applications").is_symlink() and os.readlink(mountpoint / "Applications") == "/Applications"
    report["applicationsLink"] = os.readlink(mountpoint / "Applications")
    guide = (mountpoint / "安装与测试说明.txt").read_bytes()
    assert guide == (ROOT / "docs/install-and-verify.txt").read_bytes()
    report["guide"] = {"filename": "安装与测试说明.txt", "bytes": len(guide), "sha256": hashlib.sha256(guide).hexdigest(), "exactSourceBytes": True, "mode": oct(stat.S_IMODE((mountpoint / "安装与测试说明.txt").stat().st_mode))}
    expected = inventory(ROOT / "bin/MCP Gateway.app")
    assert inventory(mountpoint / "MCP Gateway.app") == expected
    report["mountedBundleMatchesSource"] = True
    copied = workspace / "MCP Gateway.app"
    run("ditto", str(mountpoint / "MCP Gateway.app"), str(copied))
    assert inventory(copied) == expected
    report["copiedBundleMatchesSource"] = True
    report["bundleInventory"] = expected
    report["bundleEntryCounts"] = {kind: sum(item["type"] == kind for item in expected.values()) for kind in ("file", "directory", "symlink")}
    run("codesign", "--verify", "--deep", "--strict", str(copied))
    report["copiedCodeSignatureValid"] = True
    report["architectures"] = {name: run("lipo", "-archs", str(copied / "Contents/MacOS" / name)).decode().strip() for name in ("mcp-gateway", "mcpproxy")}
    assert set(report["architectures"].values()) == {"arm64"}
    info = plistlib.loads((copied / "Contents/Info.plist").read_bytes())
    report["bundleInfo"] = {key: info.get(key) for key in ("CFBundleIdentifier", "CFBundleShortVersionString", "CFBundleVersion", "LSMinimumSystemVersion")}
    run("hdiutil", "detach", owned_device)
    owned_device = None
    mounts = plistlib.loads(run("hdiutil", "info", "-plist"))
    assert all(entity.get("mount-point") != str(mountpoint) for image in mounts.get("images", []) for entity in image.get("system-entities", []))
    report["imageDetachedBeforeLaunch"] = True
    report["copiedApp"] = str(copied)
    print("DMG contents verified; image detached; starting copied-app probe", flush=True)
    result = subprocess.run([sys.executable, str(ROOT / "tests/acceptance/production_app_probe.py"), "--app", str(copied), "--report", str(args.production_report)], capture_output=True, text=True)
    report["productionProbeExitCode"] = result.returncode
    assert result.returncode == 0, result.stderr
    production = json.loads(args.production_report.read_text())
    assert production["passed"] and production["appExitCode"] == 0 and production["ownedCoreExited"] and production["temporaryAdminKeyDeleted"] and not production.get("cleanupPending")
    assert production["appSHA256"] == expected["Contents/MacOS/mcp-gateway"]["sha256"] and production["coreSHA256"] == expected["Contents/MacOS/mcpproxy"]["sha256"]
    report["productionReport"] = str(args.production_report)
    report["passed"] = True
except Exception as exc:
    report["error"] = str(exc)
    raise
finally:
    if owned_device:
        detach = subprocess.run(["hdiutil", "detach", owned_device], capture_output=True)
        report["failureCleanupDetachedImage"] = detach.returncode == 0
    args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({key: value for key, value in report.items() if key != "bundleInventory"}, ensure_ascii=False, indent=2))
