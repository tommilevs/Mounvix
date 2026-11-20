// Package backup — инкрементальное резервное копирование папок на локальные диски/USB и WebDAV.
package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Target — назначение бэкапа: локальная папка (в т.ч. USB) или WebDAV.
type Target struct {
	Type     string `json:"type"` // local | webdav
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

// Job — задание бэкапа.
type Job struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	Targets       []Target  `json:"targets"`
	CreatedAt     time.Time `json:"created_at"`
	IntervalHours int       `json:"interval_hours,omitempty"`
	LastRun       time.Time `json:"last_run"`
}

// Report — результат запуска задания.
type Report struct {
	JobID    string    `json:"job_id"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Files    int       `json:"files"`
	Bytes    int64     `json:"bytes"`
	Skipped  int       `json:"skipped"`
	Errors   []string  `json:"errors"`
}

type Manager struct {
	mu         sync.Mutex
	dir        string
	path       string
	reportPath string
	jobs       []*Job
	last       map[string]Report
	onReport   func(Report)
}

func New(dir string) (*Manager, error) {
	m := &Manager{dir: dir, path: filepath.Join(dir, "backup-jobs.json"), reportPath: filepath.Join(dir, "backup-reports.json"), last: map[string]Report{}}
	if b, err := os.ReadFile(m.path); err == nil {
		if err := json.Unmarshal(b, &m.jobs); err != nil {
			return nil, fmt.Errorf("load backup jobs: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if b, err := os.ReadFile(m.reportPath); err == nil {
		if err := json.Unmarshal(b, &m.last); err != nil {
			return nil, fmt.Errorf("load backup reports: %w", err)
		}
		if m.last == nil {
			m.last = map[string]Report{}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read backup reports: %w", err)
	}
	m.Start(context.Background())
	return m, nil
}

// SetReportCallback registers a callback invoked after a terminal report is durable.
func (m *Manager) SetReportCallback(callback func(Report)) {
	m.mu.Lock()
	m.onReport = callback
	m.mu.Unlock()
}

// Restore восстанавливает файлы из локальной цели бэкапа в dest (с перезаписью).
func (m *Manager) Restore(ctx context.Context, id, dest string) (Report, error) {
	job := m.Get(id)
	if job == nil {
		return Report{}, errors.New("job not found")
	}
	if dest == "" {
		return Report{}, errors.New("dest required")
	}
	var local, dav *Target
	for i := range job.Targets {
		if job.Targets[i].Type == "local" && local == nil {
			local = &job.Targets[i]
		}
		if job.Targets[i].Type == "webdav" && dav == nil {
			dav = &job.Targets[i]
		}
	}
	if local == nil {
		if dav == nil {
			return Report{}, errors.New("restore target required")
		}
		rep := Report{JobID: id, Started: time.Now()}
		err := m.restoreWebDAV(ctx, job, *dav, dest, &rep)
		rep.Finished = time.Now()
		m.mu.Lock()
		m.last[id+"-restore"] = rep
		m.mu.Unlock()
		return rep, err
	}
	rep := Report{JobID: id, Started: time.Now()}
	walkErr := filepath.WalkDir(local.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		rel, err := filepath.Rel(local.Path, p)
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		if err := copyLocal(p, filepath.Join(dest, rel), info); err != nil {
			rep.Errors = append(rep.Errors, rel+": "+err.Error())
			return nil
		}
		rep.Files++
		rep.Bytes += info.Size()
		return nil
	})
	if walkErr != nil {
		return rep, walkErr
	}
	rep.Finished = time.Now()
	m.mu.Lock()
	m.last[id+"-restore"] = rep
	m.mu.Unlock()
	return rep, nil
}
func (m *Manager) List() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, len(m.jobs))
	copy(out, m.jobs)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *Manager) Get(id string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id {
			return j
		}
	}
	return nil
}

func (m *Manager) Create(name, source string, targets []Target, intervalHours int) (*Job, error) {
	if name == "" || source == "" {
		return nil, errors.New("name and source required")
	}
	if len(targets) == 0 {
		return nil, errors.New("targets required")
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	j := &Job{ID: hex.EncodeToString(raw), Name: name, Source: source, Targets: targets, CreatedAt: time.Now(), IntervalHours: intervalHours}
	if intervalHours > 0 {
		j.LastRun = time.Now()
	}
	m.mu.Lock()
	m.jobs = append(m.jobs, j)
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (m *Manager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.jobs {
		if j.ID == id {
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			_ = m.saveLocked()
			delete(m.last, id)
			return true
		}
	}
	return false
}

func (m *Manager) LastReport(id string) (Report, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.last[id]
	return r, ok
}

// RunDue запускает задания, у которых истёк интервал (для планировщика).
func (m *Manager) RunDue(ctx context.Context, now time.Time) []Report {
	var due []*Job
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.IntervalHours <= 0 {
			continue
		}
		if j.LastRun.Add(time.Duration(j.IntervalHours) * time.Hour).After(now) {
			continue
		}
		due = append(due, j)
	}
	m.mu.Unlock()
	out := make([]Report, 0, len(due))
	for _, j := range due {
		rep, err := m.Run(ctx, j.ID)
		if err != nil {
			rep.Errors = append(rep.Errors, err.Error())
		}
		out = append(out, rep)
	}
	return out
}

// Start запускает фоновый планировщик (проверка раз в 30 секунд).
func (m *Manager) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				m.RunDue(ctx, now)
			}
		}
	}()
}

func (m *Manager) saveReportsLocked() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m.last, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.reportPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.reportPath)
}

func (m *Manager) saveLocked() error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m.jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// Run выполняет инкрементальный бэкап: копируются файлы с mtime новее прошлого запуска.
func (m *Manager) Run(ctx context.Context, id string) (Report, error) {
	job := m.Get(id)
	if job == nil {
		return Report{}, errors.New("job not found")
	}
	rep := Report{JobID: id, Started: time.Now()}
	cutoff := job.LastRun
	client := &http.Client{Timeout: 90 * time.Second}

	walkErr := filepath.WalkDir(job.Source, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		if !cutoff.IsZero() && !info.ModTime().After(cutoff) {
			rep.Skipped++
			return nil
		}
		rel, err := filepath.Rel(job.Source, p)
		if err != nil {
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		ok := false
		for _, t := range job.Targets {
			if err := copyToTarget(ctx, client, p, rel, t, info); err != nil {
				rep.Errors = append(rep.Errors, t.Type+" "+t.Path+t.URL+": "+err.Error())
			} else {
				ok = true
			}
		}
		if ok {
			rep.Files++
			rep.Bytes += info.Size()
		}
		return nil
	})
	if walkErr != nil {
		return rep, walkErr
	}
	rep.Finished = time.Now()
	m.mu.Lock()
	job.LastRun = rep.Finished
	m.last[id] = rep
	jobErr := m.saveLocked()
	reportErr := m.saveReportsLocked()
	callback := m.onReport
	m.mu.Unlock()
	if jobErr != nil {
		return rep, fmt.Errorf("persist backup job: %w", jobErr)
	}
	if reportErr != nil {
		return rep, fmt.Errorf("persist terminal backup report: %w", reportErr)
	}
	if callback != nil {
		callback(rep)
	}
	return rep, nil
}

func copyToTarget(ctx context.Context, client *http.Client, file, rel string, t Target, info os.FileInfo) error {
	switch t.Type {
	case "local":
		return copyLocal(file, filepath.Join(t.Path, rel), info)
	case "webdav":
		return putWebDAV(ctx, client, file, rel, t)
	default:
		return fmt.Errorf("unknown target type %q", t.Type)
	}
}

func copyLocal(src, dst string, info os.FileInfo) error {
	if st, err := os.Stat(dst); err == nil && st.Size() == info.Size() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func putWebDAV(ctx context.Context, client *http.Client, file, rel string, t Target) error {
	base, err := url.Parse(strings.TrimRight(t.URL, "/"))
	if err != nil {
		return err
	}
	escaped := strings.Split(filepath.ToSlash(rel), "/")
	for i, seg := range escaped {
		escaped[i] = url.PathEscape(seg)
	}
	basePath := strings.TrimRight(base.Path, "/")
	if basePath == "" {
		basePath = "/"
	}
	mkcol(ctx, client, base, basePath, t)
	dirs := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
	for _, d := range dirs {
		if d == "" || d == "." {
			continue
		}
		basePath = strings.TrimRight(basePath, "/") + "/" + url.PathEscape(d)
		mkcol(ctx, client, base, basePath, t)
	}
	u := *base
	u.Path = strings.TrimRight(basePath, "/") + "/" + strings.Join(escaped, "/")
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), f)
	if err != nil {
		return err
	}
	if st, err := f.Stat(); err == nil {
		req.ContentLength = st.Size()
	}
	applyAuth(req, t)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webdav PUT %d", resp.StatusCode)
	}
	return nil
}

func mkcol(ctx context.Context, client *http.Client, base *url.URL, p string, t Target) {
	u := *base
	u.Path = p
	req, err := http.NewRequestWithContext(ctx, "MKCOL", u.String(), nil)
	if err != nil {
		return
	}
	applyAuth(req, t)
	resp, err := client.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
func applyAuth(req *http.Request, t Target) {
	if t.User != "" {
		req.SetBasicAuth(t.User, t.Password)
	}
	req.Header.Set("User-Agent", "Mounvix-Backup")
}

// JoinURL — служебный помощник (экспортирован для тестов/страниц).
func JoinURL(base, rel string) string {
	b, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return base + "/" + rel
	}
	b.Path = path.Join(b.Path, rel)
	return b.String()
}

var _ = filepath.Separator
