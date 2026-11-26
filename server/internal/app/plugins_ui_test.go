package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

func newPluginUIFixture(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	pluginRoot := filepath.Join(base, "installed")
	pluginDir := filepath.Join(pluginRoot, "demo")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "bin", "demo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "pages", "home.html"), []byte(`<h1>Safe page</h1><script>document.body.textContent='executed'</script>`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo plugin","version":"1.0.0","api":1,"author":"test publisher","description":"fixture","executable":"bin/demo","runtime_methods":["plugin.renderPage"],"pages":[{"id":"home","name":"Home","path":"pages/home.html"}]}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(base, "data")
	cfg.PluginDir = pluginRoot
	cfg.PublicBasePath = "/mounvix"
	a, err := New(cfg, filepath.Join(base, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("close fixture app: %v", err)
		}
	})
	if err := a.setPluginSourceEnabled(nil, "admin", "demo", plugins.SourcePackage, true); err != nil {
		t.Fatal(err)
	}
	return a, a.store.CreateSession("admin")
}

func TestPluginAndNotificationsPagesShowSandboxedAdminInterface(t *testing.T) {
	a, admin := newPluginUIFixture(t)
	pluginPage := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/plugins/demo/pages/home", nil)
	pluginPage.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	pluginPageResponse := httptest.NewRecorder()
	a.ServeHTTP(pluginPageResponse, pluginPage)
	if pluginPageResponse.Code != http.StatusOK {
		t.Fatalf("plugin page status=%d body=%s", pluginPageResponse.Code, pluginPageResponse.Body.String())
	}
	for _, directive := range []string{"default-src 'none'", "script-src 'none'", "connect-src 'none'", "object-src 'none'", "form-action 'none'", "frame-ancestors 'self'"} {
		if !strings.Contains(pluginPageResponse.Header().Get("Content-Security-Policy"), directive) {
			t.Errorf("plugin page CSP lacks %q: %q", directive, pluginPageResponse.Header().Get("Content-Security-Policy"))
		}
	}
	if pluginPageResponse.Header().Get("Cache-Control") != "no-store" || !strings.Contains(pluginPageResponse.Body.String(), "Safe page") {
		t.Fatalf("plugin page headers/body = %v / %s", pluginPageResponse.Header(), pluginPageResponse.Body.String())
	}
	for _, unsafePath := range []string{"/plugins/demo/pages/../plugin.json", "/plugins/../etc/pages/passwd"} {
		escapeRequest := httptest.NewRequest(http.MethodGet, "http://example.test/plugins/demo/pages/home", nil)
		escapeRequest.URL.Path = unsafePath
		escapeRequest.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		escapeResponse := httptest.NewRecorder()
		a.handlePluginPage(escapeResponse, escapeRequest)
		if escapeResponse.Code != http.StatusNotFound {
			t.Errorf("unsafe page path %q status=%d, want 404", unsafePath, escapeResponse.Code)
		}
	}
	outsidePage := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outsidePage, []byte("must not escape"), 0o600); err != nil {
		t.Fatal(err)
	}
	installedPage := filepath.Join(a.configSnapshot().PluginDir, "demo", "pages", "home.html")
	if err := os.Remove(installedPage); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePage, installedPage); err != nil {
		t.Fatal(err)
	}
	unsafeRequest := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/plugins/demo/pages/home", nil)
	unsafeRequest.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	unsafeResponse := httptest.NewRecorder()
	a.ServeHTTP(unsafeResponse, unsafeRequest)
	if unsafeResponse.Code != http.StatusNotFound || strings.Contains(unsafeResponse.Body.String(), "must not escape") {
		t.Fatalf("symlinked page escaped package root: status=%d body=%s", unsafeResponse.Code, unsafeResponse.Body.String())
	}

	pluginsPage := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/plugins", nil)
	pluginsPage.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	pluginsPageResponse := httptest.NewRecorder()
	a.ServeHTTP(pluginsPageResponse, pluginsPage)
	if pluginsPageResponse.Code != http.StatusOK || pluginsPageResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("plugins page status=%d headers=%v", pluginsPageResponse.Code, pluginsPageResponse.Header())
	}
	pluginsHTML := pluginsPageResponse.Body.String()
	if !strings.Contains(pluginsHTML, "полномочиями процесса Mounvix") {
		t.Fatalf("plugins UI lacks the package privilege warning: %s", pluginsHTML)
	}
	if !strings.Contains(pluginsHTML, `<iframe`) || !strings.Contains(pluginsHTML, `sandbox`) || strings.Contains(pluginsHTML, `allow-same-origin`) || strings.Contains(pluginsHTML, `allow-scripts`) {
		t.Fatalf("plugin pages are not rendered in a restrictive sandbox: %s", pluginsHTML)
	}

	notificationsPage := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/notifications", nil)
	notificationsPage.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	notificationsResponse := httptest.NewRecorder()
	a.ServeHTTP(notificationsResponse, notificationsPage)
	if notificationsResponse.Code != http.StatusOK || notificationsResponse.Header().Get("Cache-Control") != "no-store" || !strings.Contains(notificationsResponse.Body.String(), "notifications.js") || !strings.Contains(notificationsResponse.Body.String(), "data-notifications") {
		t.Fatalf("notifications page status=%d body=%s", notificationsResponse.Code, notificationsResponse.Body.String())
	}

	if err := a.store.AddUserHash("reader", "test-hash", "user"); err != nil {
		t.Fatal(err)
	}
	reader := a.store.CreateSession("reader")
	for _, path := range []string{"/mounvix/plugins", "/mounvix/notifications", "/mounvix/plugins/demo/pages/home"} {
		req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: reader})
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("non-admin GET %s status=%d, want 403", path, rec.Code)
		}
	}
}

