#!/usr/bin/env bash
# Single source of truth for the released version: build/Info.plist.
# Reads it, or writes the same value into every other place that carries it.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
plist="$root/build/Info.plist"
pattern='^[0-9]+\.[0-9]+\.[0-9]+$'

# Read and write in place so the checked-in layout of Info.plist is preserved.
read_version() { sed -n 's|.*<key>CFBundleShortVersionString</key><string>\([^<]*\)</string>.*|\1|p' "$plist"; }

set_version() {
  local version="$1"
  [[ "$version" =~ $pattern ]] || { echo "版本号必须是 X.Y.Z：$version" >&2; exit 1; }
  # BSD and GNU sed disagree on -i; go through a temp file so both work.
  sed "s|<key>CFBundleShortVersionString</key><string>[^<]*</string>|<key>CFBundleShortVersionString</key><string>$version</string>|" "$plist" > "$plist.tmp"
  mv "$plist.tmp" "$plist"
  python3 - "$root" "$version" <<'PY'
import json, pathlib, re, sys
root, version = pathlib.Path(sys.argv[1]), sys.argv[2]
package = root / "frontend/package.json"
data = json.loads(package.read_text())
data["version"] = version
package.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
lock = root / "frontend/package-lock.json"
text = lock.read_text()
lock.write_text(re.sub(r'("version":\s*)"[^"]*"', r'\g<1>"%s"' % version, text, count=2))
manager = root / "internal/gateway/manager.go"
source = manager.read_text()
new, replaced = re.subn(r'(?m)^(const Version = ")[^"]*(")', r'\g<1>%s\2' % version, source, count=1)
if not replaced:
    sys.exit("internal/gateway/manager.go 未找到 const Version")
manager.write_text(new)
PY
  echo "version $version"
}

case "${1:-read}" in
  read) read_version ;;
  set)
    [[ $# -eq 2 ]] || { echo "用法: $0 set X.Y.Z" >&2; exit 2; }
    set_version "$2"
    ;;
  *) echo "用法: $0 [read|set X.Y.Z]" >&2; exit 2 ;;
esac
