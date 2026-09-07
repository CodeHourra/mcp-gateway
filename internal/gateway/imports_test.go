package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportIdentityAndReferenceSafety(t *testing.T) {
	for _, source := range []string{"OMP", "Claude Code", "CodeBuddy", "自动识别"} {
		value, err := normalizeExpression("Bearer ${TEST_TOKEN}", source)
		if err != nil || value != "Bearer ${env:TEST_TOKEN}" {
			t.Fatalf("%s normalized to %q: %v", source, value, err)
		}
	}
	if _, err := normalizeExpression("${TEST_TOKEN:-fallback}", "OMP"); err == nil {
		t.Fatal("silently accepted an unsupported default expression")
	}
	if _, err := normalizeExpression("!read-secret", "OMP"); err == nil {
		t.Fatal("accepted command-based credential lookup")
	}
	stored := []string{}
	secured, err := secureReferenceParts("literal-secret-${env:SUFFIX}-tail", func(value string) (string, error) {
		stored = append(stored, value)
		return fmt.Sprintf("${keyring:part-%d}", len(stored)), nil
	})
	if err != nil || secured != "${keyring:part-1}${env:SUFFIX}${keyring:part-2}" || !reflect.DeepEqual(stored, []string{"literal-secret-", "-tail"}) {
		t.Fatalf("composition did not secure literal parts: %q %v %v", secured, stored, err)
	}
	base := Object{"protocol": "stdio", "command": "node", "args": []any{"first", "second"}, "working_dir": "/one", "env": Object{"TOKEN": "${env:ACCOUNT_A}"}, "headers": Object{}}
	original, err := connectionIdentity(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, update := range []Object{{"args": []any{"second", "first"}}, {"working_dir": "/two"}, {"env": Object{"TOKEN": "${env:ACCOUNT_B}"}}, {"protocol": "sse"}} {
		encoded, _ := json.Marshal(base)
		modified := Object{}
		_ = json.Unmarshal(encoded, &modified)
		for key, value := range update {
			modified[key] = value
		}
		identity, err := connectionIdentity(modified)
		if err != nil || reflect.DeepEqual(identity, original) {
			t.Fatalf("identity lost a connection difference: %v %v", update, err)
		}
	}
	for _, content := range []string{`{"mcpServers":{"x":{"url":"https://example.com/mcp?api_key=plain-secret"}}}`, `{"mcpServers":{"x":{"command":"node","args":["--token","plain-secret"]}}}`} {
		entries, err := parseImport([]byte(content), "Claude Code", "")
		if err != nil || len(entries) != 1 || entries[0].BlockedReason == "" {
			t.Fatalf("credential-bearing configuration was not blocked: %v %v", entries, err)
		}
	}
}

func TestImportMergeFollowsPreviewItemAndRejectsStaleOrEmptyApply(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "unused-core")
	if err != nil {
		t.Fatal(err)
	}
	corePath := filepath.Join(dir, "core.json")
	cfg := Object{"mcpServers": []any{Object{"name": "svc", "protocol": "stdio", "command": "A", "args": []any{}, "enabled": true}}, "unknown": Object{"keep": true}}
	if err := writeJSON(corePath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, "services.json"), map[string]ServiceMeta{"svc": {Sources: []string{"original"}}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/config" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		calls++
		patch := Object{}
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			t.Error(err)
			return
		}
		cfg["mcpServers"] = patch["mcpServers"]
		if err := writeJSON(corePath, cfg); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer server.Close()
	m.baseURL = server.URL
	preview, err := m.PreviewImport(t.Context(), Object{"source": "Claude Code", "content": `{"mcpServers":{"svc":{"command":"B"},"zzcopy":{"command":"B"}}}`})
	if err != nil {
		t.Fatal(err)
	}
	view := object(preview)
	decisions := []any{}
	for _, value := range array(view["items"]) {
		item := object(value)
		decisions = append(decisions, Object{"id": item["id"], "action": item["action"]})
	}
	result, err := m.ApplyImport(t.Context(), Object{"previewId": view["id"], "decisions": decisions})
	if err != nil {
		t.Fatal(err)
	}
	if object(result)["added"] != 1 || object(result)["merged"] != 1 || calls != 1 {
		t.Fatalf("unexpected import counts: %#v calls=%d", result, calls)
	}
	meta, err := m.metadata()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(meta["svc"].Sources, []string{"original"}) {
		t.Fatalf("source metadata was merged into original A: %#v", meta)
	}
	if len(meta["svc-2"].Sources) != 2 || !strings.Contains(strings.Join(meta["svc-2"].Sources, " "), "zzcopy") {
		t.Fatalf("B's final identity lost duplicate source: %#v", meta)
	}
	preview, err = m.PreviewImport(t.Context(), Object{"source": "Claude Code", "content": `{"mcpServers":{"new":{"command":"C"}}}`})
	if err != nil {
		t.Fatal(err)
	}
	view = object(preview)
	item := object(array(view["items"])[0])
	before, _ := os.ReadFile(corePath)
	if _, err := m.ApplyImport(t.Context(), Object{"previewId": view["id"], "decisions": []any{Object{"id": item["id"], "action": "skip"}}}); err == nil {
		t.Fatal("all-skip import reported success")
	}
	after, _ := os.ReadFile(corePath)
	if !bytes.Equal(before, after) || calls != 1 {
		t.Fatal("all-skip import changed state")
	}
	if err := writeJSON(filepath.Join(dir, "services.json"), map[string]ServiceMeta{"changed": {Sources: []string{"external"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApplyImport(t.Context(), Object{"previewId": view["id"], "decisions": []any{Object{"id": item["id"], "action": "add"}}}); err == nil {
		t.Fatal("stale preview overwrote newer configuration")
	}
}

func TestBackupRestoresBytesAndPreferencesWhenSettingsFileWasAbsent(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "unused-core")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"mcpServers\": [], \"listen\": \"127.0.0.1:17840\", \"keep\": true\n}\n")
	path := filepath.Join(dir, "core.json")
	if err := AtomicWrite(path, original); err != nil {
		t.Fatal(err)
	}
	settings := m.Preferences()
	id, err := m.backup("initial")
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.settings.ListenAddress = "127.0.0.1:17841"
	m.settings.Mode = "aggregate"
	changed := m.settings
	m.mu.Unlock()
	if err := writeJSON(filepath.Join(dir, "settings.json"), changed); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, Object{"changed": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RestoreBackup(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(original, actual) {
		t.Fatal("original config bytes were not restored")
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("previously missing settings file was not restored to missing")
	}
	if m.Preferences() != settings || m.Address() != "http://127.0.0.1:17840/mcp/call" {
		t.Fatalf("restored preferences mismatch: %#v", m.Preferences())
	}
	manifestPath := filepath.Join(dir, "backups", id, "0.data")
	if err := os.WriteFile(manifestPath, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RestoreBackup(t.Context(), id); err == nil {
		t.Fatal("corrupt backup restored")
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(actual, unchanged) {
		t.Fatal("corrupt backup changed current data")
	}
}
