package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		ID: "demo", Name: "Demo", Version: "1.0.0", API: 1, Author: "Mounvix",
		Description: "Example plugin", MinApp: "0.1.0", Arch: []string{"amd64"},
		Capabilities: Capabilities{FSPaths: []string{"/srv/media"}, Net: true, Schedule: true},
		Pages:        []Page{{ID: "home", Path: "pages/home.html"}},
		Jobs:         []Job{{ID: "sync", Schedule: "0 * * * *", TimeoutSeconds: 300}},
		Hooks:        []string{"upload.done"},
		Endpoints:    []Endpoint{{ID: "status", Method: "GET", Path: "status"}},
		Executable:   "bin/plugin",
	}
}

func TestParseManifest(t *testing.T) {
	input := `{"id":"demo","name":"Demo","version":"1.0.0","api":1,"author":"A","description":"D"}`
	got, err := ParseManifest(strings.NewReader(input))
	if err != nil || got.ID != "demo" {
		t.Fatalf("ParseManifest() = %+v, %v", got, err)
	}
	_, err = ParseManifest(strings.NewReader(strings.Repeat("x", 64*1024+1)))
	if err == nil {
		t.Fatal("oversized manifest accepted")
	}
}

func TestManifestValidateRejectsUnsupportedAndUnsafeContracts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"invalid ID", func(m *Manifest) { m.ID = "../bad" }},
		{"unsupported API", func(m *Manifest) { m.API = 2 }},
		{"unsupported architecture", func(m *Manifest) { m.Arch = []string{"other"} }},
		{"minimum app version", func(m *Manifest) { m.MinApp = "99.0.0" }},
		{"invalid minimum app version", func(m *Manifest) { m.MinApp = "v1.invalid.0" }},
		{"missing executable", func(m *Manifest) { m.Executable = "" }},
		{"duplicate menu IDs", func(m *Manifest) { m.Menu = []MenuItem{{ID: "home", Label: "Home"}, {ID: "home", Label: "Duplicate"}} }},
		{"duplicate page IDs", func(m *Manifest) { m.Pages = append(m.Pages, m.Pages[0]) }},
		{"duplicate job IDs", func(m *Manifest) { m.Jobs = append(m.Jobs, m.Jobs[0]) }},
		{"duplicate endpoint IDs", func(m *Manifest) { m.Endpoints = append(m.Endpoints, m.Endpoints[0]) }},
		{"job timeout too short", func(m *Manifest) { m.Jobs[0].TimeoutSeconds = 0 }},
		{"job timeout too long", func(m *Manifest) { m.Jobs[0].TimeoutSeconds = 601 }},
		{"cron minute out of range", func(m *Manifest) { m.Jobs[0].Schedule = "60 * * * *" }},
		{"cron field count", func(m *Manifest) { m.Jobs[0].Schedule = "* * * *" }},
		{"unsupported endpoint method", func(m *Manifest) { m.Endpoints[0].Method = "TRACE" }},
		{"unsupported hook", func(m *Manifest) { m.Hooks = []string{"file.indexed"} }},
		{"absolute executable", func(m *Manifest) { m.Executable = "/bin/true" }},
		{"traversal page", func(m *Manifest) { m.Pages[0].Path = "../outside.html" }},
		{"duplicate hook", func(m *Manifest) { m.Hooks = append(m.Hooks, m.Hooks[0]) }},
		{"reserved package id", func(m *Manifest) { m.ID = "rclone" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			tt.mutate(&m)
			if err := m.Validate("1.0.0", "amd64"); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestManifestValidateAcceptsValidContract(t *testing.T) {
	m := validManifest()
	if err := m.Validate("1.0.0", "amd64"); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestManifestValidateAcceptsTaggedBuildVersion(t *testing.T) {
	m := validManifest()
	if err := m.Validate("v1.0.0", "amd64"); err != nil {
		t.Fatalf("tagged app version rejected: %v", err)
	}
}

func TestManifestValidateTreatsDevBuildAsUnbounded(t *testing.T) {
	m := validManifest()
	if err := m.Validate("dev", "amd64"); err != nil {
		t.Fatalf("development app version rejected: %v", err)
	}
}

func TestValidatePackagePathsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "plugin"), []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	outsidePage := filepath.Join(outside, "home.html")
	if err := os.WriteFile(outsidePage, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePage, filepath.Join(root, "pages", "home.html")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ValidatePackagePaths(root, validManifest()); err == nil {
		t.Fatal("package symlink escape accepted")
	}
}
