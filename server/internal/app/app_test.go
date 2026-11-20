package app

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
)

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	share := filepath.Join(base, "share")
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(base, "data")
	cfg.Roots = []Root{{Name: "share", Path: share}}
	a, err := New(cfg, filepath.Join(cfg.DataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return a, share
}

func TestSeedAdminLogin(t *testing.T) {
	a, _ := newTestApp(t)
	if _, ok := a.store.Verify("admin", "1234567890"); !ok {
		t.Fatal("seeded admin login failed")
	}
	if _, ok := a.store.Verify("admin", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
}

func TestPanelLoginFlow(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}

	resp, err := c.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Mounvix") {
		t.Fatalf("login page: status=%d", resp.StatusCode)
	}

	resp, err = c.PostForm(srv.URL+"/login", url.Values{"username": {"admin"}, "password": {"1234567890"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("after login: status=%d (redirect not followed?)", resp.StatusCode)
	}

	resp, err = c.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Обзор") {
		t.Fatalf("dashboard: status=%d body=%q", resp.StatusCode, string(body)[:min(80, len(body))])
	}
}

func TestPanelRequiresAuth(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	c := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(srv.URL + "/users")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("unauthenticated /users: status=%d loc=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestWebDAV(t *testing.T) {
	a, share := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	do := func(method, path, user, pass string, body io.Reader) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, body)
		if user != "" {
			req.SetBasicAuth(user, pass)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := do("PROPFIND", "/dav/share/", "", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("PROPFIND without auth: %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	resp = do("PROPFIND", "/dav/share/", "admin", "1234567890", nil)
	if resp.StatusCode != 207 {
		t.Fatalf("PROPFIND with auth: %d, want 207", resp.StatusCode)
	}
	resp.Body.Close()

	resp = do("PUT", "/dav/share/hello.txt", "admin", "1234567890", strings.NewReader("hello"))
	if resp.StatusCode != 201 && resp.StatusCode != 204 {
		t.Fatalf("PUT: %d", resp.StatusCode)
	}
	resp.Body.Close()
	b, err := os.ReadFile(filepath.Join(share, "hello.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("file content: %q err=%v", string(b), err)
	}

	if err := a.store.AddUser("reader", "reader-pass", "user"); err != nil {
		t.Fatal(err)
	}
	a.store.SetACL("reader", map[string]string{"share": "r"})

	resp = do("GET", "/dav/share/hello.txt", "reader", "reader-pass", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("reader GET: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = do("PUT", "/dav/share/x.txt", "reader", "reader-pass", strings.NewReader("nope"))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only PUT: %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSettingsLangSwitch(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if resp, err := c.PostForm(srv.URL+"/login", url.Values{"username": {"admin"}, "password": {"1234567890"}}); err == nil {
		resp.Body.Close()
	}
	resp, err := c.PostForm(srv.URL+"/settings", url.Values{"action": {"lang"}, "lang": {"en"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = c.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Overview") {
		t.Fatalf("EN dashboard missing Overview: %q", string(body)[:min(120, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestNewLoadsPackagesFromConfiguredPluginDir(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	pluginRoot := filepath.Join(base, "system-plugins")
	pkgDir := filepath.Join(pluginRoot, "demo")
	if err := os.MkdirAll(filepath.Join(pkgDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "bin", "demo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"test plugin","executable":"bin/demo"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dataDir
	cfg.PluginDir = pluginRoot
	a, err := New(cfg, filepath.Join(base, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.pluginReg.LookupSource("demo", plugins.SourcePackage); !ok {
		t.Fatal("configured package plugin root was not loaded")
	}
}

func TestNewRejectsUnsafePublicConfig(t *testing.T) {
	tests := []struct{ name, basePath, scheme string }{
		{"base path", "/public/../admin", ""},
		{"scheme", "", "javascript"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DataDir = filepath.Join(t.TempDir(), "data")
			cfg.PublicBasePath = tt.basePath
			cfg.PublicScheme = tt.scheme
			if _, err := New(cfg, filepath.Join(cfg.DataDir, "config.json")); err == nil {
				t.Fatal("unsafe public configuration accepted")
			}
		})
	}
}
