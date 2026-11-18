package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedServesPackagesAndBlocksTraversal(t *testing.T) {
	a, _ := newTestApp(t)
	feedDir := filepath.Join(a.cfg.DataDir, "feed")
	if err := os.MkdirAll(feedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkgs := "Package: mounvix\nVersion: 9.9.9\n\n"
	if err := os.WriteFile(filepath.Join(feedDir, "Packages"), []byte(pkgs), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/feed/Packages")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != pkgs {
		t.Fatalf("feed: %d %q", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && ct == "text/html" {
		t.Fatalf("unexpected html content type")
	}

	resp, err = http.Get(srv.URL + "/feed/%2e%2e/%2e%2e/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == 200 && strings.Contains(string(body), "root:") {
		t.Fatalf("traversal leaked /etc/passwd: %q", string(body))
	}
}
