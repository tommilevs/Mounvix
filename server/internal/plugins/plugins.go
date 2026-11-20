// Package plugins — manifest loader and source-aware plugin registry.
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type PluginSource string

const (
	SourceLegacy  PluginSource = "legacy"
	SourcePackage PluginSource = "package"
	SourceBuiltin PluginSource = "builtin"
)

type Plugin struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description,omitempty"`
	Kind        string       `json:"kind"`
	Command     string       `json:"command,omitempty"`
	Builtin     bool         `json:"builtin"`
	Enabled     bool         `json:"enabled"`
	Available   bool         `json:"available"`
	Source      PluginSource `json:"source,omitempty"`
	Manifest    *Manifest    `json:"manifest,omitempty"`
}

type state struct {
	Version        int      `json:"version,omitempty"`
	Enabled        []string `json:"enabled"`
	PackageEnabled []string `json:"package_enabled"`
	BuiltinEnabled []string `json:"builtin_enabled"`
}
type Registry struct {
	mu             sync.Mutex
	dir            string
	pluginRoot     string
	appVersion     string
	arch           string
	sources        map[PluginSource]map[string]*Plugin
	legacyEnabled  map[string]bool
	packageEnabled map[string]bool
	builtinEnabled map[string]bool
	legacyStateIDs map[string]bool
	audit          *AuditStore
}

