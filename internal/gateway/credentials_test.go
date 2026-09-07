package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLocalTokensReuseRenewAndRejectAPIFailure(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	key, err := m.Key("admin")
	if err != nil {
		t.Fatal(err)
	}
	exists, revoked, fail, created := false, false, false, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-API-Key") != key {
			t.Error("wrong management token")
		}
		if fail {
			w.WriteHeader(503)
			_ = json.NewEncoder(w).Encode(Object{"success": false})
			return
		}
		var data any
		switch r.Method {
		case http.MethodGet:
			if !exists {
				w.WriteHeader(404)
				_ = json.NewEncoder(w).Encode(Object{"success": false})
				return
			}
			data = Object{"revoked": revoked, "expires_at": time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)}
		case http.MethodPost:
			created++
			exists = true
			revoked = false
			data = Object{"token": "controlled-client-token"}
		case http.MethodDelete:
			exists = false
		}
		_ = json.NewEncoder(w).Encode(Object{"success": true, "data": data})
	}))
	defer api.Close()
	newManager := func() *Manager {
		other, err := New(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		other.baseURL, other.adminKey = api.URL, key
		return other
	}
	// Independent managers emulate simultaneous clients sharing a profile.
	var wg sync.WaitGroup
	for range 5 {
		other := newManager()
		wg.Go(func() {
			if got, err := other.Key("admin"); err != nil || got != key {
				t.Error("management token changed", err)
			}
			if err := other.EnsureAgentToken(t.Context(), "codebuddy"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("created %d tokens for concurrent clients", created)
	}
	m = newManager()
	revoked = true
	if err := m.EnsureAgentToken(t.Context(), "codebuddy"); err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatal("revoked token not renewed")
	}
	path := filepath.Join(dir, "credentials", "client-codebuddy")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureAgentToken(t.Context(), "codebuddy"); err != nil {
		t.Fatal(err)
	}
	if created != 3 {
		t.Fatal("missing token file not repaired")
	}
	fail = true
	if err := m.EnsureAgentToken(t.Context(), "codebuddy"); err == nil {
		t.Fatal("API failure accepted")
	}
	if created != 3 {
		t.Fatal("API failure rotated token")
	}
	for name, mode := range map[string]os.FileMode{"": 0700, "admin": 0600, "client-codebuddy": 0600} {
		info, err := os.Stat(filepath.Join(dir, "credentials", name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatal("incorrect token permissions", name, err)
		}
	}
	unlock, err := m.lockCredentials(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if release, err := m.lockCredentials(ctx); err == nil {
		release()
		t.Fatal("lock ignored another process")
	}
}
