package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/backup"
	"github.com/tommilevs/Mounvix/server/internal/feeds"
	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"github.com/tommilevs/Mounvix/server/internal/share"
	"github.com/tommilevs/Mounvix/server/internal/version"
	"golang.org/x/net/webdav"
)

// App — HTTP-приложение Mounvix (панель + WebDAV + API).
type App struct {
	cfgMu      sync.RWMutex
	cfg        Config
	cfgPath    string
	store      *Store
	mux        *http.ServeMux
	tmpl       *template.Template
	locks      webdav.LockSystem
	shares     *share.Manager
	backups    *backup.Manager
	feeds      *feeds.Manager
	regs       *RegStore
	pluginReg  *plugins.Registry
	jobStore   *plugins.JobStore
	jobManager *plugins.JobManager
	cancel     context.CancelFunc
	background sync.WaitGroup
	started    time.Time
}

func (a *App) configSnapshot() Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

func New(cfg Config, cfgPath string) (*App, error) {
	if err := validatePublicBasePath(cfg.PublicBasePath); err != nil {
		return nil, err
	}
	if err := validatePublicScheme(cfg.PublicScheme); err != nil {
		return nil, err
	}
	st, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	sh, err := share.New(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	bk, err := backup.New(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	rg, err := NewRegStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	pl, err := plugins.NewRegistry(cfg.DataDir, pluginDirForConfig(cfg, cfgPath, cfg.DataDir), version.Version, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	feedManager, err := feeds.New(cfg.DataDir, cfg.Feeds)
	if err != nil {
		return nil, err
	}
	appCtx, cancel := context.WithCancel(context.Background())
	jobStore, err := plugins.NewJobStore(cfg.DataDir)
	if err != nil {
		cancel()
		return nil, err
	}
	runtime := plugins.NewRuntime(cfg.DataDir, pl)
	jobManager, err := plugins.NewJobManager(jobStore,
		func(ctx context.Context, job plugins.JobRecord) (string, error) {
			var request struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(job.Payload, &request); err != nil {
				return "", err
			}
			if request.Method != "plugin.runJob" {
				return "", errors.New("unsupported plugin job method")
			}
			if _, err := runtime.Call(ctx, job.PluginID, request.Method, request.Params); err != nil {
				return "plugin job failed", err
			}
			return "plugin job completed", nil
		},
		func(ctx context.Context, job plugins.JobRecord) (string, error) {
			var request struct {
				Src string `json:"src"`
				Dst string `json:"dst"`
			}
			if err := json.Unmarshal(job.Payload, &request); err != nil {
				return "", err
			}
			if _, err := pl.RcloneSync(ctx, request.Src, request.Dst); err != nil {
				return "rclone job failed", err
			}
			return "rclone job completed", nil
		},
		func(context.Context, plugins.JobRecord) (string, error) {
			return "", errors.New("package job executor is not configured")
		},
	)
	if err != nil {
		cancel()
		return nil, err
	}
	for _, p := range pl.List() {
		if !p.Enabled {
			if err := jobManager.DisablePlugin(context.Background(), p.ID); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, store: st, shares: sh, backups: bk, feeds: feedManager, regs: rg, pluginReg: pl, jobStore: jobStore, jobManager: jobManager, cancel: cancel, started: time.Now(), locks: webdav.NewMemLS()}
	if err := jobManager.Start(appCtx); err != nil {
		cancel()
		return nil, err
	}
	a.tmpl = mustTemplates(a)
	a.mux = http.NewServeMux()
	a.routes()
	a.background.Add(2)
	go func() { defer a.background.Done(); a.runFeedScheduler(appCtx) }()
	go func() { defer a.background.Done(); a.runPluginScheduler(appCtx) }()
	return a, nil
}

// Close stops background schedulers and cancels active child jobs.
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	if a.cancel != nil {
		a.cancel()
	}
	var closeErr error
	if a.jobManager != nil {
		closeErr = a.jobManager.Close()
	}
	a.background.Wait()
	return closeErr
}

func (a *App) runFeedScheduler(ctx context.Context) {
	if err := a.runFeedSchedulerOnce(ctx, time.Now()); err != nil {
		log.Printf("mounvix feeds: scheduler run: %v", err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := a.runFeedSchedulerOnce(ctx, now); err != nil {
				log.Printf("mounvix feeds: scheduler run: %v", err)
			}
		}
	}
}

func (a *App) runFeedSchedulerOnce(ctx context.Context, now time.Time) error {
	cfg := a.configSnapshot()
	roots := make([]feeds.Root, 0, len(cfg.Roots))
	for _, root := range cfg.Roots {
		roots = append(roots, feeds.Root{Name: root.Name, Path: root.Path})
	}
	return a.feeds.RunDue(ctx, now, roots)
}

func (a *App) runPluginScheduler(ctx context.Context) {
	if err := a.runPluginSchedulerOnce(ctx, time.Now()); err != nil {
		log.Printf("mounvix plugins: scheduler run: %v", err)
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := a.runPluginSchedulerOnce(ctx, now); err != nil {
				log.Printf("mounvix plugins: scheduler run: %v", err)
			}
		}
	}
}

func (a *App) runPluginSchedulerOnce(ctx context.Context, now time.Time) error {
	if ctx == nil {
		return errors.New("plugin scheduler context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	slotTime := now.UTC().Truncate(time.Minute)
	slot := slotTime.Format(time.RFC3339)
	var schedulerErr error
	for _, plugin := range a.pluginReg.List() {
		if !plugin.Enabled || !plugin.Available || plugin.Manifest == nil || !plugin.Manifest.Capabilities.Schedule {
			continue
		}
		for _, job := range plugin.Manifest.Jobs {
			schedule, err := plugins.ParseSchedule(job.Schedule)
			if err != nil {
				schedulerErr = errors.Join(schedulerErr, fmt.Errorf("plugin %s job %s schedule: %w", plugin.ID, job.ID, err))
				continue
			}
			if !schedule.Matches(slotTime) {
				continue
			}
			params, err := json.Marshal(map[string]string{"job_id": job.ID})
			if err != nil {
				schedulerErr = errors.Join(schedulerErr, fmt.Errorf("encode plugin %s job %s: %w", plugin.ID, job.ID, err))
				continue
			}
			payload, err := json.Marshal(map[string]any{"method": "plugin.runJob", "params": params})
			if err != nil {
				schedulerErr = errors.Join(schedulerErr, fmt.Errorf("encode plugin %s job %s payload: %w", plugin.ID, job.ID, err))
				continue
			}
			_, err = a.jobManager.Enqueue(ctx, plugins.JobRequest{ActorID: "system", Kind: "plugin", PluginID: plugin.ID, JobID: job.ID, Operation: "scheduled", ScheduledSlot: slot, Payload: payload})
			if err != nil && !errors.Is(err, plugins.ErrDuplicateScheduleSlot) {
				schedulerErr = errors.Join(schedulerErr, fmt.Errorf("enqueue plugin %s job %s: %w", plugin.ID, job.ID, err))
			}
		}
	}
	return schedulerErr
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

func (a *App) routes() {
	a.mux.HandleFunc("/health", a.handleHealth)
	a.mux.HandleFunc("/style.css", a.handleStyle)
	a.mux.HandleFunc("/theme.js", a.handleThemeScript)
	a.mux.HandleFunc("/login", a.handleLogin)
	a.mux.HandleFunc("/register", a.handleRegister)
	a.mux.HandleFunc("/registrations", a.handleRegistrations)
	a.mux.HandleFunc("/plugins", a.requireAuth(a.handlePluginsPage))
	a.mux.HandleFunc("/api/v1/plugins", a.handlePluginsAPI)
	a.mux.HandleFunc("/api/v1/session", a.handleAPISession)
	a.mux.HandleFunc("/api/v1/feeds", a.handleFeedsAPI)
	a.mux.HandleFunc("/api/v1/feeds/", a.handleFeedReadAPI)
	a.mux.HandleFunc("/ai", a.requireAuth(a.handleAIPage))
	a.mux.HandleFunc("/api/v1/ai/status", a.handleAIStatusAPI)
	a.mux.HandleFunc("/api/v1/ai/describe", a.handleAIDescribeAPI)
	a.mux.HandleFunc("/feed/", a.handleFeed)
	a.mux.HandleFunc("/logout", a.handleLogout)
	a.mux.HandleFunc("/", a.requireAuth(a.handleDashboard))
	a.mux.HandleFunc("/folders", a.requireAuth(a.handleFolders))
	a.mux.HandleFunc("/users", a.requireAuth(a.handleUsers))
	a.mux.HandleFunc("/settings", a.requireAuth(a.handleSettings))
	a.mux.HandleFunc("/logs", a.requireAuth(a.handleLogs))
	a.mux.HandleFunc("/stats", a.requireAuth(a.handleStats))
	a.mux.HandleFunc("/dav/", a.handleDAV)
	a.mux.HandleFunc("/media", a.requireAuth(a.handleMediaPage))
	a.mux.HandleFunc("/api/v1/thumbs", a.handleThumb)
	a.mux.HandleFunc("/api/v1/file", a.handleFileStream)
	a.mux.HandleFunc("/shares", a.requireAuth(a.handleSharesPage))
	a.mux.HandleFunc("/api/v1/shares", a.handleSharesAPI)
	a.mux.HandleFunc("/api/v1/shares/", a.handleShareRevokeAPI)
	a.mux.HandleFunc("/s/", a.handlePublicShare)
	a.mux.HandleFunc("/backup", a.requireAuth(a.handleBackupPage))
	a.mux.HandleFunc("/api/v1/backup/jobs", a.handleBackupJobsAPI)
	a.mux.HandleFunc("/api/v1/backup/run", a.handleBackupRunAPI)
	a.mux.HandleFunc("/api/v1/backup/report", a.handleBackupReportAPI)
	a.mux.HandleFunc("/api/v1/backup/restore", a.handleBackupRestoreAPI)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok":       true,
		"version":  version.Version,
		"uptime_s": int(time.Since(a.started).Seconds()),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := jsonMarshal(v)
	_, _ = w.Write(b)
}

func (a *App) findRoot(name string) (Root, bool) {
	cfg := a.configSnapshot()
	for _, rt := range cfg.Roots {
		if rt.Name == name {
			return rt, true
		}
	}
	return Root{}, false
}

// userPerm возвращает права пользователя на корень: "" | "r" | "rw".
func (a *App) userPerm(u *User, root string) string {
	if u == nil {
		return ""
	}
	if u.IsAdmin() {
		return "rw"
	}
	if p, ok := u.ACL[root]; ok && (p == "r" || p == "rw") {
		return p
	}
	return ""
}

func (a *App) cookieUser(r *http.Request) (*User, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, false
	}
	name, ok := a.store.SessionUser(c.Value)
	if !ok {
		return nil, false
	}
	u := a.store.Find(name)
	if u == nil || u.Disabled {
		return nil, false
	}
	return u, true
}

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.cookieUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *App) basicUser(r *http.Request) (*User, bool) {
	name, pass, ok := r.BasicAuth()
	if !ok {
		return nil, false
	}
	return a.store.Verify(name, pass)
}

var safeMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodOptions: true, "PROPFIND": true,
}

func isSafeMethod(m string) bool { return safeMethods[strings.ToUpper(m)] }
