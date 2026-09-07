"""Collect notices from the exact Go modules and frontend packages being bundled."""
import json
from pathlib import Path
import shutil
import subprocess
import sys

destination = Path(sys.argv[1])
destination.mkdir(parents=True, exist_ok=True)
modules = {}
decoder = json.JSONDecoder()
for directory in sys.argv[2:]:
    stream = subprocess.check_output(["go", "list", "-deps", "-json", "./..."], cwd=directory, text=True)
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
for directory in subprocess.check_output(["npm", "ls", "--omit=dev", "--all", "--parseable"], cwd=frontend, text=True).splitlines():
    path = Path(directory)
    if path == frontend:
        continue
    manifest = json.loads((path / "package.json").read_text())
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
go_root = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
shutil.copyfile(go_root / "LICENSE", destination / "Go-runtime-LICENSE")
(destination / "versions.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
print(f"Collected notices for {len(manifest)} dependency modules.")
