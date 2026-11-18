package app

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/backup"
	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"github.com/tommilevs/Mounvix/server/internal/share"
	"github.com/tommilevs/Mounvix/server/internal/version"
	"golang.org/x/net/webdav"
)

// App — HTTP-приложение Mounvix (панель + WebDAV + API).
type App struct {
	cfg       Config
	cfgPath   string
	store     *Store
	mux       *http.ServeMux
	tmpl      *template.Template
	locks     webdav.LockSystem
	shares    *share.Manager
	backups   *backup.Manager
	regs      *RegStore
	pluginReg *plugins.Registry
	started   time.Time
}

func New(cfg Config, cfgPath string) (*App, error) {
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
	pl, err := plugins.New(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, store: st, shares: sh, backups: bk, regs: rg, pluginReg: pl, started: time.Now(), locks: webdav.NewMemLS()}
	a.tmpl = mustTemplates(a)
	a.mux = http.NewServeMux()
	a.routes()
	return a, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

func (a *App) routes() {
	a.mux.HandleFunc("/health", a.handleHealth)
	a.mux.HandleFunc("/style.css", a.handleStyle)
	a.mux.HandleFunc("/login", a.handleLogin)
	a.mux.HandleFunc("/register", a.handleRegister)
	a.mux.HandleFunc("/registrations", a.handleRegistrations)
	a.mux.HandleFunc("/plugins", a.requireAuth(a.handlePluginsPage))
	a.mux.HandleFunc("/api/v1/plugins", a.handlePluginsAPI)
	a.mux.HandleFunc("/ai", a.requireAuth(a.handleAIPage))
	a.mux.HandleFunc("/api/v1/ai/status", a.handleAIStatusAPI)
	a.mux.HandleFunc("/api/v1/ai/describe", a.handleAIDescribeAPI)
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
	for _, rt := range a.cfg.Roots {
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
