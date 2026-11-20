package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
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

type packageRemovalManager interface {
	CanRemove() bool
	Remove(context.Context, string) error
}

// App — HTTP-приложение Mounvix (панель + WebDAV + API).
type App struct {
	cfgMu             sync.RWMutex
	cfg               Config
	cfgPath           string
	store             *Store
	mux               *http.ServeMux
	tmpl              *template.Template
	locks             webdav.LockSystem
	shares            *share.Manager
	backups           *backup.Manager
	feeds             *feeds.Manager
	regs              *RegStore
	pluginReg         *plugins.Registry
	jobStore          *plugins.JobStore
	jobManager        *plugins.JobManager
	runtime           *plugins.Runtime
	hookStore         *plugins.HookStore
	notifications     *plugins.NotificationStore
	rcloneAdapter     plugins.RcloneAdapter
	packageInstaller  packageRemovalManager
	pluginOperationMu sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	background        sync.WaitGroup
	started           time.Time
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
	hookStore, hookErr := plugins.NewHookStore(cfg.DataDir)
	if hookErr != nil {
		log.Printf("mounvix plugins: hooks disabled: %v", hookErr)
	}
	notifications, notificationErr := plugins.NewNotificationStore(cfg.DataDir)
	if notificationErr != nil {
		log.Printf("mounvix plugins: notifications disabled: %v", notificationErr)
	}
	runtime := plugins.NewRuntime(cfg.DataDir, pl)
	rcloneAdapter := plugins.RcloneAdapter{SourceRoot: cfg.RcloneSourceRoot, LogStore: plugins.NewLogStore(cfg.DataDir)}
	packageInstaller := plugins.NewPackageInstaller(cfgPath)
	var app, a *App
	var jobManager *plugins.JobManager
	jobManager, err = plugins.NewJobManager(jobStore,
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
			if _, err := runtime.CallWithRunID(ctx, job.PluginID, request.Method, request.Params, job.RunID); err != nil {
				return "plugin job failed", err
			}
			return "plugin job completed", nil
		},
		func(ctx context.Context, job plugins.JobRecord) (string, error) {
			var request plugins.RcloneJobRequest
			if err := json.Unmarshal(job.Payload, &request); err != nil {
				return "", err
			}
			if app == nil {
				return "", errors.New("rclone jobs are unavailable")
			}
			if _, err := app.rcloneAdapter.RunSyncWithRunID(ctx, request, job.RunID); err != nil {
				return "rclone job failed", err
			}
			return "rclone job completed", nil
		},
		func(ctx context.Context, job plugins.JobRecord) (string, error) {
			if job.Operation != "remove" {
				return "", errors.New("unsupported package job operation")
			}
			pluginID := job.PluginID
			if pluginID == "" {
				pluginID = job.JobID
			}
			if app == nil {
				return "", errors.New("package removal is unavailable")
			}
			app.pluginOperationMu.Lock()
			defer app.pluginOperationMu.Unlock()
			packagePlugin, exists := app.pluginReg.LookupSource(pluginID, plugins.SourcePackage)
			if !exists {
				return "package already absent", nil
			}
			var request struct {
				ConfirmedLegacyFallback bool `json:"confirmed_legacy_fallback"`
			}
			if err := json.Unmarshal(job.Payload, &request); err != nil {
				return "", errors.New("invalid package removal request")
			}
			if packagePlugin.Enabled {
				return "", errors.New("disable package source before removal")
			}
			legacy, hasLegacy := app.pluginReg.LookupSource(pluginID, plugins.SourceLegacy)
			if hasLegacy && legacy.Enabled && !request.ConfirmedLegacyFallback {
				return "", errors.New("legacy fallback confirmation required")
			}
			if app.packageInstaller == nil || !app.packageInstaller.CanRemove() {
				return "", errors.New("package removal is unavailable on this platform")
			}
			if err := jobManager.DisablePlugin(ctx, pluginID); err != nil {
				return "", fmt.Errorf("stop plugin jobs before package removal: %w", err)
			}
			if err := app.packageInstaller.Remove(ctx, pluginID); err != nil {
				if packagePlugin.Enabled {
					jobManager.EnablePlugin(pluginID)
				}
				return "", fmt.Errorf("remove package: %w", err)
			}
			if err := app.pluginReg.ForgetPackageAfterExternalRemoval(pluginID); err != nil {
				return "package removed but registry state could not be updated", err
			}
			if hasLegacy && legacy.Enabled {
				jobManager.EnablePlugin(pluginID)
			}
			return "package removed", nil
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
	a = &App{cfg: cfg, cfgPath: cfgPath, store: st, shares: sh, backups: bk, feeds: feedManager, regs: rg, pluginReg: pl, jobStore: jobStore, jobManager: jobManager, runtime: runtime, hookStore: hookStore, notifications: notifications, rcloneAdapter: rcloneAdapter, packageInstaller: packageInstaller, ctx: appCtx, cancel: cancel, started: time.Now(), locks: webdav.NewMemLS()}
	app = a
	bk.SetReportCallback(func(report backup.Report) {
		a.publishHook(plugins.HookEvent{EventID: newHookEventID("backup.done"), Kind: "backup.done", Payload: map[string]any{"job_id": report.JobID, "files": report.Files, "bytes": report.Bytes, "skipped": report.Skipped, "error_count": len(report.Errors), "status": "completed"}})
	})
	jobManager.SetIntentRecorder(func(record plugins.JobRecord) error {
		action, pluginID := auditedJobOperation(record)
		if action == "" || pluginID == "" {
			return nil
		}
		if err := a.pluginReg.RecordAudit(plugins.AuditEvent{Actor: record.ActorID, Phase: "intent", Action: action, PluginID: pluginID, RunID: record.RunID, Outcome: "queued"}); err != nil {
			log.Printf("mounvix plugins: record job intent %s: %v", record.RunID, err)
			return fmt.Errorf("record job intent %s: %w", record.RunID, err)
		}
		return nil
	})
	jobManager.SetOutcomePublisher(func(record plugins.JobRecord) {
		if err := a.publishJobOutcome(record); err != nil {
			log.Printf("mounvix plugins: publish job outcome %s: %v", record.RunID, err)
		}
	})
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
	schedulerErr := a.reconcileJobOutcomes(ctx)
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

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	basePath := a.configSnapshot().PublicBasePath
	if basePath != "" {
		if path == basePath {
			path = "/"
		} else if strings.HasPrefix(path, basePath+"/") {
			path = strings.TrimPrefix(path, basePath)
		} else {
			http.NotFound(w, r)
			return
		}
		request := r.Clone(r.Context())
		requestURL := *r.URL
		requestURL.Path = path
		requestURL.RawPath = ""
		request.URL = &requestURL
		r = request
	}
	if !a.originRequestAllowed(r, path) {
		if strings.HasPrefix(path, "/api/v1/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		http.Error(w, "same-origin request required", http.StatusForbidden)
		return
	}
	a.mux.ServeHTTP(w, r)
}

func (a *App) routes() {
	a.mux.HandleFunc("/health", a.handleHealth)
	a.mux.HandleFunc("/style.css", a.handleStyle)
	a.mux.HandleFunc("/theme.js", a.handleThemeScript)
	a.mux.HandleFunc("/notifications.js", a.handleNotificationsScript)
	a.mux.HandleFunc("/plugins.js", a.handlePluginsScript)
	a.mux.HandleFunc("/login", a.handleLogin)
	a.mux.HandleFunc("/register", a.handleRegister)
	a.mux.HandleFunc("/registrations", a.handleRegistrations)
	a.mux.HandleFunc("/plugins", a.requireAuth(a.handlePluginsPage))
	a.mux.HandleFunc("/plugins/", a.requireAuth(a.handlePluginPage))
	a.mux.HandleFunc("/notifications", a.requireAuth(a.handleNotificationsPage))
	a.mux.HandleFunc("/api/v1/plugins", a.handlePluginsAPI)
	a.mux.HandleFunc("/api/v1/plugins/", a.handlePluginSourceAPI)
	a.mux.HandleFunc("/api/v1/rclone-remotes", a.handleRcloneRemotesAPI)
	a.mux.HandleFunc("/api/v1/rclone-jobs", a.handleRcloneJobsAPI)
	a.mux.HandleFunc("/api/v1/plugin-catalog", a.handlePluginCatalogAPI)
	a.mux.HandleFunc("/api/v1/plugin-installations", a.handlePluginInstallationsAPI)
	a.mux.HandleFunc("/api/v1/plugin-installations/", a.handlePluginInstallationsAPI)
	a.mux.HandleFunc("/api/v1/jobs", a.handlePluginJobsAPI)
	a.mux.HandleFunc("/api/v1/jobs/", a.handlePluginJobDetailAPI)
	a.mux.HandleFunc("/api/v1/notifications", a.handleNotificationsAPI)
	a.mux.HandleFunc("/api/v1/notifications/ws", a.handleNotificationsWebSocket)
	a.mux.HandleFunc("/api/v1/notifications/", a.handleNotificationReadAPI)
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
			login := a.routePath("/login")
			next := a.safeReturnRoute(a.routePath(r.URL.Path))
			http.Redirect(w, r, login+"?next="+url.QueryEscape(next), http.StatusSeeOther)
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
