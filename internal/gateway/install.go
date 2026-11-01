package gateway

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrManualInstall means the running app cannot be replaced in place (not run
// from an app bundle, install directory not writable, unsupported platform).
// The verified installer is still on disk for the user to open.
var ErrManualInstall = errors.New("无法原地替换当前应用")

// macInstallScript runs after the old process exited. Arguments: pid, dmg, app
// bundle, launcher, log. The old bundle is only moved aside once the new one is
// copied and passes codesign; any failure puts the old one back and relaunches it.
const macInstallScript = `#!/bin/sh
pid=$1; dmg=$2; app=$3; launcher=$4; log=$5
note() { printf '%s %s\n' "$(date '+%F %T')" "$1" >> "$log"; }
fail() {
  note "更新失败: $1"
  [ -d "$app" ] && "$launcher" "$app"
  /usr/bin/osascript -e 'display notification "更新失败，已保留当前版本。详情见数据目录 update.log" with title "MCP Gateway"' >/dev/null 2>&1
  exit 1
}
i=0
while kill -0 "$pid" 2>/dev/null; do
  i=$((i+1)); [ "$i" -gt 300 ] && fail "等待旧版本退出超时"
  sleep 0.2
done
mnt=$(mktemp -d "${TMPDIR:-/tmp}/mcpgw-update.XXXXXX") || fail "无法创建挂载点"
detach() { hdiutil detach "$mnt" -quiet >/dev/null 2>&1 || hdiutil detach -force "$mnt" -quiet >/dev/null 2>&1; rmdir "$mnt" 2>/dev/null; }
hdiutil attach -quiet -noverify -nobrowse -readonly -noautoopen -mountpoint "$mnt" "$dmg" >>"$log" 2>&1 || { rmdir "$mnt" 2>/dev/null; fail "无法挂载安装包"; }
new="$app.update-new"; old="$app.update-old"
rm -rf "$new" "$old"
ditto "$mnt/MCP Gateway.app" "$new" >>"$log" 2>&1 || { detach; rm -rf "$new"; fail "复制新版本失败"; }
detach
codesign --verify --deep --strict "$new" >>"$log" 2>&1 || { rm -rf "$new"; fail "新版本签名校验失败"; }
mv "$app" "$old" || { rm -rf "$new"; fail "无法移开旧版本"; }
if ! mv "$new" "$app"; then mv "$old" "$app"; rm -rf "$new"; fail "无法放入新版本"; fi
rm -rf "$old" "$dmg" "$0"
note "更新完成"
"$launcher" "$app"
`

// winInstallScript runs after the old process exited. The zip is extracted to a
// staging directory first; top-level entries are then swapped in with the old
// ones kept in a backup directory, restored if any step fails.
const winInstallScript = `param($ProcId, $Zip, $Dir, $Exe, $Log)
$ErrorActionPreference = 'Stop'
function Note($m) { Add-Content -LiteralPath $Log -Value ("{0} {1}" -f (Get-Date -Format 's'), $m) -Encoding UTF8 }
function Fail($m) {
  Note "更新失败: $m"
  if (Test-Path -LiteralPath $Exe) { Start-Process -FilePath $Exe }
  exit 1
}
function Retry($block) {
  for ($i = 0; $i -lt 20; $i++) {
    try { & $block; return } catch { if ($i -eq 19) { throw }; Start-Sleep -Milliseconds 500 }
  }
}
$p = Get-Process -Id $ProcId -ErrorAction SilentlyContinue
if ($p -and -not $p.WaitForExit(60000)) { Fail '等待旧版本退出超时' }
$stage = Join-Path $env:TEMP ('mcpgw-update-' + [guid]::NewGuid().ToString('N'))
$bak = Join-Path $Dir '.update-old'
$moved = @()
try {
  if (Test-Path -LiteralPath $bak) { Remove-Item -LiteralPath $bak -Recurse -Force }
  Expand-Archive -LiteralPath $Zip -DestinationPath $stage -Force
  foreach ($need in 'mcp-gateway.exe', 'mcpproxy.exe') {
    if (-not (Test-Path -LiteralPath (Join-Path $stage $need))) { throw "安装包缺少 $need" }
  }
  New-Item -ItemType Directory -Path $bak | Out-Null
  foreach ($item in Get-ChildItem -LiteralPath $stage -Force) {
    $target = Join-Path $Dir $item.Name
    if (Test-Path -LiteralPath $target) {
      Retry { Move-Item -LiteralPath $target -Destination (Join-Path $bak $item.Name) -Force }
    }
    $moved += $item.Name
    Retry { Copy-Item -LiteralPath $item.FullName -Destination $target -Recurse -Force }
  }
} catch {
  $reason = $_.Exception.Message
  foreach ($name in $moved) {
    $target = Join-Path $Dir $name
    if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue }
    $saved = Join-Path $bak $name
    if (Test-Path -LiteralPath $saved) { Move-Item -LiteralPath $saved -Destination $target -Force -ErrorAction SilentlyContinue }
  }
  Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
  Fail $reason
}
Remove-Item -LiteralPath $bak, $stage, $Zip -Recurse -Force -ErrorAction SilentlyContinue
Note '更新完成'
Start-Process -FilePath $Exe
Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
`

// PrepareInstall checks that the running app can be replaced in place and
// writes the helper script, so every failure surfaces before the app quits. The
// returned start function launches the detached helper; call it as the last act
// of shutdown, because the helper only waits for this process to exit.
func PrepareInstall(installer, dataDir string) (start func() error, err error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	pid := strconv.Itoa(os.Getpid())
	log := filepath.Join(dataDir, "update.log")
	scriptDir := filepath.Dir(installer)

	var target, script string
	var argv []string
	var body string
	switch runtime.GOOS {
	case "darwin":
		// <app>/Contents/MacOS/mcp-gateway
		target = filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if !strings.HasSuffix(target, ".app") {
			return nil, fmt.Errorf("%w：当前不是从应用包运行", ErrManualInstall)
		}
		script, body = filepath.Join(scriptDir, "update.sh"), macInstallScript
		argv = []string{"/bin/sh", script, pid, installer, target, "/usr/bin/open", log}
	case "windows":
		target = filepath.Dir(exe)
		script, body = filepath.Join(scriptDir, "update.ps1"), "\xEF\xBB\xBF"+winInstallScript
		argv = []string{"powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", script,
			"-ProcId", pid, "-Zip", installer, "-Dir", target, "-Exe", exe, "-Log", log}
	default:
		return nil, fmt.Errorf("%w：%s 不支持自动更新", ErrManualInstall, runtime.GOOS)
	}
	parent := target
	if runtime.GOOS == "darwin" {
		parent = filepath.Dir(target) // the bundle is renamed, so its parent must be writable
	}
	probe, err := os.CreateTemp(parent, ".mcpgw-write-test-*")
	if err != nil {
		return nil, fmt.Errorf("%w：%s 不可写", ErrManualInstall, parent)
	}
	probe.Close()
	os.Remove(probe.Name())
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		return nil, err
	}
	return func() error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.SysProcAttr = detachedAttr()
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动更新程序失败: %w", err)
		}
		return cmd.Process.Release()
	}, nil
}