func TestPluginsPageDisablesRcloneWhenAllowedRootIsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a small POSIX rclone stub")
	}
	a, admin := newPluginUIFixture(t)
	binDir := t.TempDir()
	rcloneStub := "#!/bin/sh\ncase \"$1\" in\nversion) echo 'rclone v1.2.3' ;;\nlistremotes) echo 'backup:' ;;\n*) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, "rclone"), []byte(rcloneStub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/plugins", nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Разрешённый корень источника rclone не настроен") || strings.Contains(response.Body.String(), `id="rclone-sync-form"`) {
		t.Fatalf("rclone source-root warning/form state status=%d body=%s", response.Code, response.Body.String())
	}
	allowedRoot := filepath.Join(t.TempDir(), "rclone-source")
	a.cfgMu.Lock()
	a.cfg.RcloneSourceRoot = allowedRoot
	a.cfgMu.Unlock()
	configured := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/plugins", nil)
	configured.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	configuredResponse := httptest.NewRecorder()
	a.ServeHTTP(configuredResponse, configured)
	if configuredResponse.Code != http.StatusOK || !strings.Contains(configuredResponse.Body.String(), `id="rclone-sync-form"`) || strings.Contains(configuredResponse.Body.String(), allowedRoot) {
		t.Fatalf("configured rclone source-root form status=%d body=%s", configuredResponse.Code, configuredResponse.Body.String())
	}
}

func TestLegacyPluginsAPIIsNoStoreAndSameOrigin(t *testing.T) {
	a, admin := newPluginUIFixture(t)
	get := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/api/v1/plugins", nil)
	get.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	getResponse := httptest.NewRecorder()
	a.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || getResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("legacy plugins GET status=%d headers=%v body=%s", getResponse.Code, getResponse.Header(), getResponse.Body.String())
	}
	legacyBody := getResponse.Body.String()
	for _, privateField := range []string{`"command"`, `"manifest"`, a.configSnapshot().PluginDir} {
		if strings.Contains(legacyBody, privateField) {
			t.Fatalf("legacy plugins GET leaks %q: %s", privateField, legacyBody)
		}
	}

	post := func(origin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/api/v1/plugins", bytes.NewBufferString(`{"action":"rclone_sync","source_path":"daily","remote_name":"backup","destination_path":"archive","confirmed":true}`))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "legacy-rclone-key")
		if origin {
			req.Header.Set("Origin", "http://example.test")
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	csrf := post(false)
	if csrf.Code != http.StatusForbidden || csrf.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cross-origin legacy plugins POST status=%d headers=%v body=%s", csrf.Code, csrf.Header(), csrf.Body.String())
	}
	invalid := post(true)
	if invalid.Code != http.StatusBadRequest || invalid.Header().Get("Cache-Control") != "no-store" || !strings.Contains(invalid.Body.String(), `"code":"invalid_request"`) || strings.Contains(invalid.Body.String(), a.configSnapshot().DataDir) {
		t.Fatalf("safe legacy rclone error status=%d headers=%v body=%s", invalid.Code, invalid.Header(), invalid.Body.String())
	}
	large := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/api/v1/plugins", strings.NewReader(strings.Repeat(" ", maxPluginAPIRequestBytes+1)))
	large.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	large.Header.Set("Origin", "http://example.test")
	large.Header.Set("Content-Type", "application/json")
	largeResponse := httptest.NewRecorder()
	a.ServeHTTP(largeResponse, large)
	if largeResponse.Code != http.StatusRequestEntityTooLarge || largeResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("oversized legacy POST status=%d headers=%v body=%s", largeResponse.Code, largeResponse.Header(), largeResponse.Body.String())
	}
}

func TestPluginSourcePatchPersistsSourceSpecificState(t *testing.T) {
	a, admin := newPluginUIFixture(t)
	request := httptest.NewRequest(http.MethodPatch, "http://example.test/mounvix/api/v1/plugins/demo", bytes.NewBufferString(`{"source":"package","enabled":false}`))
	request.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	request.Header.Set("Origin", "http://example.test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("source PATCH status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	packageSource, found := a.pluginReg.LookupSource("demo", plugins.SourcePackage)
	if !found || packageSource.Enabled {
		t.Fatalf("package source state after PATCH = %#v, found=%v", packageSource, found)
	}
	reloaded, err := plugins.NewRegistry(a.configSnapshot().DataDir, a.configSnapshot().PluginDir, version.Version, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	persisted, found := reloaded.LookupSource("demo", plugins.SourcePackage)
	if !found || persisted.Enabled {
		t.Fatalf("persisted package source state = %#v, found=%v", persisted, found)
	}

	invalid := httptest.NewRequest(http.MethodPatch, "http://example.test/mounvix/api/v1/plugins/demo", bytes.NewBufferString(`{"source":"unknown","enabled":true}`))
	invalid.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	invalid.Header.Set("Origin", "http://example.test")
	invalid.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	a.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || invalidResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid source PATCH status=%d headers=%v body=%s", invalidResponse.Code, invalidResponse.Header(), invalidResponse.Body.String())
	}
}
