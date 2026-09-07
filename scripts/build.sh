#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
export GOPROXY="${MCP_GATEWAY_GOPROXY:-https://proxy.golang.org,direct}"
export GOSUMDB=sum.golang.org
export GOTOOLCHAIN=auto
export GOMODCACHE="${GOMODCACHE:-$root/.cache/go-mod}"
export GOCACHE="${GOCACHE:-$root/.cache/go-build}"
export MACOSX_DEPLOYMENT_TARGET=15.0
export CGO_CFLAGS="-mmacosx-version-min=15.0"
export CGO_LDFLAGS="-mmacosx-version-min=15.0"
mkdir -p .cache/bin bin
if [ ! -x .cache/bin/wails3 ]; then
  GOBIN="$root/.cache/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.16
fi
.cache/bin/wails3 generate bindings -ts -b -d frontend/bindings ./...
npm --prefix frontend ci
npm --prefix frontend run build
go test ./...
bundle="$root/bin/MCP Gateway.app"
mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources/Licenses"
go build -tags production -trimpath -buildvcs=false -ldflags="-w -s" -o "$bundle/Contents/MacOS/mcp-gateway" .

source_dir="$(mktemp -d "$root/.cache/core-build.XXXXXX")"
trap 'rm -rf "$source_dir"' EXIT
git clone --quiet --depth 1 --branch v0.65.0 https://github.com/smart-mcp-proxy/mcpproxy-go.git "$source_dir"
test "$(git -C "$source_dir" rev-parse HEAD)" = 308a81272844df896b616b886295305d97f90f8d
for patch in "$root"/patches/mcpproxy/*.patch; do
  test -f "$patch"
  git -C "$source_dir" apply --check "$patch"
  git -C "$source_dir" apply "$patch"
done
(cd "$source_dir" && CGO_ENABLED=1 go build -trimpath -buildvcs=false -ldflags '-s -w -X main.version=v0.65.0-gateway.1 -X github.com/smart-mcp-proxy/mcpproxy-go/internal/httpapi.buildVersion=v0.65.0-gateway.1' -o "$bundle/Contents/MacOS/mcpproxy" ./cmd/mcpproxy)
rm -rf "$bundle/Contents/Resources/Licenses"
python3 scripts/licenses.py "$bundle/Contents/Resources/Licenses" "$root" "$source_dir"
cp "$source_dir/LICENSE" "$bundle/Contents/Resources/Licenses/MCPProxy-Go.txt"
cp build/Info.plist "$bundle/Contents/Info.plist"
mkdir -p .cache/Gateway.iconset
CLANG_MODULE_CACHE_PATH="$root/.cache/swift-modules" SWIFT_MODULECACHE_PATH="$root/.cache/swift-modules" swift build/icon.swift "$root/.cache/Gateway.iconset"
iconutil -c icns .cache/Gateway.iconset -o "$bundle/Contents/Resources/Gateway.icns"
codesign --force --sign - "$bundle/Contents/MacOS/mcpproxy"
codesign --force --deep --sign - "$bundle"
codesign --verify --deep --strict "$bundle"
printf 'Built %s\n' "$bundle"