func New(dir string) (*Registry, error) { return NewRegistry(dir, "", "", "") }
func NewRegistry(dataDir, pluginRoot, appVersion, arch string) (*Registry, error) {
	if dataDir == "" {
		return nil, errors.New("data directory is required")
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	r := &Registry{dir: dataDir, pluginRoot: pluginRoot, appVersion: appVersion, arch: arch, sources: map[PluginSource]map[string]*Plugin{SourceLegacy: {}, SourcePackage: {}, SourceBuiltin: {}}, legacyEnabled: map[string]bool{}, packageEnabled: map[string]bool{}, builtinEnabled: map[string]bool{}, legacyStateIDs: map[string]bool{}}
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0o755); err != nil {
		return nil, err
	}
	var err error
	r.audit, err = NewAuditStore(dataDir)
	if err != nil {
		return nil, err
	}
	if err = r.loadState(); err != nil {
		return nil, err
	}
	if err = r.loadLegacy(); err != nil {
		return nil, err
	}
	if err = r.loadPackages(); err != nil {
		return nil, err
	}
	r.loadBuiltin()
	// Migrate the old flat enabled-ID set by source without writing until mutation.
	for id := range r.legacyStateIDs {
		if _, ok := r.sources[SourceLegacy][id]; ok {
			r.legacyEnabled[id] = true
		} else if id == "rclone" {
			r.builtinEnabled[id] = true
		} else {
			r.legacyEnabled[id] = true
		}
	}
	r.legacyStateIDs = map[string]bool{}
	for source, items := range r.sources {
		for id, p := range items {
			p.Source = source
			switch source {
			case SourceLegacy:
				p.Enabled = r.legacyEnabled[id]
			case SourcePackage:
				p.Enabled = r.packageEnabled[id]
			case SourceBuiltin:
				p.Enabled = r.builtinEnabled[id]
			}
		}
	}
	for id := range r.sources[SourcePackage] {
		if _, ok := r.sources[SourceLegacy][id]; ok {
			if err := r.audit.Append(AuditEvent{Actor: "system", Phase: "completed", Action: "manifest_conflict", PluginID: id, Source: SourcePackage, Outcome: "package manifest shadows legacy manifest"}); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}
func (r *Registry) loadState() error {
	b, err := os.ReadFile(filepath.Join(r.dir, "plugins-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st state
	if err = json.Unmarshal(b, &st); err != nil {
		return nil
	} // Preserve malformed legacy state; do not erase it on startup.
	if st.Version >= 2 {
		for _, id := range st.Enabled {
			r.legacyEnabled[id] = true
		}
		for _, id := range st.PackageEnabled {
			r.packageEnabled[id] = true
		}
		for _, id := range st.BuiltinEnabled {
			r.builtinEnabled[id] = true
		}
	} else {
		for _, id := range st.Enabled {
			r.legacyStateIDs[id] = true
		}
	}
	return nil
}
func (r *Registry) loadLegacy() error {
	root := filepath.Join(r.dir, "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		var p Plugin
		if json.Unmarshal(b, &p) != nil || p.ID == "" {
			continue
		}
		p.Builtin = false
		p.Available = true
		p.Source = SourceLegacy
		r.sources[SourceLegacy][p.ID] = &p
	}
	return nil
}
func (r *Registry) loadPackages() error {
	if r.pluginRoot == "" {
		return nil
	}
	root, err := filepath.Abs(r.pluginRoot)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !pluginIDPattern.MatchString(entry.Name()) || entry.Name() == "rclone" {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil || !withinRoot(root, resolved) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(resolved, "plugin.json"))
		if err != nil {
			continue
		}
		m, err := ParseManifest(strings.NewReader(string(b)))
		if err != nil || m.ID != entry.Name() || m.Validate(r.appVersion, r.arch) != nil {
			continue
		}
		if err = ValidatePackagePaths(resolved, m); err != nil {
			continue
		}
		p := &Plugin{ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description, Kind: "process", Command: filepath.Join(resolved, filepath.FromSlash(m.Executable)), Available: true, Source: SourcePackage, Manifest: &m}
		r.sources[SourcePackage][p.ID] = p
	}
	return nil
}
func (r *Registry) loadBuiltin() {
	p := &Plugin{ID: "rclone", Name: "rclone", Version: "builtin", Description: "Google Drive/облака через rclone", Kind: "rclone", Builtin: true, Source: SourceBuiltin}
	if _, err := exec.LookPath("rclone"); err == nil {
		p.Available = true
	}
	r.sources[SourceBuiltin][p.ID] = p
}
func (r *Registry) effectiveLocked(id string) (*Plugin, bool) {
	for _, source := range []PluginSource{SourcePackage, SourceLegacy, SourceBuiltin} {
		if p, ok := r.sources[source][id]; ok {
			copy := *p
			copy.Enabled = r.enabledLocked(id, source)
			return &copy, true
		}
	}
	return nil, false
}
func (r *Registry) enabledLocked(id string, source PluginSource) bool {
	switch source {
	case SourceLegacy:
		return r.legacyEnabled[id]
	case SourcePackage:
		return r.packageEnabled[id]
	case SourceBuiltin:
		return r.builtinEnabled[id]
	}
	return false
}
func (r *Registry) List() []*Plugin {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := map[string]bool{}
	for _, items := range r.sources {
		for id := range items {
			ids[id] = true
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	out := make([]*Plugin, 0, len(ordered))
	for _, id := range ordered {
		if p, ok := r.effectiveLocked(id); ok {
			out = append(out, p)
		}
	}
	return out
}
func (r *Registry) LookupSource(id string, source PluginSource) (*Plugin, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validSource(source) {
		return nil, false
	}
	p, ok := r.sources[source][id]
	if !ok {
		return nil, false
	}
	copy := *p
	copy.Enabled = r.enabledLocked(id, source)
	return &copy, true
}
func validSource(s PluginSource) bool {
	return s == SourceLegacy || s == SourcePackage || s == SourceBuiltin
}
func (r *Registry) SetEnabled(id string, on bool) error {
	p, ok := r.effectiveSource(id)
	if !ok {
		return errors.New("plugin not found")
	}
	return r.SetSourceEnabled(id, p, on)
}
func (r *Registry) effectiveSource(id string) (PluginSource, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range []PluginSource{SourcePackage, SourceLegacy, SourceBuiltin} {
		if _, ok := r.sources[s][id]; ok {
			return s, true
		}
	}
	return "", false
}
func (r *Registry) SetSourceEnabled(id string, source PluginSource, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validSource(source) {
		return errors.New("invalid plugin source")
	}
	p, ok := r.sources[source][id]
	if !ok {
		return errors.New("plugin source not found")
	}
	if on && !p.Available {
		return errors.New("plugin not available (binary missing)")
	}
	switch source {
	case SourceLegacy:
		r.legacyEnabled[id] = on
	case SourcePackage:
		r.packageEnabled[id] = on
	case SourceBuiltin:
		r.builtinEnabled[id] = on
	}
	p.Enabled = on
	return r.saveLocked()
}
func (r *Registry) RemovePackage(id string, confirmedLegacyFallback bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[SourcePackage][id]; !ok {
		return errors.New("package plugin not found")
	}
	if _, ok := r.sources[SourceLegacy][id]; ok && r.legacyEnabled[id] && !confirmedLegacyFallback {
		return errors.New("confirmed legacy fallback required")
	}
	delete(r.sources[SourcePackage], id)
	delete(r.packageEnabled, id)
	if legacy, ok := r.sources[SourceLegacy][id]; ok {
		legacy.Enabled = r.legacyEnabled[id]
	}
	if err := r.saveLocked(); err != nil {
		return err
	}
	return nil
}
func (r *Registry) saveLocked() error {
	st := state{Version: 2, Enabled: boolIDs(r.legacyEnabled), PackageEnabled: boolIDs(r.packageEnabled), BuiltinEnabled: boolIDs(r.builtinEnabled)}
	sort.Strings(st.Enabled)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(r.dir, "plugins-state.json")
	tmp := p + ".tmp"
	if err = os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
func boolIDs(in map[string]bool) []string {
	out := []string{}
	for id, on := range in {
		if on {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
func (r *Registry) RcloneSync(ctx context.Context, src, dst string) (string, error) {
	if src == "" || dst == "" {
		return "", errors.New("src and dst required")
	}
	if strings.HasPrefix(src, "-") || strings.HasPrefix(dst, "-") {
		return "", errors.New("path must not start with '-'")
	}
	path, err := exec.LookPath("rclone")
	if err != nil {
		return "", errors.New("rclone not installed")
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, "sync", src, dst, "--stats", "0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type RcloneStatus struct {
	Installed bool     `json:"installed"`
	Version   string   `json:"version,omitempty"`
	Remotes   []string `json:"remotes,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func (r *Registry) RcloneStatus() RcloneStatus {
	path, err := exec.LookPath("rclone")
	if err != nil {
		return RcloneStatus{Installed: false}
	}
	st := RcloneStatus{Installed: true}
	if out, err := exec.Command(path, "version").Output(); err == nil {
		if line := strings.SplitN(string(out), "\n", 2)[0]; line != "" {
			st.Version = strings.TrimSpace(line)
		}
	} else {
		st.Error = err.Error()
	}
	if out, err := exec.Command(path, "listremotes").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				st.Remotes = append(st.Remotes, strings.TrimSuffix(line, ":"))
			}
		}
	}
	return st
}

func ValidatePackagePaths(root string, m Manifest) error {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return err
	}
	paths := append([]string(nil), m.Paths()...)
	for _, rel := range paths {
		if err := validateRelativePath(rel); err != nil {
			return err
		}
		candidate := filepath.Join(canonical, filepath.FromSlash(rel))
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("package path %q unavailable: %w", rel, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil || !withinRoot(canonical, resolved) {
			return fmt.Errorf("package path %q escapes package root", rel)
		}
	}
	return nil
}
func (m Manifest) Paths() []string {
	out := []string{}
	if m.Executable != "" {
		out = append(out, m.Executable)
	}
	for _, p := range m.Pages {
		out = append(out, p.Path)
	}
	for _, p := range m.Menu {
		if p.Path != "" {
			out = append(out, p.Path)
		}
	}
	return out
}
func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
