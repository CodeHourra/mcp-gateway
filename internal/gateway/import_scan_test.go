package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func importScanTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	home := t.TempDir()
	for name, value := range map[string]string{
		"HOME": home, "CODEX_HOME": filepath.Join(home, "codex"),
		"CODEBUDDY_CONFIG_DIR": filepath.Join(home, "buddy"), "PI_CODING_AGENT_DIR": filepath.Join(home, "omp"),
		"OMP_PROFILE": "", "PI_PROFILE": "", "PI_CONFIG_DIR": "",
	} {
		t.Setenv(name, value)
	}
	m, err := New(filepath.Join(home, "gateway"), "unused-core")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(m.Dir, "core.json"), Object{"mcpServers": []any{}}); err != nil {
		t.Fatal(err)
	}
	return m, home
}

func importScanFileState(t *testing.T, root string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%s:%d", info.Mode(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += ":" + digest(content)
		}
		state[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func scanImportItems(t *testing.T, m *Manager) map[string]Object {
	t.Helper()
	result, err := m.Request(t.Context(), "scanImportSources", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]Object{}
	for _, value := range array(object(result)["items"]) {
		item := object(value)
		if len(item) != 7 {
			t.Fatalf("scan returned fields outside its summary contract: %v", item)
		}
		items[stringValue(item["id"])] = item
	}
	if len(items) != 5 {
		t.Fatalf("got %d import sources, want five", len(items))
	}
	return items
}

func TestScanImportSourcesSummariesAndFreshPreview(t *testing.T) {
	m, home := importScanTestManager(t)
	marker := filepath.Join(home, "must-not-execute")
	files := map[string]string{
		filepath.Join(home, "omp", "mcp.json"):      fmt.Sprintf(`{"mcpServers":{"working":{"command":"node","env":{"TOKEN":"omp-secret"}},"blocked":{"command":%q}}}`, "!touch "+marker),
		filepath.Join(home, ".claude.json"):         `{"mcpServers":{"parser-secret":`,
		filepath.Join(home, "buddy", "mcp.json"):    "// CodeBuddy comment\n" + `{"mcpServers":{"private-buddy-service":{"url":"https://example.com/mcp","headers":{"Authorization":"Bearer header-secret"},},},}`,
		filepath.Join(home, "codex", "config.toml"): "model = \"unrelated\"\n",
	}
	for path, content := range files {
		if err := AtomicWrite(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	before := importScanFileState(t, home)
	items := scanImportItems(t, m)
	for id, status := range map[string]string{"omp": "found", "claude-code": "invalid", "cursor": "unavailable", "codebuddy": "found", "codex": "empty"} {
		if items[id]["status"] != status {
			t.Fatalf("%s: got %v, want %s", id, items[id], status)
		}
	}
	if items["omp"]["serviceCount"] != 2 || items["omp"]["blockedCount"] != 1 || items["codebuddy"]["serviceCount"] != 1 {
		t.Fatalf("wrong service/blocked counts: %v", items)
	}
	encoded, _ := json.Marshal(items)
	for _, secret := range []string{"omp-secret", "parser-secret", "header-secret", "private-buddy-service", "!touch"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("scan exposed source content: %s", secret)
		}
	}
	if len(m.previews) != 0 || !reflect.DeepEqual(before, importScanFileState(t, home)) {
		t.Fatal("scan wrote state, executed a command, or cached imported configuration")
	}

	// A newly preferred CodeBuddy file must replace the path observed by the scan.
	currentPath := filepath.Join(home, "buddy", ".mcp.json")
	current := "// newer active file\n" + `{"mcpServers":{"current":{"url":"https://new.example/mcp","headers":{"Authorization":"Bearer updated-secret"}},"second":{"command":"node"},},}`
	if err := AtomicWrite(currentPath, []byte(current)); err != nil {
		t.Fatal(err)
	}
	before = importScanFileState(t, home)
	preview, err := m.Request(t.Context(), "previewScannedImport", `{"sourceId":"codebuddy"}`)
	if err != nil {
		t.Fatal(err)
	}
	view := object(preview)
	if view["source"] != "CodeBuddy" || len(array(view["items"])) != 2 {
		t.Fatalf("preview did not parse the current CodeBuddy JSONC file: %v", view)
	}
	encoded, _ = json.Marshal(preview)
	if bytes.Contains(encoded, []byte("updated-secret")) || bytes.Contains(encoded, []byte("private-buddy-service")) {
		t.Fatal("preview exposed a credential or reused the earlier scanned content")
	}
	if !reflect.DeepEqual(before, importScanFileState(t, home)) {
		t.Fatal("preview changed user configuration or created persistent state")
	}
	for _, sourceID := range []string{"", "../codex", currentPath, "unknown"} {
		params, _ := json.Marshal(Object{"sourceId": sourceID})
		if _, err := m.Request(t.Context(), "previewScannedImport", string(params)); err == nil {
			t.Fatalf("accepted an unknown source ID: %q", sourceID)
		}
	}
	if !reflect.DeepEqual(before, importScanFileState(t, home)) || len(m.previews) != 1 {
		t.Fatal("rejected preview changed state")
	}
}

func TestScanImportSourcesIsolatesPathFailuresAndLimits(t *testing.T) {
	m, home := importScanTestManager(t)
	if err := AtomicWrite(filepath.Join(home, "omp", "mcp.json"), []byte(`{"mcpServers":{"ok":{"command":"node"}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "buddy"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".mcp.json", filepath.Join(home, "buddy", ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "mcp.json"), 0700); err != nil {
		t.Fatal(err)
	}
	largePath := filepath.Join(home, "codex", "config.toml")
	if err := AtomicWrite(largePath, []byte(strings.Repeat(" ", maxConfigFileBytes+1))); err != nil {
		t.Fatal(err)
	}
	before := importScanFileState(t, home)
	items := scanImportItems(t, m)
	if items["omp"]["status"] != "found" {
		t.Fatal("CodeBuddy's path error prevented scanning another adapter")
	}
	for _, source := range []string{"codebuddy", "cursor", "codex"} {
		if items[source]["status"] != "unavailable" {
			t.Fatalf("%s was not rejected: %v", source, items[source])
		}
		if _, err := m.PreviewScannedImport(t.Context(), source); err == nil {
			t.Fatalf("preview accepted unreadable/non-regular/oversized %s", source)
		}
	}
	if !reflect.DeepEqual(before, importScanFileState(t, home)) {
		t.Fatal("path failures changed files")
	}
	if err := os.Truncate(largePath, maxConfigFileBytes); err != nil {
		t.Fatal(err)
	}
	if scanImportItems(t, m)["codex"]["status"] != "empty" {
		t.Fatal("exactly 10 MB of whitespace should be accepted as an empty file")
	}
	t.Setenv("PI_CONFIG_DIR", filepath.Join(home, "unknown-omp-layout"))
	if scanImportItems(t, m)["omp"]["status"] != "unavailable" {
		t.Fatal("an unsupported OMP path override was reported as an active source")
	}
}

func TestPreviewScannedImportReresolvesCurrentSymlink(t *testing.T) {
	m, home := importScanTestManager(t)
	first, second := filepath.Join(home, "first.toml"), filepath.Join(home, "second.toml")
	for path, name := range map[string]string{first: "before", second: "after"} {
		if err := AtomicWrite(path, []byte("[mcp_servers."+name+"]\ncommand = \"node\"\n")); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, "codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, path); err != nil {
		t.Fatal(err)
	}
	if scanImportItems(t, m)["codex"]["status"] != "found" {
		t.Fatal("regular configuration target behind a symlink was rejected")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, path); err != nil {
		t.Fatal(err)
	}
	before := importScanFileState(t, home)
	preview, err := m.PreviewScannedImport(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	items := array(object(preview)["items"])
	if len(items) != 1 || object(items[0])["name"] != "after" {
		t.Fatal("preview reused the scan's former symlink target")
	}
	if !reflect.DeepEqual(before, importScanFileState(t, home)) {
		t.Fatal("preview changed the source link or files")
	}
}

func TestImportBlocksOnlyGatewayConnectorStructure(t *testing.T) {
	m, _ := importScanTestManager(t)
	for _, tc := range []struct {
		name    string
		entry   Object
		blocked bool
	}{
		{"renamed-entry", gatewayClientEntry(Object{}, "/Applications/MCP Gateway.app/Contents/MacOS/mcp-gateway", "/profile", "cursor", "json"), true},
		{"mcp-gateway", Object{"command": "node", "args": []any{"server.js"}}, false},
		{"mcp-gateway", Object{"command": "/tools/mcp-gateway", "args": []any{"serve"}}, false},
		{"mcp-gateway", Object{"command": "/tools/other", "args": []any{"connect", "--client", "cursor", "--data-dir", "/profile"}}, false},
	} {
		content, _ := json.Marshal(Object{"mcpServers": Object{tc.name: tc.entry}})
		preview, err := m.PreviewImport(t.Context(), Object{"source": "Claude Code", "content": string(content)})
		if err != nil {
			t.Fatal(err)
		}
		items := array(object(preview)["items"])
		if len(items) != 1 {
			t.Fatal("missing preview entry")
		}
		item := object(items[0])
		if (stringValue(item["blockedReason"]) != "") != tc.blocked || (item["action"] == "skip") != tc.blocked {
			t.Fatalf("wrong self-connection decision for %s: %v", tc.name, item)
		}
	}
}
