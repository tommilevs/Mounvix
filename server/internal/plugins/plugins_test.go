package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	// Встроенный плагин должен быть доступен при наличии бинарника.
	for _, p := range r.List() {
		if p.ID == "rclone" && !p.Available {
			t.Fatal("rclone builtin unavailable despite binary")
		}
	}
}
