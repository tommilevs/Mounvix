// Package plugins — реестр плагинов Mounvix (манифесты + встроенный rclone).
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Plugin — запись реестра плагинов.
type Plugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"` // "rclone" | "process"
	Command     string `json:"command,omitempty"`
	Builtin     bool   `json:"builtin"`
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"`
}

type state struct {
	Enabled []string `json:"enabled"`
}

// Registry — реестр: манифесты plugins/*.json + состояние plugins-state.json.
type Registry struct {
	mu    sync.Mutex
	dir   string
	items map[string]*Plugin
	order []string
}

func New(dir string) (*Registry, error) {
	r := &Registry{dir: dir, items: map[string]*Plugin{}}
	mroot := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(mroot, 0o755); err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(mroot); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(mroot, e.Name()))
			if err != nil {
				continue
			}
			var p Plugin
			if err := json.Unmarshal(b, &p); err != nil || p.ID == "" {
				continue
			}
			p.Builtin = false
			p.Available = true
			r.add(&p)
		}
	}
	// Встроенный плагин rclone (доступность — наличие бинарника).
	rclone := &Plugin{ID: "rclone", Name: "rclone", Version: "builtin", Description: "Google Drive/облака через rclone", Kind: "rclone", Builtin: true}
	if _, err := exec.LookPath("rclone"); err == nil {
		rclone.Available = true
	}
	r.add(rclone)
	// Состояние enabled.
	if b, err := os.ReadFile(filepath.Join(dir, "plugins-state.json")); err == nil {
		var st state
		if json.Unmarshal(b, &st) == nil {
			for _, id := range st.Enabled {
				if p, ok := r.items[id]; ok {
					p.Enabled = true
				}
			}
		}
	}
	return r, nil
}

func (r *Registry) add(p *Plugin) {
	if _, ok := r.items[p.ID]; !ok {
		r.order = append(r.order, p.ID)
	}
	r.items[p.ID] = p
}

func (r *Registry) List() []*Plugin {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Plugin, 0, len(r.items))
	for _, p := range r.items {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) SetEnabled(id string, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.items[id]
	if !ok {
		return errors.New("plugin not found")
	}
	if on && !p.Available {
		return errors.New("plugin not available (binary missing)")
	}
	p.Enabled = on
	return r.saveLocked()
}

func (r *Registry) saveLocked() error {
	st := state{}
	for id, p := range r.items {
		if p.Enabled {
			st.Enabled = append(st.Enabled, id)
		}
	}
	sort.Strings(st.Enabled)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(r.dir, "plugins-state.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// RcloneSync выполняет rclone sync src → dst (без shell, аргументы отдельно). Только для админа.
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

// RcloneStatus — версия и список удалённых хранилищ rclone.
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
