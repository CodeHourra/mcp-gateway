package acceptance_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcp-gateway/internal/gateway"

	"github.com/tidwall/jsonc"
	"github.com/zalando/go-keyring"
)

// The client and gateway both live in temporary directories. The token API is
// a controlled stub and Keychain is in memory; this is file/backup acceptance,
// not a real client's handshake or system credential-store acceptance.
func TestAgentConfigApplyAndExactRestore(t *testing.T) {
	for _, scenario := range []struct {
		name, client, variable, original string
	}{
		{"OMP JSON", "omp", "PI_CODING_AGENT_DIR", "{\n  \"customFlag\": true,\n  \"mcpServers\": {\"unrelated\":{\"command\":\"keep-this\",\"args\":[\"a\",\"b\"]},\"mcp-gateway\":{\"url\":\"https://example.test/mcp?api_key=ACCEPTANCE_PLAINTEXT\",\"disabledTools\":[\"danger\"]}}\n}\n"},
		{"CodeBuddy JSONC", "codebuddy", "CODEBUDDY_CONFIG_DIR", "{\n  // KEEP OUTER COMMENT\n  \"customFlag\": true,\n  \"mcpServers\": {\n    \"unrelated\": {\"command\":\"keep-this\",\"args\":[\"a\",\"b\"]}, // KEEP SIBLING COMMENT\n    \"mcp-gateway\": {\"url\":\"https://example.test/mcp?api_key=ACCEPTANCE_PLAINTEXT\",\"disabledTools\":[\"danger\"]},\n  },\n}\n"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			keyring.MockInit()
			clientDir, appDir := t.TempDir(), t.TempDir()
			t.Setenv(scenario.variable, clientDir)
			if scenario.client == "omp" {
				for _, key := range []string{"OMP_PROFILE", "PI_PROFILE", "PI_CONFIG_DIR"} {
					t.Setenv(key, "")
				}
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			bridge, err := json.Marshal(gateway.Object{
				"command": filepath.Join("/previous-install", filepath.Base(executable)),
				"args":    []string{"connect", "--client", scenario.client, "--data-dir", appDir},
			})
			if err != nil {
				t.Fatal(err)
			}
			// An owned bridge with stale connection fields must update in place;
			// an unrelated same-name remote service is a conflict, not an update.
			original := strings.Replace(scenario.original, `"url":`, string(bridge[1:len(bridge)-1])+`,"url":`, 1)
			path := filepath.Join(clientDir, "mcp.json")
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/tokens/") {
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(gateway.Object{"success": false, "error": "controlled absent token"})
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/tokens" {
					_ = json.NewEncoder(w).Encode(gateway.Object{"success": true, "data": gateway.Object{"token": "acceptance-memory-token"}})
					return
				}
				t.Errorf("unexpected API operation: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer api.Close()
			settings, _ := json.Marshal(gateway.Settings{Theme: "system", Mode: "progressive", ListenAddress: api.Listener.Addr().String(), LogRetentionDays: 14})
			if err := os.WriteFile(filepath.Join(appDir, "settings.json"), settings, 0600); err != nil {
				t.Fatal(err)
			}
			manager, err := gateway.New(appDir, "acceptance-unused-core")
			if err != nil {
				t.Fatal(err)
			}
			preview, err := manager.PreviewAgentConfig(t.Context(), scenario.client)
			if err != nil {
				t.Fatal(err)
			}
			view := preview.(map[string]any)
			encoded, _ := json.Marshal(view)
			if strings.Contains(string(encoded), "ACCEPTANCE_PLAINTEXT") {
				t.Fatal("agent preview exposed an existing URL credential")
			}
			result, err := manager.ApplyAgentConfig(t.Context(), view["id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]any
			if err := json.Unmarshal(jsonc.ToJSON(after), &parsed); err != nil {
				t.Fatal("generated client configuration is invalid:", err)
			}
			servers := parsed["mcpServers"].(map[string]any)
			entry := servers["mcp-gateway"].(map[string]any)
			if parsed["customFlag"] != true || servers["unrelated"].(map[string]any)["command"] != "keep-this" || entry["type"] != "stdio" || entry["url"] != nil || entry["disabledTools"].([]any)[0] != "danger" {
				t.Fatal("configuration changed unrelated values or lost the entry's tool policy")
			}
			for _, comment := range []string{"KEEP OUTER COMMENT", "KEEP SIBLING COMMENT"} {
				if strings.Contains(scenario.original, comment) && !strings.Contains(string(after), comment) {
					t.Fatal("JSONC comment was lost:", comment)
				}
			}
			backupID := result.(map[string]any)["backupId"].(string)
			backupDir := filepath.Join(appDir, "backups", backupID)
			if err := filepath.WalkDir(backupDir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := entry.Info()
				if err == nil && info.Mode().Perm()&0077 != 0 {
					t.Errorf("backup accessible outside owner: %s mode %v", path, info.Mode())
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.RestoreBackup(t.Context(), backupID); err != nil {
				t.Fatal(err)
			}
			restored, err := os.ReadFile(path)
			if err != nil || string(restored) != original {
				t.Fatal("restoration did not recover exact original bytes:", err)
			}
		})
	}
}
