package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
)

func TestPluginCatalogAPIRequiresAdminAndIsNoStore(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	if err := a.store.AddUser("reader", "reader-pass", "user"); err != nil {
		t.Fatal(err)
	}
	admin := a.store.CreateSession("admin")
	reader := a.store.CreateSession("reader")

	for _, tc := range []struct {
		name       string
		cookie     string
		wantStatus int
	}{
		{name: "unauthenticated", wantStatus: http.StatusUnauthorized},
		{name: "non-admin", cookie: reader, wantStatus: http.StatusForbidden},
		{name: "admin", cookie: admin, wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/plugin-catalog", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: cookieName, Value: tc.cookie})
			}
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control=%q, want no-store", got)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("Content-Type=%q", got)
			}
			var body struct {
				Catalog struct {
					ReadOnly bool   `json:"read_only"`
					IndexURL string `json:"index_url"`
					Reason   string `json:"reason"`
				} `json:"catalog"`
				Plugins []json.RawMessage `json:"plugins"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !body.Catalog.ReadOnly || body.Catalog.IndexURL == "" || body.Catalog.Reason == "" || len(body.Plugins) != 0 {
				t.Fatalf("unexpected catalog response: %+v", body)
			}
			var shape map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &shape); err != nil {
				t.Fatal(err)
			}
			keys := make([]string, 0, len(shape))
			for key := range shape {
				keys = append(keys, key)
			}
			if !reflect.DeepEqual(sortedStrings(keys), []string{"catalog", "plugins"}) {
				t.Fatalf("response keys=%v", keys)
			}
		})
	}
}

type fixedPluginCatalogSource struct {
	catalog plugins.Catalog
	err     error
}

func (source fixedPluginCatalogSource) Fetch(context.Context) (plugins.Catalog, error) {
	return source.catalog, source.err
}

func makeVerifiedCatalogForAPITest(t *testing.T) plugins.Catalog {
	t.Helper()
	currentPublic, currentPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nextPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys := []plugins.CatalogKey{
		{ID: "api-current", PublicKey: currentPublic, Role: "current"},
		{ID: "api-next", PublicKey: nextPublic, Role: "next"},
	}
	index, err := json.Marshal(struct {
		Plugins []plugins.CatalogEntry `json:"plugins"`
	}{Plugins: []plugins.CatalogEntry{{
		ID: "demo", Name: "Demo", Version: "1.0.0", Arch: []string{runtime.GOARCH},
		Assets: map[string]plugins.CatalogAsset{"ipk": {
			URL:  "https://raw.githubusercontent.com/tommilevs/Mounvix/main/plugins/demo/1.0.0/demo.ipk",
			Size: 1, SHA256: strings.Repeat("a", 64), Arch: runtime.GOARCH,
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := json.Marshal(map[string]string{
		"key_id": "api-current", "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(currentPrivate, index)),
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := plugins.VerifyCatalog(index, envelope, keys)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestPluginCatalogAPIExposesOnlyVerifiedCatalog(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	a.catalogSource = fixedPluginCatalogSource{catalog: makeVerifiedCatalogForAPITest(t)}
	admin := a.store.CreateSession("admin")
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/plugin-catalog", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Catalog struct {
			ReadOnly bool   `json:"read_only"`
			KeyID    string `json:"key_id"`
		} `json:"catalog"`
		Plugins []plugins.CatalogEntry `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Catalog.ReadOnly || body.Catalog.KeyID != "api-current" || len(body.Plugins) != 1 || body.Plugins[0].ID != "demo" {
		t.Fatalf("verified catalog not exposed: %+v", body)
	}
}

type catalogPackageInstallCall struct {
	id    string
	asset plugins.CatalogAsset
}

type recordingCatalogPackageInstaller struct {
	calls chan catalogPackageInstallCall
}

func (installer recordingCatalogPackageInstaller) CanInstall() bool { return true }

func (installer recordingCatalogPackageInstaller) InstallFormat() (string, bool) {
	return "ipk", true
}

func (installer recordingCatalogPackageInstaller) InstallCatalogPackage(ctx context.Context, id string, asset plugins.CatalogAsset) error {
	select {
	case installer.calls <- catalogPackageInstallCall{id: id, asset: asset}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPackageInstallAPIRejectsUnverifiedCatalogContents(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	a.catalogSource = fixedPluginCatalogSource{catalog: plugins.Catalog{Plugins: []plugins.CatalogEntry{{ID: "demo", Name: "Demo", Version: "1.0.0"}}}}
	installer := recordingCatalogPackageInstaller{calls: make(chan catalogPackageInstallCall, 1)}
	a.packageInstallExecutor = installer
	admin := a.store.CreateSession("admin")
	req := httptest.NewRequest(http.MethodPut, "http://example.test/api/v1/plugin-installations/demo", bytes.NewBufferString(`{"version":"1.0.0","confirmed":true}`))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Idempotency-Key", "unverified-catalog-install")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("catalog_read_only")) {
		t.Fatalf("unverified catalog install status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case call := <-installer.calls:
		t.Fatalf("unverified catalog reached package manager: %+v", call)
	default:
	}
	page, err := a.jobStore.ListAfter(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range page.Items {
		if job.Kind == "package" && job.PluginID == "demo" {
			t.Fatalf("unverified catalog created an install job: %+v", job)
		}
	}
}

func TestPackageInstallAPIQueuesExactVerifiedCatalogVersionAndDeduplicates(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	a.catalogSource = fixedPluginCatalogSource{catalog: makeVerifiedCatalogForAPITest(t)}
	installer := recordingCatalogPackageInstaller{calls: make(chan catalogPackageInstallCall, 1)}
	a.packageInstallExecutor = installer
	admin := a.store.CreateSession("admin")
	call := func(version, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "http://example.test/api/v1/plugin-installations/demo", bytes.NewBufferString(`{"version":"`+version+`","confirmed":true}`))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	first := call("1.0.0", "install-demo-1")
	second := call("1.0.0", "install-demo-1")
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("install status=%d/%d body=%s/%s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	var firstBody, secondBody struct {
		Job plugins.JobRecord `json:"job"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatal(err)
	}
	if firstBody.Job.RunID == "" || secondBody.Job.RunID != firstBody.Job.RunID {
		t.Fatalf("install job ids=%q/%q", firstBody.Job.RunID, secondBody.Job.RunID)
	}
	select {
	case installed := <-installer.calls:
		if installed.id != "demo" || installed.asset.ID != "demo" || installed.asset.Version != "1.0.0" || installed.asset.Format != "ipk" {
			t.Fatalf("installer received unverified or wrong catalog asset: %+v", installed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued package job did not reach installer")
	}
	changed := call("2.0.0", "install-demo-1")
	if changed.Code != http.StatusConflict || !bytes.Contains(changed.Body.Bytes(), []byte("idempotency_conflict")) {
		t.Fatalf("reused key with changed request status=%d body=%s", changed.Code, changed.Body.String())
	}
	unlisted := call("2.0.0", "install-demo-2")
	if unlisted.Code != http.StatusConflict || !bytes.Contains(unlisted.Body.Bytes(), []byte("catalog_version_unavailable")) {
		t.Fatalf("unlisted version status=%d body=%s", unlisted.Code, unlisted.Body.String())
	}
}

type blockingCatalogPackageInstaller struct {
	started chan struct{}
	release <-chan struct{}
}

func (installer blockingCatalogPackageInstaller) CanInstall() bool { return true }

func (installer blockingCatalogPackageInstaller) InstallFormat() (string, bool) {
	return "ipk", true
}

func (installer blockingCatalogPackageInstaller) InstallCatalogPackage(ctx context.Context, _ string, _ plugins.CatalogAsset) error {
	select {
	case installer.started <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-installer.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPackageInstallAPIReusesPendingRunForSameVersion(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	a.catalogSource = fixedPluginCatalogSource{catalog: makeVerifiedCatalogForAPITest(t)}
	installer := blockingCatalogPackageInstaller{started: make(chan struct{}, 1), release: release}
	a.packageInstallExecutor = installer
	admin := a.store.CreateSession("admin")
	call := func(version, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "http://example.test/api/v1/plugin-installations/demo", bytes.NewBufferString(`{"version":"`+version+`","confirmed":true}`))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	first := call("1.0.0", "install-pending-1")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first install status=%d body=%s", first.Code, first.Body.String())
	}
	var firstBody struct {
		Job plugins.JobRecord `json:"job"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	select {
	case <-installer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("package install did not start")
	}
	retry := call("1.0.0", "install-pending-2")
	if retry.Code != http.StatusAccepted {
		t.Fatalf("same-version pending retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	var retryBody struct {
		Job plugins.JobRecord `json:"job"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &retryBody); err != nil {
		t.Fatal(err)
	}
	if retryBody.Job.RunID != firstBody.Job.RunID {
		t.Fatalf("same-version retry returned different jobs: %q != %q", retryBody.Job.RunID, firstBody.Job.RunID)
	}
	different := call("2.0.0", "install-pending-3")
	if different.Code != http.StatusConflict || !bytes.Contains(different.Body.Bytes(), []byte("package_busy")) {
		t.Fatalf("different version during pending install status=%d body=%s", different.Code, different.Body.String())
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := a.jobStore.ListAfter(context.Background(), 0, 1000)
		if err != nil {
			t.Fatal(err)
		}
		finished := false
		for _, run := range page.Items {
			if run.RunID == firstBody.Job.RunID {
				if run.Status == plugins.JobStatusFailed {
					t.Fatalf("package install failed: %s", run.Summary)
				}
				finished = run.Status == plugins.JobStatusSucceeded
			}
		}
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("package install did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type failingCatalogPackageInstaller struct{}

func (failingCatalogPackageInstaller) CanInstall() bool { return true }

func (failingCatalogPackageInstaller) InstallFormat() (string, bool) {
	return "ipk", true
}

func (failingCatalogPackageInstaller) InstallCatalogPackage(context.Context, string, plugins.CatalogAsset) error {
	return errors.New("simulated package-manager failure")
}

func TestFailedPackageUpdatePersistsDisabledState(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	cfg := a.configSnapshot()
	pluginRoot := pluginDirForConfig(cfg, a.cfgPath, cfg.DataDir)
	packageDir := filepath.Join(pluginRoot, "demo")
	binaryPath := filepath.Join(packageDir, "bin", "demo")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"0.9.0","api":1,"author":"test","description":"test","executable":"bin/demo"}`
	if err := os.WriteFile(filepath.Join(packageDir, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binaryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.pluginReg.ReloadPackages(); err != nil {
		t.Fatal(err)
	}
	if err := a.pluginReg.SetSourceEnabled("demo", plugins.SourcePackage, true); err != nil {
		t.Fatalf("enable package before failed update: %v", err)
	}
	a.catalogSource = fixedPluginCatalogSource{catalog: makeVerifiedCatalogForAPITest(t)}
	a.packageInstallExecutor = failingCatalogPackageInstaller{}
	job := plugins.JobRecord{
		Kind:      "package",
		PluginID:  "demo",
		JobID:     "demo",
		Operation: "update",
		Payload:   json.RawMessage(`{"version":"1.0.0","confirmed":true}`),
	}
	if _, err := a.executePackageInstall(context.Background(), job); err == nil {
		t.Fatal("expected package-manager failure")
	}
	packagePlugin, found := a.pluginReg.LookupSource("demo", plugins.SourcePackage)
	if !found || packagePlugin.Enabled {
		t.Fatalf("failed update left package enabled: found=%v plugin=%+v", found, packagePlugin)
	}
	_, enqueueErr := a.jobManager.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "run", Operation: "run", Payload: json.RawMessage(`{}`)})
	if !errors.Is(enqueueErr, plugins.ErrPluginJobsDisabled) {
		t.Fatalf("failed update left plugin jobs enabled: %v", enqueueErr)
	}
}

func TestPluginSourcePatchPersistsBuiltInState(t *testing.T) {
	a, _ := newTestApp(t)
	cfg, cfgPath := a.cfg, a.cfgPath
	admin := a.store.CreateSession("admin")
	req := httptest.NewRequest(http.MethodPatch, "http://example.test/api/v1/plugins/rclone", bytes.NewBufferString(`{"source":"builtin","enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.test")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		a.Close()
		t.Fatalf("PATCH status=%d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		a.Close()
		t.Fatalf("Cache-Control=%q, want no-store", got)
	}
	var body struct {
		Plugin struct {
			ID      string               `json:"id"`
			Source  plugins.PluginSource `json:"source"`
			Enabled bool                 `json:"enabled"`
		} `json:"plugin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		a.Close()
		t.Fatalf("decode response: %v", err)
	}
	if body.Plugin.ID != "rclone" || body.Plugin.Source != plugins.SourceBuiltin || body.Plugin.Enabled {
		a.Close()
		t.Fatalf("unexpected plugin response: %+v", body.Plugin)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := New(cfg, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	plugin, ok := reopened.pluginReg.LookupSource("rclone", plugins.SourceBuiltin)
	if !ok || plugin.Enabled {
		t.Fatalf("built-in state after restart = %+v, found=%v; want disabled", plugin, ok)
	}
}

func TestPluginInstallationsAPIExposesSafeReadOnlyState(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	admin := a.store.CreateSession("admin")
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://example.test"+path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if method != http.MethodGet {
			req.Header.Set("Origin", "http://example.test")
			req.Header.Set("Idempotency-Key", "install-demo-v1")
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}

	list := request(http.MethodGet, "/api/v1/plugin-installations", "")
	if list.Code != http.StatusOK {
		t.Fatalf("GET status=%d, want 200; body=%s", list.Code, list.Body.String())
	}
	if list.Header().Get("Cache-Control") != "no-store" || list.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected response headers: %v", list.Header())
	}
	var body struct {
		Items []struct {
			ID              string               `json:"id"`
			EffectiveSource plugins.PluginSource `json:"effective_source"`
			BuiltinPresent  bool                 `json:"builtin_present"`
			BuiltinEnabled  bool                 `json:"builtin_enabled"`
			PackagePresent  bool                 `json:"package_present"`
			LegacyPresent   bool                 `json:"legacy_present"`
			Shadowed        bool                 `json:"shadowed"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode installations: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "rclone" || body.Items[0].EffectiveSource != plugins.SourceBuiltin || !body.Items[0].BuiltinPresent || body.Items[0].PackagePresent || body.Items[0].LegacyPresent || body.Items[0].Shadowed {
		t.Fatalf("unexpected installations: %+v", body.Items)
	}
	if bytes.Contains(list.Body.Bytes(), []byte(`"command"`)) || bytes.Contains(list.Body.Bytes(), []byte(`"filesystem_path"`)) {
		t.Fatalf("response leaks executable or local path metadata: %s", list.Body.String())
	}

	install := request(http.MethodPut, "/api/v1/plugin-installations/demo", `{"version":"1.0.0","confirmed":true}`)
	if install.Code != http.StatusConflict {
		t.Fatalf("PUT status=%d, want 409; body=%s", install.Code, install.Body.String())
	}
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(install.Body.Bytes(), &failure); err != nil || failure.Error.Code != "catalog_read_only" {
		t.Fatalf("PUT error=%+v decode=%v", failure.Error, err)
	}
}

func TestPluginJobsAPIUsesLatestPageAndRedactedLogs(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	admin := a.store.CreateSession("admin")
	var records []plugins.JobRecord
	for index := 0; index < 3; index++ {
		record, err := a.jobStore.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "rclone", JobID: "rclone.sync", Payload: json.RawMessage(`{"local_path":"/private/source"}`)})
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	logs := plugins.NewLogStore(a.configSnapshot().DataDir)
	if err := logs.WriteRun("rclone", "rclone", records[2].RunID, []byte("Authorization: Bearer private-token\n")); err != nil {
		t.Fatal(err)
	}
	packageRecord, err := a.jobStore.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "package", PluginID: "demo", JobID: "demo", Operation: "remove"})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}

	page := call("/api/v1/jobs?limit=2")
	if page.Code != http.StatusOK || page.Header().Get("Cache-Control") != "no-store" || page.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("jobs page status=%d headers=%v body=%s", page.Code, page.Header(), page.Body.String())
	}
	var pageBody struct {
		Items      []plugins.JobRecord `json:"items"`
		NextCursor uint64              `json:"next_cursor"`
		Reset      bool                `json:"reset"`
		LatestSeq  uint64              `json:"latest_seq"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &pageBody); err != nil {
		t.Fatal(err)
	}
	if len(pageBody.Items) != 2 || pageBody.Items[0].RunID != records[2].RunID || pageBody.Items[1].RunID != packageRecord.RunID || pageBody.NextCursor != packageRecord.Seq || pageBody.Reset || pageBody.LatestSeq != packageRecord.Seq {
		t.Fatalf("unexpected latest page: %+v", pageBody)
	}

	detail := call("/api/v1/jobs/" + records[2].RunID)
	if detail.Code != http.StatusOK || bytes.Contains(detail.Body.Bytes(), []byte("private/source")) || bytes.Contains(detail.Body.Bytes(), []byte(`"payload"`)) || bytes.Contains(detail.Body.Bytes(), []byte(`"actor_id"`)) {
		t.Fatalf("unsafe job detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	logResponse := call("/api/v1/jobs/" + records[2].RunID + "/logs")
	if logResponse.Code != http.StatusOK || bytes.Contains(logResponse.Body.Bytes(), []byte("private-token")) || !bytes.Contains(logResponse.Body.Bytes(), []byte("[REDACTED]")) {
		t.Fatalf("run log was not safely redacted: status=%d body=%s", logResponse.Code, logResponse.Body.String())
	}
	packageLog := call("/api/v1/jobs/" + packageRecord.RunID + "/logs")
	if packageLog.Code != http.StatusNotFound || !bytes.Contains(packageLog.Body.Bytes(), []byte(`"log_unavailable"`)) || bytes.Contains(packageLog.Body.Bytes(), []byte("/private")) {
		t.Fatalf("package log response status=%d body=%s", packageLog.Code, packageLog.Body.String())
	}
}

func TestPluginJobCancellationAPIIsOriginGuardedAuditedAndIdempotent(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	admin := a.store.CreateSession("admin")
	job, err := a.jobStore.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	call := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/jobs/"+job.RunID+"/cancellations", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	if got := call("http://evil.test").Code; got != http.StatusForbidden {
		t.Fatalf("foreign-origin cancellation status=%d, want 403", got)
	}
	for attempt := 0; attempt < 2; attempt++ {
		response := call("http://example.test")
		if response.Code != http.StatusOK {
			t.Fatalf("cancel attempt %d status=%d body=%s", attempt+1, response.Code, response.Body.String())
		}
		var body struct {
			Job plugins.JobRecord `json:"job"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Job.Status != plugins.JobStatusCancelled || body.Job.RunID != job.RunID {
			t.Fatalf("cancel response job=%+v error=%v", body.Job, err)
		}
	}
	page, err := a.jobStore.ListAfter(context.Background(), 0, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != plugins.JobStatusCancelled {
		t.Fatalf("durable job after cancel=%+v err=%v", page.Items, err)
	}
	data, err := os.ReadFile(filepath.Join(a.configSnapshot().DataDir, "plugin-audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Items []plugins.AuditEvent `json:"items"`
	}
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatal(err)
	}
	var intent, result int
	for _, event := range audit.Items {
		if event.Action == "cancel" && event.RunID == job.RunID {
			if event.Phase == "intent" {
				intent++
			}
			if event.Phase == "result" {
				result++
			}
		}
	}
	if intent != 1 || result != 1 {
		t.Fatalf("cancellation audit intent/result counts = %d/%d", intent, result)
	}
}

func TestPluginJobRunAPIValidatesManifestAndDeduplicatesRetries(t *testing.T) {
	base := t.TempDir()
	pluginRoot := filepath.Join(base, "packages")
	pkgDir := filepath.Join(pluginRoot, "demo")
	if err := os.MkdirAll(filepath.Join(pkgDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "bin", "demo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"test","executable":"bin/demo","runtime_methods":["plugin.runJob"],"jobs":[{"id":"sync","schedule":"0 0 1 1 *","timeout_seconds":30},{"id":"daily","schedule":"0 0 1 1 *","timeout_seconds":30}]}`
	if err := os.WriteFile(filepath.Join(pkgDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(base, "data")
	cfg.PluginDir = pluginRoot
	a, err := New(cfg, filepath.Join(base, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.setPluginSourceEnabled(context.Background(), "admin", "demo", plugins.SourcePackage, true); err != nil {
		t.Fatal(err)
	}
	admin := a.store.CreateSession("admin")
	call := func(jobID, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/plugins/demo/jobs/"+jobID+"/runs", bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Idempotency-Key", key)
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	if invalid := call("missing", "missing-key", ""); invalid.Code != http.StatusNotFound {
		t.Fatalf("unknown manifest job status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	if nonempty := call("sync", "body-key", `{}`); nonempty.Code != http.StatusBadRequest {
		t.Fatalf("non-empty run body status=%d body=%s", nonempty.Code, nonempty.Body.String())
	}
	first := call("sync", "same-key", "")
	second := call("sync", "same-key", "")
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("run responses status=%d/%d headers=%v body=%s", first.Code, second.Code, first.Header(), first.Body.String())
	}
	var firstBody, secondBody struct {
		Job plugins.JobRecord `json:"job"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatal(err)
	}
	if firstBody.Job.RunID == "" || secondBody.Job.RunID != firstBody.Job.RunID {
		t.Fatalf("idempotent run ids = %q and %q", firstBody.Job.RunID, secondBody.Job.RunID)
	}
	if conflict := call("daily", "same-key", ""); conflict.Code != http.StatusConflict {
		t.Fatalf("changed request with reused key status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestRcloneJobsAPIUsesAllowlistedRemotesAndIdempotency(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	root := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(root, "daily"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.rcloneAdapter = plugins.RcloneAdapter{
		SourceRoot: root,
		LookPath:   func(string) (string, error) { return "/bin/echo", nil },
		ListRemotesFunc: func(context.Context) ([]string, error) {
			return []string{"backup"}, nil
		},
		LogStore: plugins.NewLogStore(a.configSnapshot().DataDir),
	}
	admin := a.store.CreateSession("admin")
	call := func(method, path, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://example.test"+path, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		req.Header.Set("Origin", "http://example.test")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	remotes := call(http.MethodGet, "/api/v1/rclone-remotes", "", "")
	if remotes.Code != http.StatusOK || !bytes.Contains(remotes.Body.Bytes(), []byte(`"backup"`)) || remotes.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rclone remotes status=%d body=%s headers=%v", remotes.Code, remotes.Body.String(), remotes.Header())
	}
	missingConfirmation := call(http.MethodPost, "/api/v1/rclone-jobs", "confirmation-key", `{"source_path":"daily","remote_name":"backup","destination_path":"archive"}`)
	if missingConfirmation.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed sync status=%d body=%s", missingConfirmation.Code, missingConfirmation.Body.String())
	}
	request := `{"source_path":"daily","remote_name":"backup","destination_path":"archive","confirmed":true}`
	first := call(http.MethodPost, "/api/v1/rclone-jobs", "sync-key", request)
	second := call(http.MethodPost, "/api/v1/rclone-jobs", "sync-key", request)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("sync status=%d/%d body=%s/%s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	var firstBody, secondBody struct {
		Job plugins.JobRecord `json:"job"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatal(err)
	}
	if firstBody.Job.RunID == "" || secondBody.Job.RunID != firstBody.Job.RunID {
		t.Fatalf("idempotent rclone run ids=%q/%q", firstBody.Job.RunID, secondBody.Job.RunID)
	}
	conflict := call(http.MethodPost, "/api/v1/rclone-jobs", "sync-key", `{"source_path":"daily","remote_name":"backup","destination_path":"other","confirmed":true}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("changed sync request status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	invalid := call(http.MethodPost, "/api/v1/rclone-jobs", "bad-path-key", `{"source_path":"../private","remote_name":"backup","destination_path":"archive","confirmed":true}`)
	if invalid.Code != http.StatusBadRequest || bytes.Contains(invalid.Body.Bytes(), []byte(root)) {
		t.Fatalf("unsafe rclone path response status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := a.jobStore.ListAfter(context.Background(), 0, 1000)
		if err != nil {
			t.Fatal(err)
		}
		finished := false
		for _, run := range page.Items {
			if run.RunID == firstBody.Job.RunID {
				if run.Status == plugins.JobStatusFailed || run.Status == plugins.JobStatusCancelled || run.Status == plugins.JobStatusInterrupted {
					t.Fatalf("rclone job unexpectedly ended %s: %s", run.Status, run.Summary)
				}
				finished = run.Status == plugins.JobStatusSucceeded
			}
		}
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rclone job did not finish before deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	logsResponse := call(http.MethodGet, "/api/v1/jobs/"+firstBody.Job.RunID+"/logs", "", "")
	if logsResponse.Code != http.StatusOK || !bytes.Contains(logsResponse.Body.Bytes(), []byte("sync")) {
		t.Fatalf("rclone run logs status=%d body=%s", logsResponse.Code, logsResponse.Body.String())
	}
	if bytes.Contains(logsResponse.Body.Bytes(), []byte(root)) {
		t.Fatalf("rclone run logs exposed local source path: %s", logsResponse.Body.String())
	}
}

type blockingPackageRemover struct {
	started chan string
	release <-chan struct{}
}

func (r blockingPackageRemover) CanRemove() bool { return true }

func (r blockingPackageRemover) Remove(ctx context.Context, id string) error {
	select {
	case r.started <- id:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestPackageRemovalAPIUsesValidatedInstalledIDAndDeduplicates(t *testing.T) {
	base := t.TempDir()
	pluginRoot := filepath.Join(base, "packages")
	pkgDir := filepath.Join(pluginRoot, "demo")
	if err := os.MkdirAll(filepath.Join(pkgDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "bin", "demo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"test","executable":"bin/demo"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(base, "data", "plugins")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyManifest := `{"id":"demo","name":"Legacy demo","version":"0.9.0","kind":"process","command":"/bin/true"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "demo.json"), []byte(legacyManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(base, "data")
	cfg.PluginDir = pluginRoot
	a, err := New(cfg, "/opt/etc/mounvix/config.json")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	a.packageInstaller = blockingPackageRemover{started: started, release: release}
	if err := a.setPluginSourceEnabled(context.Background(), "admin", "demo", plugins.SourceLegacy, true); err != nil {
		t.Fatal(err)
	}
	admin := a.store.CreateSession("admin")
	call := func(id, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "http://example.test/api/v1/plugin-installations/"+id, bytes.NewBufferString(body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: admin})
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Idempotency-Key", key)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	if err := a.setPluginSourceEnabled(context.Background(), "admin", "demo", plugins.SourcePackage, true); err != nil {
		t.Fatal(err)
	}
	enabledRemoval := call("demo", "remove-enabled", `{"confirmed_legacy_fallback":false}`)
	if enabledRemoval.Code != http.StatusConflict {
		t.Fatalf("enabled package DELETE status=%d body=%s", enabledRemoval.Code, enabledRemoval.Body.String())
	}
	if err := a.setPluginSourceEnabled(context.Background(), "admin", "demo", plugins.SourcePackage, false); err != nil {
		t.Fatal(err)
	}
	activePluginRun, err := a.jobStore.Enqueue(context.Background(), plugins.JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "run-test", Payload: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.jobStore.MarkRunning(context.Background(), activePluginRun.RunID); err != nil {
		t.Fatal(err)
	}
	runningRemoval := call("demo", "remove-running-key", `{"confirmed_legacy_fallback":true}`)
	if runningRemoval.Code != http.StatusConflict || !bytes.Contains(runningRemoval.Body.Bytes(), []byte("package_busy")) {
		t.Fatalf("DELETE during active plugin job status=%d body=%s", runningRemoval.Code, runningRemoval.Body.String())
	}
	if err := a.jobStore.Finish(context.Background(), activePluginRun.RunID, "success", "completed"); err != nil {
		t.Fatal(err)
	}
	fallbackNoConfirm := call("demo", "remove-unconfirmed", `{"confirmed_legacy_fallback":false}`)
	if fallbackNoConfirm.Code != http.StatusConflict || !bytes.Contains(fallbackNoConfirm.Body.Bytes(), []byte("legacy_fallback_confirmation_required")) {
		t.Fatalf("unconfirmed fallback DELETE status=%d body=%s", fallbackNoConfirm.Code, fallbackNoConfirm.Body.String())
	}
	first := call("demo", "remove-key", `{"confirmed_legacy_fallback":true}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("DELETE status=%d body=%s", first.Code, first.Body.String())
	}
	select {
	case id := <-started:
		if id != "demo" {
			t.Fatalf("remover got ID %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("validated package removal did not start")
	}
	enableDone := make(chan error, 1)
	go func() {
		enableDone <- a.setPluginSourceEnabled(context.Background(), "admin", "demo", plugins.SourcePackage, true)
	}()
	select {
	case err := <-enableDone:
		t.Fatalf("package source changed during in-flight removal: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	busy := call("demo", "another-remove-key", `{"confirmed_legacy_fallback":true}`)
	if busy.Code != http.StatusConflict || !bytes.Contains(busy.Body.Bytes(), []byte("package_busy")) {
		t.Fatalf("different key during running removal status=%d body=%s", busy.Code, busy.Body.String())
	}
	second := call("demo", "remove-key", `{"confirmed_legacy_fallback":true}`)
	if second.Code != http.StatusAccepted {
		t.Fatalf("idempotent DELETE status=%d body=%s", second.Code, second.Body.String())
	}
	var firstBody, secondBody struct {
		Job plugins.JobRecord `json:"job"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatal(err)
	}
	if firstBody.Job.RunID == "" || secondBody.Job.RunID != firstBody.Job.RunID {
		t.Fatalf("remove run ids=%q/%q", firstBody.Job.RunID, secondBody.Job.RunID)
	}
	close(release)
	released = true
	deadline := time.After(3 * time.Second)
	for {
		if _, ok := a.pluginReg.LookupSource("demo", plugins.SourcePackage); !ok {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("package source remained registered after successful removal")
		}
	}
	select {
	case duplicate := <-started:
		t.Fatalf("duplicate remove executed for %q", duplicate)
	default:
	}
	select {
	case err := <-enableDone:
		if err == nil {
			t.Fatal("package source was re-enabled after removal")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("package source toggle remained blocked after removal finished")
	}
	legacyFallback, ok := a.pluginReg.LookupSource("demo", plugins.SourceLegacy)
	if !ok || !legacyFallback.Enabled {
		t.Fatalf("enabled legacy fallback was not preserved after removal: %#v, found=%v", legacyFallback, ok)
	}
	absent := call("demo", "remove-after-absent", `{"confirmed_legacy_fallback":true}`)
	if absent.Code != http.StatusNoContent || absent.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("absent package DELETE status=%d headers=%v body=%s", absent.Code, absent.Header(), absent.Body.String())
	}
}

func sortedStrings(values []string) []string {
	for i := range values {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
	return values
}
