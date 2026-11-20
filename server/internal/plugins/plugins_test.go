package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManifestsAndState(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	man := map[string]any{"id": "demo", "name": "Demo", "version": "1.0", "description": "test", "kind": "process", "command": "/bin/true"}
	b, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(dir, "plugins", "demo.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found *Plugin
	for _, p := range r.List() {
		if p.ID == "demo" {
			found = p
		}
	}
	if found == nil || found.Available != true || found.Builtin {
		t.Fatalf("demo plugin: %+v", found)
	}
	if err := r.SetEnabled("demo", true); err != nil {
		t.Fatal(err)
	}
	r2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r2.List() {
		if p.ID == "demo" && !p.Enabled {
			t.Fatal("enabled state not persisted")
		}
	}
}

func TestRegistryDisableHookDoesNotHoldStateMutex(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "demo.json"), []byte(`{"id":"demo","name":"Demo","version":"1","kind":"process","command":"/bin/true"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(dir, "", "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- registry.SetSourceEnabledAsWithHooks("admin", "demo", SourceLegacy, false, func() error {
			if _, ok := registry.LookupSource("demo", SourceLegacy); !ok {
				return os.ErrNotExist
			}
			return nil
		}, nil)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("disable hook blocked on the registry state mutex")
	}
}

func TestRegistryRunsDisableHookAfterDurableIntentAndRollsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "demo.json"), []byte(`{"id":"demo","name":"Demo","version":"1","kind":"process","command":"/bin/true"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(dir, "", "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	hookCalled := false
	err = registry.SetSourceEnabledAsWithHooks("admin", "demo", SourceLegacy, false, func() error {
		hookCalled = true
		events, err := registry.audit.Recent(10)
		if err != nil {
			return err
		}
		if len(events) != 1 || events[0].Phase != "intent" || events[0].Action != "disable" {
			t.Fatalf("disable hook ran before durable intent: %+v", events)
		}
		return nil
	}, nil)
	if err != nil || !hookCalled {
		t.Fatalf("SetSourceEnabledAsWithHooks() err=%v hook=%v", err, hookCalled)
	}
	plugin, ok := registry.LookupSource("demo", SourceLegacy)
	if !ok || plugin.Enabled {
		t.Fatalf("demo state after disable = %+v found=%v", plugin, ok)
	}

	rollbackCalled := false
	err = registry.SetSourceEnabledAsWithHooks("admin", "demo", SourceLegacy, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = registry.SetSourceEnabledAsWithHooks("admin", "demo", SourceLegacy, false, func() error { return os.ErrPermission }, func() error {
		rollbackCalled = true
		return nil
	})
	if err == nil || !rollbackCalled {
		t.Fatalf("disable failure err=%v rollback=%v", err, rollbackCalled)
	}
	plugin, ok = registry.LookupSource("demo", SourceLegacy)
	if !ok || !plugin.Enabled {
		t.Fatalf("demo state after failed disable = %+v found=%v", plugin, ok)
	}
}

func TestRegistryAuditsEnableIntentAndResult(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"1","kind":"process","command":"/bin/true"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "demo.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetEnabledAs("admin", "demo", true); err != nil {
		t.Fatal(err)
	}
	events, err := registry.audit.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("audit events = %+v, want intent and result", events)
	}
	if events[0].Actor != "admin" || events[0].Phase != "intent" || events[0].Action != "enable" || events[0].PluginID != "demo" {
		t.Fatalf("enable intent event = %+v", events[0])
	}
	if events[1].Actor != "admin" || events[1].Phase != "result" || events[1].Action != "enable" || events[1].Outcome != "succeeded" {
		t.Fatalf("enable result event = %+v", events[1])
	}
}

func TestRegistryForgetsExternallyRemovedPackageAndKeepsLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	pluginRoot := filepath.Join(dir, "system-plugins")
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
	legacyDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"demo","name":"Legacy Demo","version":"0.9.0","kind":"process","command":"/bin/true"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "demo.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(dir, pluginRoot, "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetSourceEnabledAs("admin", "demo", SourceLegacy, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.ForgetPackageAfterExternalRemoval("demo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.LookupSource("demo", SourcePackage); ok {
		t.Fatal("removed package source remains registered")
	}
	legacyPlugin, ok := registry.LookupSource("demo", SourceLegacy)
	if !ok || !legacyPlugin.Enabled || legacyPlugin.Version != "0.9.0" {
		t.Fatalf("legacy fallback after removal = %+v found=%v", legacyPlugin, ok)
	}
}

func TestRegistryAuditsPackageRemovalIntentAndResult(t *testing.T) {
	dir := t.TempDir()
	pluginRoot := filepath.Join(dir, "system-plugins")
	pluginDir := filepath.Join(pluginRoot, "demo")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "bin", "demo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"test","description":"test","executable":"bin/demo"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(dir, pluginRoot, "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RemovePackageAs("admin", "demo", true); err != nil {
		t.Fatal(err)
	}
	events, err := registry.audit.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "remove" || events[0].Phase != "intent" || events[1].Action != "remove" || events[1].Phase != "result" || events[1].Outcome != "succeeded" {
		t.Fatalf("removal audit events = %+v", events)
	}
}

func TestRegistryPackagePrecedenceAndSourceSpecificState(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "system-plugins")
	legacyDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"demo","name":"Legacy Demo","version":"1.0","kind":"process","command":"/bin/true"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "demo.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins-state.json"), []byte(`{"enabled":["demo","rclone","future-plugin"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(filepath.Join(pkgDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "bin", "demo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	packageManifest := `{"id":"demo","name":"Package Demo","version":"2.0.0","api":1,"author":"A","description":"D","executable":"bin/demo"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "plugin.json"), []byte(packageManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRegistry(dir, root, "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	list := r.List()
	var effective *Plugin
	for _, p := range list {
		if p.ID == "demo" {
			effective = p
		}
	}
	if effective == nil || effective.Name != "Package Demo" || effective.Source != SourcePackage || effective.Enabled {
		t.Fatalf("effective package: %+v", effective)
	}
	legacyEntry, ok := r.LookupSource("demo", SourceLegacy)
	if !ok || legacyEntry.Name != "Legacy Demo" || !legacyEntry.Enabled {
		t.Fatalf("shadowed legacy entry not preserved: %+v %v", legacyEntry, ok)
	}
	if _, ok := r.LookupSource("future-plugin", SourceLegacy); ok {
		t.Fatal("unknown id unexpectedly loaded as a plugin")
	}
	if err := r.SetSourceEnabled("demo", SourcePackage, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSourceEnabled("demo", SourceLegacy, false); err != nil {
		t.Fatal(err)
	}
	r2, err := NewRegistry(dir, root, "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	pkg, _ := r2.LookupSource("demo", SourcePackage)
	old, _ := r2.LookupSource("demo", SourceLegacy)
	if !pkg.Enabled || old.Enabled {
		t.Fatalf("source state was not independent: package=%+v legacy=%+v", pkg, old)
	}
	if err := r2.SetSourceEnabled("demo", SourceLegacy, true); err != nil {
		t.Fatal(err)
	}
	if err := r2.RemovePackage("demo", false); err == nil {
		t.Fatal("unconfirmed enabled legacy fallback accepted")
	}
	if err := r2.RemovePackage("demo", true); err != nil {
		t.Fatalf("confirmed removal: %v", err)
	}
	fallback := r2.List()
	for _, p := range fallback {
		if p.ID == "demo" {
			if p.Source != SourceLegacy || !p.Enabled {
				t.Fatalf("legacy fallback not revealed: %+v", p)
			}
			return
		}
	}
	t.Fatal("legacy fallback missing after package removal")
}

func TestRegistryLegacyStateMigrationIsReadOnlyUntilMutation(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "known.json"), []byte(`{"id":"known","name":"Known","version":"1","kind":"process"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldState := []byte(`{"enabled":["known","rclone","unknown"]}`)
	statePath := filepath.Join(dir, "plugins-state.json")
	if err := os.WriteFile(statePath, oldState, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir, "", "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(oldState) {
		t.Fatal("legacy state rewritten during load")
	}
	if _, ok := r.LookupSource("unknown", SourceLegacy); ok {
		t.Fatal("unknown legacy ID should be preserved in state, not loaded as a manifest")
	}
	if builtin, ok := r.LookupSource("rclone", SourceBuiltin); !ok || !builtin.Enabled {
		t.Fatalf("old rclone ID was not migrated to builtin: %+v", builtin)
	}
	if err := r.SetSourceEnabled("known", SourceLegacy, false); err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	got, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &state); err != nil {
		t.Fatal(err)
	}
	if state["version"] != float64(2) {
		t.Fatalf("state version=%v", state["version"])
	}
	if _, ok := state["package_enabled"]; !ok {
		t.Fatal("package_enabled missing in v2 state")
	}
	if _, ok := state["builtin_enabled"]; !ok {
		t.Fatal("builtin_enabled missing in v2 state")
	}
	legacyIDs, _ := state["enabled"].([]any)
	foundUnknown := false
	for _, id := range legacyIDs {
		if id == "unknown" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatal("unknown legacy ID was discarded")
	}
}

func TestRegistryLegacyRcloneStateMapsToLegacySource(t *testing.T) {
	dir := t.TempDir()
	legacyDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"rclone","name":"Legacy rclone","version":"1.0","kind":"process"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "rclone.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins-state.json"), []byte(`{"enabled":["rclone"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(dir, "", "1.0.0", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	legacyEntry, ok := r.LookupSource("rclone", SourceLegacy)
	if !ok || !legacyEntry.Enabled {
		t.Fatalf("legacy rclone state not preserved: %+v", legacyEntry)
	}
	builtin, ok := r.LookupSource("rclone", SourceBuiltin)
	if !ok || builtin.Enabled {
		t.Fatalf("legacy rclone state incorrectly enabled builtin: %+v", builtin)
	}
}

func TestRcloneDetection(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then echo 'rclone v9.9.9-test'; fi\nif [ \"$1\" = listremotes ]; then echo 'gdrive:'; echo 'yandex:'; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st := r.RcloneStatus()
	if !st.Installed || st.Version == "" {
		t.Fatalf("rclone not detected: %+v", st)
	}
	if len(st.Remotes) != 2 || st.Remotes[0] != "gdrive" || st.Remotes[1] != "yandex" {
		t.Fatalf("remotes: %+v", st.Remotes)
	}
	for _, p := range r.List() {
		if p.ID == "rclone" && !p.Available {
			t.Fatal("rclone builtin unavailable despite binary")
		}
	}
}
