package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"golang.org/x/crypto/bcrypt"
)

const testAdminPassword = "mounvix-test-admin-password"

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	share := filepath.Join(base, "share")
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(base, "data")
	seedTestAdmin(t, cfg.DataDir)
	cfg.Roots = []Root{{Name: "share", Path: share}}
	a, err := New(cfg, filepath.Join(cfg.DataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return a, share
}

func seedTestAdmin(t *testing.T, dataDir string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(testAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	users, err := json.Marshal([]*User{{Name: "admin", PassHash: string(hash), Role: "admin"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "users.json"), users, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuditedJobOperationMapsPluginRcloneAndPackageLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		record     plugins.JobRecord
		wantAction string
		wantPlugin string
	}{
		{name: "plugin run", record: plugins.JobRecord{Kind: "plugin", PluginID: "demo"}, wantAction: "run", wantPlugin: "demo"},
		{name: "rclone run", record: plugins.JobRecord{Kind: "rclone"}, wantAction: "run", wantPlugin: "rclone"},
		{name: "package install", record: plugins.JobRecord{Kind: "package", JobID: "demo", Operation: "install"}, wantAction: "install", wantPlugin: "demo"},
		{name: "package update", record: plugins.JobRecord{Kind: "package", PluginID: "demo", Operation: "update"}, wantAction: "update", wantPlugin: "demo"},
		{name: "unsupported package operation", record: plugins.JobRecord{Kind: "package", PluginID: "demo", Operation: "inspect"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotAction, gotPlugin := auditedJobOperation(tc.record)
			if gotAction != tc.wantAction || gotPlugin != tc.wantPlugin {
				t.Fatalf("auditedJobOperation() = %q, %q; want %q, %q", gotAction, gotPlugin, tc.wantAction, tc.wantPlugin)
			}
		})
	}
}

func TestOutcomeReconciliationPublishesDurablePluginRun(t *testing.T) {
	dir := t.TempDir()
	jobs, err := plugins.NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := jobs.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.MarkRunning(context.Background(), record.RunID); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Finish(context.Background(), record.RunID, "success", "done"); err != nil {
		t.Fatal(err)
	}
	notifications, err := plugins.NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{jobStore: jobs, notifications: notifications}
	if err := app.reconcileJobOutcomes(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := notifications.ListPage("admin", nil, nil, 50)
	if err != nil || len(page.Items) != 1 || page.Items[0].RunID != record.RunID {
		t.Fatalf("reconciled page=%+v err=%v", page, err)
	}
}

func TestOutcomeReconciliationExcludesOriginPluginAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	pluginRoot := filepath.Join(dir, "system-plugins")
	for _, id := range []string{"producer", "receiver"} {
		pluginDir := filepath.Join(pluginRoot, id)
		if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pluginDir, "bin", id), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := `{"id":"` + id + `","name":"` + id + `","version":"1.0.0","api":1,"author":"test","description":"test","executable":"bin/` + id + `","hooks":["plugin.job.succeeded"]}`
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := plugins.NewRegistry(dir, pluginRoot, "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"producer", "receiver"} {
		if err := registry.SetEnabledAs("admin", id, true); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := plugins.NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	record, err := jobs.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "producer", JobID: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.MarkRunning(context.Background(), record.RunID); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Finish(context.Background(), record.RunID, "success", "done"); err != nil {
		t.Fatal(err)
	}
	hooks, err := plugins.NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	notifications, err := plugins.NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{jobStore: jobs, pluginReg: registry, hookStore: hooks, notifications: notifications}
	for range 2 {
		if err := app.reconcileJobOutcomes(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	producerPending, err := hooks.Pending("producer")
	if err != nil {
		t.Fatal(err)
	}
	receiverPending, err := hooks.Pending("receiver")
	if err != nil {
		t.Fatal(err)
	}
	if len(producerPending) != 0 || len(receiverPending) != 1 || receiverPending[0].Event.EventID == "" {
		t.Fatalf("hook deliveries producer=%+v receiver=%+v", producerPending, receiverPending)
	}
	page, err := notifications.ListPage("admin", nil, nil, 50)
	if err != nil || len(page.Items) != 1 || page.Items[0].RunID != record.RunID {
		t.Fatalf("reconciled notifications=%+v err=%v", page, err)
	}
	auditData, err := os.ReadFile(filepath.Join(dir, "plugin-audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Items []plugins.AuditEvent `json:"items"`
	}
	if err := json.Unmarshal(auditData, &audit); err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, event := range audit.Items {
		if event.Action == "run" && event.Phase == "result" && event.RunID == record.RunID {
			results++
		}
	}
	if results != 1 {
		t.Fatalf("job outcome audit results=%d, events=%+v", results, audit.Items)
	}
}

func TestAppUsesTestAdminFixture(t *testing.T) {
	a, _ := newTestApp(t)
	if _, ok := a.store.Verify("admin", testAdminPassword); !ok {
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

	resp, err = c.PostForm(srv.URL+"/login", url.Values{"username": {"admin"}, "password": {testAdminPassword}})
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
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?next=%2Fusers" {
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

	resp = do("PROPFIND", "/dav/share/", "admin", testAdminPassword, nil)
	if resp.StatusCode != 207 {
		t.Fatalf("PROPFIND with auth: %d, want 207", resp.StatusCode)
	}
	resp.Body.Close()

	resp = do("PUT", "/dav/share/hello.txt", "admin", testAdminPassword, strings.NewReader("hello"))
	if resp.StatusCode != 201 && resp.StatusCode != 204 {
		t.Fatalf("PUT: %d", resp.StatusCode)
	}
	resp.Body.Close()
	b, err := os.ReadFile(filepath.Join(share, "hello.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("file content: %q err=%v", string(b), err)
	}
	hookJournal, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "plugin-hook-events.json"))
	if err != nil || strings.Count(string(hookJournal), `"kind": "upload.done"`) != 1 {
		t.Fatalf("successful PUT hook journal=%s err=%v", hookJournal, err)
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
	hookJournal, err = os.ReadFile(filepath.Join(a.cfg.DataDir, "plugin-hook-events.json"))
	if err != nil || strings.Count(string(hookJournal), `"kind": "upload.done"`) != 1 {
		t.Fatalf("failed PUT published hook: journal=%s err=%v", hookJournal, err)
	}
}

func TestSettingsLangSwitch(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if resp, err := c.PostForm(srv.URL+"/login", url.Values{"username": {"admin"}, "password": {testAdminPassword}}); err == nil {
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
	manifest := `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"test plugin","executable":"bin/demo","runtime_methods":["plugin.runJob"],"capabilities":{"schedule":true},"jobs":[{"id":"sync","schedule":"0 * * * *","timeout_seconds":300}]}`
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
	if err := a.setPluginEnabled("admin", "demo", true); err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 11, 20, 14, 0, 0, 0, time.UTC)
	if err := a.runPluginSchedulerOnce(context.Background(), instant); err != nil {
		t.Fatal(err)
	}
	if err := a.runPluginSchedulerOnce(context.Background(), instant); err != nil {
		t.Fatal(err)
	}
	page, err := a.jobStore.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].JobID != "sync" {
		t.Fatalf("scheduled jobs = %#v", page.Items)
	}
	if err := a.setPluginEnabled("admin", "demo", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.jobManager.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "new"}); !errors.Is(err, plugins.ErrPluginJobsDisabled) {
		t.Fatalf("enqueue for disabled plugin error = %v", err)
	}
	auditData, err := os.ReadFile(filepath.Join(dataDir, "plugin-audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Items []plugins.AuditEvent `json:"items"`
	}
	if err := json.Unmarshal(auditData, &audit); err != nil {
		t.Fatal(err)
	}
	var enabledIntent, enabledResult, disabledIntent, disabledResult, runIntent bool
	for _, event := range audit.Items {
		if event.Action == "run" && event.Phase == "intent" && event.PluginID == "demo" && event.RunID == page.Items[0].RunID && event.Actor == "system" && event.Outcome == "queued" {
			runIntent = true
		}
		if event.Actor != "admin" || event.PluginID != "demo" {
			continue
		}
		switch {
		case event.Action == "enable" && event.Phase == "intent":
			enabledIntent = true
		case event.Action == "enable" && event.Phase == "result" && event.Outcome == "succeeded":
			enabledResult = true
		case event.Action == "disable" && event.Phase == "intent":
			disabledIntent = true
		case event.Action == "disable" && event.Phase == "result" && event.Outcome == "succeeded":
			disabledResult = true
		}
	}
	if !enabledIntent || !enabledResult || !disabledIntent || !disabledResult || !runIntent {
		t.Fatalf("plugin operation audit incomplete: %+v", audit.Items)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAppShutdownClosesPluginJobManager(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.jobManager.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "job"}); !errors.Is(err, plugins.ErrJobStoreClosed) {
		t.Fatalf("enqueue after shutdown error = %v", err)
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
