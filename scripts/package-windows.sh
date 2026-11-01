#!/usr/bin/env bash
# Build the Windows production bundle from the same sources and scripts as macOS.
# Produces bin/MCP-Gateway-<version>-windows-amd64.zip containing the app, the
# gateway core, licences and the install notes.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
version="$("$root/scripts/version.sh" read)"
# Git Bash has no python3 and no sha256sum; find whatever this host provides.
py="$(command -v python3 || command -v python)"
export GOPROXY="${MCP_GATEWAY_GOPROXY:-https://proxy.golang.org,direct}"
export GOSUMDB=sum.golang.org
export GOTOOLCHAIN=auto
export GOMODCACHE="${GOMODCACHE:-$root/.cache/go-mod}"
export GOCACHE="${GOCACHE:-$root/.cache/go-build}"

mkdir -p .cache/bin bin
if [ ! -x .cache/bin/wails3 ] && [ ! -x .cache/bin/wails3.exe ]; then
  GOBIN="$root/.cache/bin" go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.16
fi
wails3=".cache/bin/wails3"
[ -x "$wails3" ] || wails3=".cache/bin/wails3.exe"
"$wails3" generate bindings -ts -b -d frontend/bindings ./...
npm --prefix frontend ci
npm --prefix frontend run build
# Host-native checks; only the Windows artefacts cross-compile. Git Bash reports
# MINGW*/MSYS*, not Windows_NT, so match those too.
# The unit and acceptance suites assert POSIX permission bits (0600 files,
# unreadable directories) and drive /bin/sh children, which Windows cannot
# satisfy, so a Windows host gets vet plus the builds instead of the suite.
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT) windows_host=1 ;;
  *) windows_host=0 ;;
esac
if [ "$windows_host" = 1 ]; then
  go vet ./...
else
  go test ./...
fi

stage="$root/.cache/windows-build"
rm -rf "$stage"
mkdir -p "$stage/Licenses"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags production -trimpath -buildvcs=false \
  -ldflags="-w -s -H windowsgui" -o "$stage/mcp-gateway.exe" .
source_dir="$root/.cache/mcpproxy-src"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 "$root/scripts/build-core.sh" "$stage/mcpproxy.exe" "$source_dir"
"$py" scripts/licenses.py "$stage/Licenses" "$root" "$source_dir"
cp "$source_dir/LICENSE" "$stage/Licenses/MCPProxy-Go.txt"
rm -rf "$source_dir"
cp docs/install-and-verify-windows.txt "$stage/安装与测试说明.txt"

name="MCP-Gateway-$version-windows-amd64.zip"
rm -f "bin/$name"
# zipdir.py instead of Compress-Archive: PowerShell writes backslash entry
# names, which unzip/7-Zip/macOS then read as one flat filename.
"$py" scripts/zipdir.py "$stage" "$root/bin/$name"
(cd "$root/bin" && {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$name"
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$name"
  else powershell -NoProfile -Command "(Get-FileHash '$(cygpath -w "$root/bin/$name")' -Algorithm SHA256).Hash.ToLower() + '  $name'"
  fi
} > "$name.sha256")
printf 'Packaged %s\n' "$root/bin/$name"
