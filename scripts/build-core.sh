#!/usr/bin/env bash
# Clone the pinned MCPProxy source, apply the gateway patches, and build the core.
#
# Usage: build-core.sh <output-binary> [source-dir-to-keep]
# GOOS/GOARCH/CGO_ENABLED come from the environment so macOS and Windows use the
# same source, patches and flags. With a source dir argument the patched tree is
# left in place for the licence collector and the caller must remove it.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:?用法: build-core.sh <输出路径> [保留源码目录]}"
keep="${2:-}"
mkdir -p "$(dirname "$out")"

if [ -n "$keep" ]; then
  source_dir="$keep"
  rm -rf "$source_dir"
  mkdir -p "$source_dir"
else
  source_dir="$(mktemp -d "$root/.cache/core-build.XXXXXX")"
  trap 'rm -rf "$source_dir"' EXIT
fi
git -c core.autocrlf=false clone --quiet --depth 1 --branch v0.65.0 https://github.com/smart-mcp-proxy/mcpproxy-go.git "$source_dir"
test "$(git -C "$source_dir" rev-parse HEAD)" = 308a81272844df896b616b886295305d97f90f8d
for patch in "$root"/patches/mcpproxy/*.patch; do
  test -f "$patch"
  git -C "$source_dir" apply --check "$patch"
  git -C "$source_dir" apply "$patch"
done
(cd "$source_dir" && go build -trimpath -buildvcs=false \
  -ldflags '-s -w -X main.version=v0.65.0-gateway.1 -X github.com/smart-mcp-proxy/mcpproxy-go/internal/httpapi.buildVersion=v0.65.0-gateway.1' \
  -o "$out" ./cmd/mcpproxy)
test -f "$out"
printf 'Built core %s\n' "$out"
