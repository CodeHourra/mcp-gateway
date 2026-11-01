package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeApp builds a minimal bundle whose only payload is a version marker.
func fakeApp(t *testing.T, dir, version string, sign bool) string {
	t.Helper()
	app := filepath.Join(dir, "MCP Gateway.app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>mcp-gateway</string><key>CFBundleIdentifier</key><string>test.mcpgw.fake</string></dict></plist>`
	for path, data := range map[string]string{
		filepath.Join(app, "Contents", "Info.plist"): plist,
		filepath.Join(macos, "mcp-gateway"):          "#!/bin/sh\n",
		filepath.Join(app, "Contents", "version"):    version,
	} {
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if sign {
		if out, err := exec.Command("codesign", "-s", "-", "--force", "--deep", app).CombinedOutput(); err != nil {
			t.Fatalf("codesign: %v\n%s", err, out)
		}
	}
	return app
}

// runInstallScript installs a dmg holding newApp over an old bundle and returns
// the bundle path, the launcher marker and the log.
func runInstallScript(t *testing.T, signNew bool) (app, launched, log, dmg, script string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS install script needs hdiutil and codesign")
	}
	root := t.TempDir()
	app = fakeApp(t, filepath.Join(mkdir(t, root, "apps")), "old", true)
	src := mkdir(t, root, "dmg-src")
	fakeApp(t, src, "new", signNew)
	dmg = filepath.Join(root, "update.dmg")
	if out, err := exec.Command("hdiutil", "create", "-quiet", "-fs", "HFS+", "-srcfolder", src, "-volname", "T", dmg).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v\n%s", err, out)
	}
	script = filepath.Join(root, "update.sh")
	if err := os.WriteFile(script, []byte(macInstallScript), 0700); err != nil {
		t.Fatal(err)
	}
	launched = filepath.Join(root, "launched")
	launcher := filepath.Join(root, "launcher.sh")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf %s \"$1\" > "+launched+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(root, "update.log")
	// A process that already exited stands in for the old app.
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("/bin/sh", script, strconv.Itoa(gone.Process.Pid), dmg, app, launcher, log).CombinedOutput()
	t.Logf("script output: %s", out)
	return app, launched, log, dmg, script
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMacInstallScriptReplacesAndRelaunches(t *testing.T) {
	app, launched, _, dmg, script := runInstallScript(t, true)
	if v, _ := os.ReadFile(filepath.Join(app, "Contents", "version")); string(v) != "new" {
		t.Fatalf("bundle not replaced, version=%q", v)
	}
	if got, _ := os.ReadFile(launched); string(got) != app {
		t.Fatalf("launcher got %q, want %q", got, app)
	}
	for _, leftover := range []string{app + ".update-old", app + ".update-new", dmg, script} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Errorf("%s should be gone", leftover)
		}
	}
}

func TestMacInstallScriptKeepsOldAppWhenSignatureInvalid(t *testing.T) {
	app, launched, log, dmg, _ := runInstallScript(t, false)
	if v, _ := os.ReadFile(filepath.Join(app, "Contents", "version")); string(v) != "old" {
		t.Fatalf("old bundle must stay in place, version=%q", v)
	}
	if got, _ := os.ReadFile(launched); string(got) != app {
		t.Fatalf("old app must be relaunched, launcher got %q", got)
	}
	if _, err := os.Stat(app + ".update-new"); !os.IsNotExist(err) {
		t.Error("staged copy must be removed")
	}
	if _, err := os.Stat(dmg); err != nil {
		t.Error("installer must be kept after a failed update")
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "签名校验失败") {
		t.Errorf("failure not logged: %s", b)
	}
}
