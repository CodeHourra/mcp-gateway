"""Collect notices from the exact Go modules and frontend packages being bundled."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys


def run_json(command, cwd):
    """Run a listing command and return UTF-8 text.

    npm is npm.cmd on Windows and CreateProcess cannot launch it directly, so
    that one goes through cmd.exe; go resolves to go.exe and needs no help.
    """
    if os.name == "nt" and command[0].lower().endswith((".cmd", ".bat")):
        command = ["cmd", "/c", subprocess.list2cmdline(command)]
    return subprocess.check_output(command, cwd=cwd, text=True, encoding="utf-8")


destination = Path(sys.argv[1])
destination.mkdir(parents=True, exist_ok=True)
modules = {}
decoder = json.JSONDecoder()
for directory in sys.argv[2:]:
    stream = run_json(["go", "list", "-deps", "-json", "./..."], directory)
    offset = 0
    while offset < len(stream):
        while offset < len(stream) and stream[offset].isspace():
            offset += 1
        if offset == len(stream):
            break
        package, offset = decoder.raw_decode(stream, offset)
        module = package.get("Module", {})
        if module.get("Version") and module.get("Dir"):
            modules[module["Path"] + "@" + module["Version"]] = Path(module["Dir"])

frontend = Path(__file__).resolve().parents[1] / "frontend"
npm = shutil.which("npm") or "npm"
for directory in run_json([npm, "ls", "--omit=dev", "--all", "--parseable"], frontend).splitlines():
    path = Path(directory)
    if path == frontend:
        continue
    manifest = json.loads((path / "package.json").read_text(encoding="utf-8"))
    modules[manifest["name"] + "@" + manifest["version"]] = path

manifest = {}
for name, directory in sorted(modules.items()):
    notices = []
    for file in directory.iterdir():
        if file.is_file() and file.suffix.lower() not in (".go", ".js", ".ts", ".py", ".c", ".h") and file.name.lower().startswith(("license", "licence", "notice", "copying")):
            target = name.replace("/", "__") + "__" + file.name + ".txt"
            shutil.copyfile(file, destination / target)
            notices.append(target)
    manifest[name] = notices
go_root = Path(run_json(["go", "env", "GOROOT"], None).strip())
shutil.copyfile(go_root / "LICENSE", destination / "Go-runtime-LICENSE")
(destination / "versions.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
print(f"Collected notices for {len(manifest)} dependency modules.")
