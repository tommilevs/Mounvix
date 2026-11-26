package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const protectedBootstrapSecret = "test-only-bootstrap-secret-do-not-expose"

func newDataDirProtectionTestApp(t *testing.T) (*App, string, string) {
	t.Helper()
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")
	cfg := DefaultConfig()
	cfg.DataDir = dataDir
	cfg.Roots = []Root{{Name: "home", Path: base}}
	seedTestAdmin(t, dataDir)
	app, err := New(cfg, filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	if err := os.WriteFile(filepath.Join(dataDir, adminBootstrapPasswordFile), []byte(protectedBootstrapSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	return app, base, dataDir
}

func TestAppDataDirectoryIsNotExposedThroughFileRoutes(t *testing.T) {
	app, _, dataDir := newDataDirProtectionTestApp(t)
	token := app.store.CreateSession("admin")
	request := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/file?root=home&path=data/"+adminBootstrapPasswordFile, nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("file route status=%d, want 404; body=%q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), protectedBootstrapSecret) {
		t.Fatal("file route exposed bootstrap password")
	}

	if _, err := app.describeImage(context.Background(), "home", filepath.Join("data", adminBootstrapPasswordFile), "describe"); err == nil {
		t.Fatal("AI image path unexpectedly allowed app data")
	}

	alias := filepath.Join(filepath.Dir(dataDir), "data-alias")
	if err := os.Symlink(dataDir, alias); err != nil {
		t.Skipf("symlink unavailable in test environment: %v", err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/file?root=home&path=data-alias/"+adminBootstrapPasswordFile, nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	recorder = httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("symlink file route status=%d, want 404; body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestWebDAVDoesNotExposeAppDataDirectory(t *testing.T) {
	app, _, dataDir := newDataDirProtectionTestApp(t)
	request := httptest.NewRequest("PROPFIND", "http://example.test/dav/home/", nil)
	request.Header.Set("Depth", "1")
	request.SetBasicAuth("admin", testAdminPassword)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != 207 {
		t.Fatalf("WebDAV listing status=%d, want 207; body=%q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "data/") || strings.Contains(recorder.Body.String(), adminBootstrapPasswordFile) {
		t.Fatalf("WebDAV listing exposed app data: %s", recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://example.test/dav/home/data/"+adminBootstrapPasswordFile, nil)
	request.SetBasicAuth("admin", testAdminPassword)
	recorder = httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("WebDAV secret read status=%d, want 404; body=%q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), protectedBootstrapSecret) {
		t.Fatal("WebDAV exposed bootstrap password")
	}

	request = httptest.NewRequest(http.MethodDelete, "http://example.test/dav/home/data/", nil)
	request.SetBasicAuth("admin", testAdminPassword)
	recorder = httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("WebDAV delete of app data status=%d, want 404; body=%q", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, adminBootstrapPasswordFile)); err != nil {
		t.Fatalf("WebDAV delete removed bootstrap file: %v", err)
	}
}

func TestPublicSharesCannotExposeAppDataDirectory(t *testing.T) {
	app, _, _ := newDataDirProtectionTestApp(t)
	if _, err := app.createUserShare("home", filepath.Join("data", adminBootstrapPasswordFile), "admin", time.Hour, "", false); err == nil {
		t.Fatal("share creation accepted an app-data path")
	}
	share, err := app.shares.Create("home", filepath.Join("data", adminBootstrapPasswordFile), "admin", time.Hour, "", false)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://example.test/s/"+share.Token, nil)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("public secret share status=%d, want 404; body=%q", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), protectedBootstrapSecret) {
		t.Fatal("public share exposed bootstrap password")
	}

	directoryShare, err := app.shares.Create("home", "", "admin", time.Hour, "", false)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://example.test/s/"+directoryShare.Token+"?path=data", nil)
	recorder = httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("public directory share app-data status=%d, want 404; body=%q", recorder.Code, recorder.Body.String())
	}
}
