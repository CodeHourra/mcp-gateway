#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
bundle="$root/bin/MCP Gateway.app"
test -x "$bundle/Contents/MacOS/mcp-gateway"
test -x "$bundle/Contents/MacOS/mcpproxy"
test "$(lipo -archs "$bundle/Contents/MacOS/mcp-gateway")" = arm64
test "$(lipo -archs "$bundle/Contents/MacOS/mcpproxy")" = arm64
codesign --verify --deep --strict "$bundle"
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$bundle/Contents/Info.plist")
name="MCP-Gateway-$version-arm64.dmg"
mkdir -p "$root/.cache" "$root/bin"
stage=$(mktemp -d "$root/.cache/dmg.XXXXXX")
trap 'rm -rf "$stage"' EXIT
mkdir "$stage/contents"
ditto "$bundle" "$stage/contents/MCP Gateway.app"
ln -s /Applications "$stage/contents/Applications"
cp "$root/docs/install-and-verify.txt" "$stage/contents/安装与测试说明.txt"
hdiutil create -volname 'MCP Gateway' -fs HFS+ -format UDZO -nospotlight \
  -srcfolder "$stage/contents" "$stage/$name"
hdiutil verify "$stage/$name"
mv "$stage/$name" "$root/bin/$name"
(cd "$root/bin" && shasum -a 256 "$name" > "$name.sha256")
printf 'Packaged %s\n' "$root/bin/$name"
