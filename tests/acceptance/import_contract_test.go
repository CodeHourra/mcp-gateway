package acceptance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-gateway/internal/gateway"
)

// This exercises only import preview and rejected apply paths. It never starts
// the core, reads the user's client files, or accesses Keychain credentials.
func TestImportReferencesAndRejectedWrites(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "core.json")
	before := []byte(`{"mcpServers":[{"name":"existing","protocol":"stdio","command":"acceptance-no-exec","args":[],"working_dir":"","url":"","headers":{},"env":{"TOKEN":"${env:ACCEPTANCE_TOKEN}"},"oauth":{}}]}`)
	if err := os.WriteFile(configPath, before, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := gateway.New(dir, "acceptance-unused-core")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := manager.PreviewImport(t.Context(), gateway.Object{
		"source":  "Claude Code",
		"content": `{"mcpServers":{"incoming":{"command":"acceptance-no-exec","args":[],"env":{"TOKEN":"${ACCEPTANCE_TOKEN}"}}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	view := preview.(map[string]any)
	items := view["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one imported connection, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["status"] != "duplicate" {
		t.Fatalf("equivalent ${VAR} and ${env:VAR} references must match, got %v", item)
	}
	params := gateway.Object{"previewId": view["id"], "decisions": []any{gateway.Object{"id": item["id"], "action": "skip"}}}
	if _, err := manager.ApplyImport(t.Context(), params); err == nil {
		t.Fatal("all-skipped import reported success")
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("rejected import changed configuration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("all-skipped import created a backup or unexpected state: %v", err)
	}
	changed := []byte(strings.Replace(string(before), "existing", "externally-changed", 1))
	if err := os.WriteFile(configPath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	params["decisions"] = []any{gateway.Object{"id": item["id"], "action": "merge"}}
	if _, err := manager.ApplyImport(t.Context(), params); err == nil {
		t.Fatal("stale preview was accepted after an external configuration edit")
	}
	after, err = os.ReadFile(configPath)
	if err != nil || string(after) != string(changed) {
		t.Fatalf("stale import overwrote the external edit: %v", err)
	}
	encoded, err := json.Marshal(view)
	if err != nil || strings.Contains(string(encoded), "ACCEPTANCE_TOKEN") {
		t.Fatalf("preview exposed an internal credential reference: %s (%v)", encoded, err)
	}
}
