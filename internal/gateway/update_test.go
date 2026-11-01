package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseServer mimics github.com: /latest redirects to /tag/<tag>, and only the
// listed assets are downloadable. body is the installer content; sumBody
// overrides the .sha256 file content when set.
func releaseServer(t *testing.T, tag string, assets []string, body, sumBody string) Updater {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/owner/repo/releases/tag/"+tag, http.StatusFound)
	})
	for _, name := range assets {
		name := name
		mux.HandleFunc("/dl/"+tag+"/"+name, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
		mux.HandleFunc("/dl/"+tag+"/"+name+".sha256", func(w http.ResponseWriter, _ *http.Request) {
			if sumBody != "" {
				fmt.Fprint(w, sumBody)
				return
			}
			sum := sha256.Sum256([]byte(body))
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Updater{LatestURL: srv.URL + "/latest", DownloadBase: srv.URL + "/dl", Suffix: "arm64.dmg", Client: srv.Client()}
}

func TestCheckComparesVersionsNumerically(t *testing.T) {
	asset := "MCP-Gateway-0.1.10-arm64.dmg"
	u := releaseServer(t, "v0.1.10", []string{asset}, "x", "")
	// Lexical comparison would call 0.1.10 older than 0.1.9.
	info, err := u.Check(context.Background(), "0.1.9")
	if err != nil || !info.Available || info.Asset != asset {
		t.Fatalf("0.1.9 -> 0.1.10 should be available: %+v, %v", info, err)
	}
	for _, current := range []string{"0.1.10", "0.2.0"} {
		info, err = u.Check(context.Background(), current)
		if err != nil || info.Available {
			t.Fatalf("%s must not be offered 0.1.10: %+v, %v", current, info, err)
		}
	}
}

func TestCheckRequiresPlatformAssetAndValidTag(t *testing.T) {
	u := releaseServer(t, "v0.2.0", []string{"MCP-Gateway-0.2.0-windows-amd64.zip"}, "x", "")
	info, err := u.Check(context.Background(), "0.1.0")
	if err != nil || info.Available || info.Latest != "0.2.0" {
		t.Fatalf("release without this platform's asset must not be downloadable: %+v, %v", info, err)
	}
	u = releaseServer(t, "nightly", nil, "x", "")
	if _, err := u.Check(context.Background(), "0.1.0"); err == nil {
		t.Fatal("non-semver tag must be rejected")
	}
}

func TestCheckReportsRepositoryWithoutReleases(t *testing.T) {
	// github.com redirects /releases/latest to /releases when nothing is published.
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/owner/repo/releases", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	u := Updater{LatestURL: srv.URL + "/latest", DownloadBase: srv.URL + "/dl", Suffix: "arm64.dmg", Client: srv.Client()}
	if info, err := u.Check(context.Background(), "0.1.0"); err == nil || info.Available {
		t.Fatalf("repository without releases must error, got %+v, %v", info, err)
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	asset := "MCP-Gateway-0.2.0-arm64.dmg"
	dir := filepath.Join(t.TempDir(), "updates")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "MCP-Gateway-0.1.5-arm64.dmg")
	if err := os.WriteFile(stale, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	bad := releaseServer(t, "v0.2.0", []string{asset}, "tampered", strings.Repeat("0", 64)+"  "+asset)
	if _, _, err := bad.Download(context.Background(), "0.1.0", dir); err == nil {
		t.Fatal("checksum mismatch must fail")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == asset || strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("rejected download left %s behind", e.Name())
		}
	}

	good := releaseServer(t, "v0.2.0", []string{asset}, "installer-bytes", "")
	path, info, err := good.Download(context.Background(), "0.1.0", dir)
	if err != nil || info.Latest != "0.2.0" {
		t.Fatalf("download failed: %+v, %v", info, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "installer-bytes" || filepath.Base(path) != asset {
		t.Fatalf("unexpected file %s: %q", path, data)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("older installers must be removed")
	}
}

func TestDownloadRefusesWhenNothingNewer(t *testing.T) {
	u := releaseServer(t, "v0.1.0", []string{"MCP-Gateway-0.1.0-arm64.dmg"}, "x", "")
	if _, _, err := u.Download(context.Background(), "0.1.0", t.TempDir()); err == nil {
		t.Fatal("no newer release: download must be refused")
	}
}
