package app

import (
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/version"
)

func stringsRU() map[string]string {
	return map[string]string{
		"login": "Вход", "username": "Логин", "password": "Пароль", "signin": "Войти",
		"nav_overview": "Обзор", "nav_folders": "Доступ к папкам", "nav_users": "Пользователи",
		"nav_shares": "Шары", "nav_media": "Медиа", "nav_backup": "Бэкап", "nav_regs": "Заявки", "nav_plugins": "Плагины", "nav_ai": "AI", "nav_settings": "Настройки", "nav_logs": "Логи", "nav_stats": "Статистика", "logout": "Выход",
		"overview": "Обзор", "version": "Версия", "uptime": "Аптайм", "roots": "Папки",
		"users": "Пользователи", "sessions": "Сессии", "audit": "Аудит",
		"folders": "Доступ к папкам", "name": "Имя", "path": "Путь", "readonly": "Только чтение",
		"your_perm":   "Ваши права",
		"create_user": "Создать пользователя", "new_password": "Новый пароль", "role": "Роль",
		"apply": "Применить", "reset": "Сбросить пароль", "disable": "Отключить", "enable": "Включить",
		"lang": "Язык", "save": "Сохранить", "settings": "Настройки", "logs": "Логи", "time": "Время", "action": "Действие",
		"info": "Информация", "stats": "Статистика", "total_users": "Пользователей",
		"total_roots": "Папок", "total_sessions": "Активных сессий", "audit_events": "Событий аудита",
		"settings_note": "Корни задаются в config.json. Здесь можно сменить язык и настроить ленту «Воспоминания».",
		"feeds_title":   "Воспоминания", "feeds_enable": "Включить автоматические подборки", "feeds_roots": "Корни для сканирования", "feeds_weekdays": "Дни недели", "feeds_times": "Время генерации (ЧЧ:ММ, через запятую)",
		"feeds_quiet_start": "Начало тихих часов", "feeds_quiet_end": "Конец тихих часов", "feeds_period": "Период поиска, дней", "feeds_max_items": "Медиа в карточке (макс. 8)", "feeds_retention": "Хранить карточки, дней",
		"weekday_mon": "Пн", "weekday_tue": "Вт", "weekday_wed": "Ср", "weekday_thu": "Чт", "weekday_fri": "Пт", "weekday_sat": "Сб", "weekday_sun": "Вс",
		"no_access": "Нет доступа", "acl": "Доступ к папкам (чтение/запись)", "create": "Создать",
		"theme": "Тема", "theme_dark": "Тёмная", "theme_light": "Светлая", "theme_system": "Система",
		"menu": "Меню", "nav_label": "Основная навигация", "skip_content": "Перейти к содержимому", "close_menu": "Закрыть меню",
	}
}

func stringsEN() map[string]string {
	return map[string]string{
		"login": "Login", "username": "Username", "password": "Password", "signin": "Sign in",
		"nav_overview": "Overview", "nav_folders": "Folders access", "nav_users": "Users",
		"nav_shares": "Shares", "nav_media": "Media", "nav_backup": "Backup", "nav_regs": "Requests", "nav_plugins": "Plugins", "nav_ai": "AI", "nav_settings": "Settings", "nav_logs": "Logs", "nav_stats": "Stats", "logout": "Log out",
		"overview": "Overview", "version": "Version", "uptime": "Uptime", "roots": "Folders",
		"users": "Users", "sessions": "Sessions", "audit": "Audit",
		"folders": "Folders access", "name": "Name", "path": "Path", "readonly": "Read-only",
		"your_perm":   "Your permission",
		"create_user": "Create user", "new_password": "New password", "role": "Role",
		"apply": "Apply", "reset": "Reset password", "disable": "Disable", "enable": "Enable",
		"lang": "Language", "save": "Save", "settings": "Settings", "logs": "Logs", "time": "Time", "action": "Action",
		"info": "Info", "stats": "Stats", "total_users": "Users", "total_roots": "Folders",
		"total_sessions": "Active sessions", "audit_events": "Audit events",
		"settings_note": "Roots are set in config.json. Here you can change the language and configure Memories.",
		"feeds_title":   "Memories", "feeds_enable": "Enable automatic collections", "feeds_roots": "Roots to scan", "feeds_weekdays": "Weekdays", "feeds_times": "Generation times (HH:MM, comma-separated)",
		"feeds_quiet_start": "Quiet hours start", "feeds_quiet_end": "Quiet hours end", "feeds_period": "Search period, days", "feeds_max_items": "Media per card (max 8)", "feeds_retention": "Keep cards, days",
		"weekday_mon": "Mon", "weekday_tue": "Tue", "weekday_wed": "Wed", "weekday_thu": "Thu", "weekday_fri": "Fri", "weekday_sat": "Sat", "weekday_sun": "Sun",
		"no_access": "No access", "acl": "Folder access (read/write)", "create": "Create",
		"theme": "Theme", "theme_dark": "Dark", "theme_light": "Light", "theme_system": "System",
		"menu": "Menu", "nav_label": "Primary navigation", "skip_content": "Skip to content", "close_menu": "Close menu",
	}
}

type permRoot struct {
	Name     string
	Path     string
	ReadOnly bool
	Perm     string
}

type view struct {
	Title      string
	User       *User
	Cfg        Config
	CfgPath    string
	Version    string
	Uptime     string
	Roots      []permRoot
	Users      []*User
	Audit      []AuditEntry
	RootsCount int
	UsersCount int
	Sessions   int
	AuditCount int
	Lang       string
}

func humanUptime(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Minute).String()
}

func (a *App) viewFor(r *http.Request) *view {
	u, _ := a.cookieUser(r)
	cfg := a.configSnapshot()
	return &view{
		User:    u,
		Cfg:     cfg,
		CfgPath: a.cfgPath,
		Version: version.Version,
		Uptime:  humanUptime(time.Since(a.started)),
		Lang:    cfg.Lang,
	}
}

func (a *App) render(w http.ResponseWriter, page string, v *view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, page, v); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	v := a.viewFor(r)
	v.Title = a.T("overview")
	cfg := a.configSnapshot()
	v.RootsCount = len(cfg.Roots)
	v.UsersCount = len(a.store.Users())
	v.Sessions = a.store.SessionCount()
	v.AuditCount = a.store.AuditCount()
	v.Audit = a.store.AuditTail(8)
	a.render(w, "dashboard.html", v)
}

func (a *App) handleFolders(w http.ResponseWriter, r *http.Request) {
	v := a.viewFor(r)
	v.Title = a.T("folders")
	cfg := a.configSnapshot()
	for _, rt := range cfg.Roots {
		perm := a.userPerm(v.User, rt.Name)
		if perm == "" && !v.User.IsAdmin() {
			continue
		}
		v.Roots = append(v.Roots, permRoot{Name: rt.Name, Path: rt.Path, ReadOnly: rt.ReadOnly, Perm: perm})
	}
	a.render(w, "folders.html", v)
}

func (a *App) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		admin, _ := a.cookieUser(r)
		switch r.PostFormValue("action") {
		case "create":
			name := r.PostFormValue("username")
			err := a.store.AddUser(name, r.PostFormValue("password"), r.PostFormValue("role"))
			if err != nil {
				a.store.Audit(admin.Name, "user_create_failed", name+": "+err.Error())
			} else {
				a.store.Audit(admin.Name, "user_create", name)
			}
		case "reset":
			name := r.PostFormValue("username")
			if err := a.store.SetPassword(name, r.PostFormValue("newpass")); err == nil {
				a.store.Audit(admin.Name, "user_reset", name)
			}
		case "toggle":
			name := r.PostFormValue("username")
			a.store.ToggleUser(name)
			a.store.Audit(admin.Name, "user_toggle", name)
		case "acl":
			name := r.PostFormValue("username")
			acl := map[string]string{}
			for k, vals := range r.PostForm {
				if !strings.HasPrefix(k, "acl__") || len(vals) == 0 {
					continue
				}
				parts := strings.Split(k, "__")
				if len(parts) != 3 {
					continue
				}
				perm := vals[0]
				if perm == "r" || perm == "rw" {
					acl[parts[2]] = perm
				}
			}
			a.store.SetACL(name, acl)
			a.store.Audit(admin.Name, "user_acl", name)
		}
		http.Redirect(w, r, a.routePath("/users"), http.StatusSeeOther)
		return
	}
	v := a.viewFor(r)
	v.Title = a.T("users")
	v.Users = a.store.Users()
	a.render(w, "users.html", v)
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		switch r.PostFormValue("action") {
		case "lang":
			if lang := r.PostFormValue("lang"); lang == "ru" || lang == "en" {
				a.cfgMu.Lock()
				a.cfg.Lang = lang
				saveErr := SaveConfig(a.cfgPath, a.cfg)
				a.cfgMu.Unlock()
				if saveErr != nil {
					a.store.Audit("admin", "settings_save_failed", saveErr.Error())
				}
				if u, ok := a.cookieUser(r); ok {
					a.store.Audit(u.Name, "settings_lang", lang)
				}
			}
		case "feeds":
			u, ok := a.cookieUser(r)
			if !ok || !u.IsAdmin() {
				http.Error(w, "admin required", http.StatusForbidden)
				return
			}
			current := a.configSnapshot()
			cfg, err := parseFeedConfig(r, current.Feeds, current.Roots)
			if err != nil {
				http.Error(w, "invalid memories feed settings: "+err.Error(), http.StatusBadRequest)
				return
			}
			a.cfgMu.Lock()
			next := a.cfg
			next.Feeds = cfg
			saveErr := SaveConfig(a.cfgPath, next)
			if saveErr == nil {
				a.cfg.Feeds = cfg
			}
			a.cfgMu.Unlock()
			if saveErr != nil {
				a.store.Audit(u.Name, "feed_settings_save_failed", saveErr.Error())
				http.Error(w, "could not save settings", http.StatusInternalServerError)
				return
			}
			a.feeds.SetConfig(cfg)
			a.store.Audit(u.Name, "feed_settings", "updated")
		}
		http.Redirect(w, r, a.routePath("/settings"), http.StatusSeeOther)
		return
	}
	v := a.viewFor(r)
	v.Title = a.T("settings")
	a.render(w, "settings.html", v)
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	v := a.viewFor(r)
	v.Title = a.T("logs")
	v.Audit = a.store.AuditTail(200)
	a.render(w, "logs.html", v)
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	v := a.viewFor(r)
	v.Title = a.T("stats")
	cfg := a.configSnapshot()
	v.RootsCount = len(cfg.Roots)
	v.UsersCount = len(a.store.Users())
	v.Sessions = a.store.SessionCount()
	v.AuditCount = a.store.AuditCount()
	a.render(w, "stats.html", v)
}

func hasInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mustTemplates(a *App) *template.Template {
	t := template.New("root").Funcs(template.FuncMap{
		"T":         func(k string) string { return a.T(k) },
		"route":     a.routePath,
		"hasInt":    hasInt,
		"hasString": hasString,
		"join":      strings.Join,
		"fmtTime": func(t time.Time) string {
			return t.Format("02.01 15:04:05")
		},
	})
	return template.Must(t.Parse(panelHTML + loginHTML))
}

const panelHTML = `{{define "themeControl.html"}}<label class="theme-control" for="theme-select">
<span>{{T "theme"}}</span>
<select id="theme-select" aria-label="{{T "theme"}}">
<option value="dark" selected>{{T "theme_dark"}}</option>
<option value="light">{{T "theme_light"}}</option>
<option value="system">{{T "theme_system"}}</option>
</select></label>{{end}}

{{define "head.html"}}<!doctype html>
<html lang="{{.Lang}}" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Mounvix — {{.Title}}</title><script src="{{route "/theme.js"}}"></script><link rel="stylesheet" href="{{route "/style.css"}}"></head><body class="panel-page">
<a class="skip-link" href="#main-content">{{T "skip_content"}}</a>
<div class="app-shell">
{{if .User}}<aside class="sidebar" id="sidebar" data-open="false">
<div class="sidebar-header">
<a class="brand" href="{{route "/"}}" aria-label="Mounvix — {{T "nav_overview"}}">Mounvix<span class="brand-subtitle">SERVER</span></a>
<button class="sidebar-close" id="nav-close" type="button" aria-label="{{T "close_menu"}}"><span aria-hidden="true">×</span></button>
</div>
<nav class="primary-nav" id="primary-nav" aria-label="{{T "nav_label"}}">
<a class="nav-link" data-nav-link href="{{route "/"}}">{{T "nav_overview"}}</a><a class="nav-link" data-nav-link href="{{route "/folders"}}">{{T "nav_folders"}}</a><a class="nav-link" data-nav-link href="{{route "/users"}}">{{T "nav_users"}}</a>
<a class="nav-link" data-nav-link href="{{route "/shares"}}">{{T "nav_shares"}}</a><a class="nav-link" data-nav-link href="{{route "/media"}}">{{T "nav_media"}}</a><a class="nav-link" data-nav-link href="{{route "/backup"}}">{{T "nav_backup"}}</a><a class="nav-link" data-nav-link href="{{route "/registrations"}}">{{T "nav_regs"}}</a><a class="nav-link" data-nav-link href="{{route "/plugins"}}">{{T "nav_plugins"}}</a><a class="nav-link" data-nav-link href="{{route "/ai"}}">{{T "nav_ai"}}</a><a class="nav-link" data-nav-link href="{{route "/settings"}}">{{T "nav_settings"}}</a><a class="nav-link" data-nav-link href="{{route "/logs"}}">{{T "nav_logs"}}</a><a class="nav-link" data-nav-link href="{{route "/stats"}}">{{T "nav_stats"}}</a>
</nav>
<div class="sidebar-footer"><form method="post" action="{{route "/logout"}}"><button>{{T "logout"}}</button></form></div>
</aside>{{end}}
<div class="main-shell{{if not .User}} main-shell--public{{end}}">
<header class="topbar">
{{if .User}}<button class="menu-toggle" id="nav-toggle" type="button" aria-controls="sidebar" aria-expanded="false"><span aria-hidden="true">☰</span><span>{{T "menu"}}</span></button><span class="mobile-brand">Mounvix</span>{{else}}<a class="mobile-brand" href="{{route "/"}}">Mounvix</a>{{end}}
{{template "themeControl.html" .}}
</header>
<main id="main-content" tabindex="-1">
{{end}}
{{define "foot.html"}}</main><footer class="site-footer">Mounvix {{.Version}}</footer></div></div></body></html>{{end}}

{{define "dashboard.html"}}{{template "head.html" .}}
<h1>{{T "overview"}}</h1>
<div class="grid">
<div class="card"><div class="muted">{{T "version"}}</div><div class="big">{{.Version}}</div></div>
<div class="card"><div class="muted">{{T "uptime"}}</div><div class="big">{{.Uptime}}</div></div>
<div class="card"><div class="muted">{{T "roots"}}</div><div class="big">{{.RootsCount}}</div></div>
<div class="card"><div class="muted">{{T "users"}}</div><div class="big">{{.UsersCount}}</div></div>
<div class="card"><div class="muted">{{T "sessions"}}</div><div class="big">{{.Sessions}}</div></div>
</div>
<h2>{{T "audit"}}</h2>
<div class="table-wrap"><table><tr><th>{{T "time"}}</th><th>{{T "username"}}</th><th>{{T "action"}}</th><th>{{T "info"}}</th></tr>
{{range .Audit}}<tr><td>{{fmtTime .At}}</td><td>{{.User}}</td><td>{{.Act}}</td><td>{{.Info}}</td></tr>{{end}}</table></div>
{{template "foot.html" .}}{{end}}

{{define "folders.html"}}{{template "head.html" .}}
<h1>{{T "folders"}}</h1>
<div class="table-wrap"><table><tr><th>{{T "name"}}</th><th>{{T "path"}}</th><th>{{T "readonly"}}</th><th>{{T "your_perm"}}</th></tr>
{{range .Roots}}<tr><td>{{.Name}}</td><td>{{.Path}}</td><td>{{if .ReadOnly}}yes{{else}}no{{end}}</td><td>{{if .Perm}}{{.Perm}}{{else}}—{{end}}</td></tr>{{end}}</table></div>
{{template "foot.html" .}}{{end}}

{{define "users.html"}}{{template "head.html" .}}
<h1>{{T "users"}}</h1>
<form method="post" action="{{route "/users"}}" class="row">
<input type="hidden" name="action" value="create">
<input name="username" placeholder="{{T "username"}}">
<input type="password" name="password" placeholder="{{T "password"}}">
<select name="role"><option value="user">user</option><option value="admin">admin</option></select>
<button>{{T "create_user"}}</button>
</form>
{{range $u := .Users}}
<section class="card">
<h3>{{$u.Name}} <span class="muted">{{$u.Role}}{{if $u.Disabled}} · disabled{{end}}</span></h3>
<form method="post" action="{{route "/users"}}" class="row"><input type="hidden" name="action" value="reset">
<input type="hidden" name="username" value="{{$u.Name}}">
<input type="password" name="newpass" placeholder="{{T "new_password"}}"><button>{{T "reset"}}</button></form>
<form method="post" action="{{route "/users"}}" class="row"><input type="hidden" name="action" value="toggle">
<input type="hidden" name="username" value="{{$u.Name}}"><button>{{T "disable"}}</button></form>
<form method="post" action="{{route "/users"}}" class="row"><input type="hidden" name="action" value="acl">
<input type="hidden" name="username" value="{{$u.Name}}">
{{range $root := $.Cfg.Roots}}<label>{{$root.Name}}
<select name="acl__{{$u.Name}}__{{$root.Name}}">
<option value="">—</option>
<option value="r" {{if eq (index $u.ACL $root.Name) "r"}}selected{{end}}>r</option>
<option value="rw" {{if eq (index $u.ACL $root.Name) "rw"}}selected{{end}}>rw</option>
</select></label>{{end}}
<button>{{T "apply"}}</button>
</form>
</section>
{{end}}
{{template "foot.html" .}}{{end}}

{{define "settings.html"}}{{template "head.html" .}}
<h1>{{T "settings"}}</h1>
<p class="muted">{{T "settings_note"}}</p>
<form method="post" action="{{route "/settings"}}" class="row">
<input type="hidden" name="action" value="lang">
<label>{{T "lang"}}
<select name="lang"><option value="ru" {{if eq .Lang "ru"}}selected{{end}}>Русский</option>
<option value="en" {{if eq .Lang "en"}}selected{{end}}>English</option></select></label>
<button>{{T "save"}}</button>
</form>
{{if .User.IsAdmin}}
<section class="card">
<h2>{{T "feeds_title"}}</h2>
<form method="post" action="{{route "/settings"}}">
<input type="hidden" name="action" value="feeds">
<label><input type="checkbox" name="feeds_enabled" value="true" {{if .Cfg.Feeds.Enabled}}checked{{end}}> {{T "feeds_enable"}}</label>
<div class="row"><label>{{T "feeds_roots"}}
<select name="feeds_roots" multiple size="4">{{range .Cfg.Roots}}<option value="{{.Name}}" {{if hasString $.Cfg.Feeds.RootNames .Name}}selected{{end}}>{{.Name}}</option>{{end}}</select></label></div>
<div class="row"><span>{{T "feeds_weekdays"}}:</span>
<label><input type="checkbox" name="feeds_weekdays" value="1" {{if hasInt .Cfg.Feeds.Weekdays 1}}checked{{end}}>{{T "weekday_mon"}}</label>
<label><input type="checkbox" name="feeds_weekdays" value="2" {{if hasInt .Cfg.Feeds.Weekdays 2}}checked{{end}}>{{T "weekday_tue"}}</label>
<label><input type="checkbox" name="feeds_weekdays" value="3" {{if hasInt .Cfg.Feeds.Weekdays 3}}checked{{end}}>{{T "weekday_wed"}}</label>
<label><input type="checkbox" name="feeds_weekdays" value="4" {{if hasInt .Cfg.Feeds.Weekdays 4}}checked{{end}}>{{T "weekday_thu"}}</label>
<label><input type="checkbox" name="feeds_weekdays" value="5" {{if hasInt .Cfg.Feeds.Weekdays 5}}checked{{end}}>{{T "weekday_fri"}}</label>
<label><input type="checkbox" name="feeds_weekdays" value="6" {{if hasInt .Cfg.Feeds.Weekdays 6}}checked{{end}}>{{T "weekday_sat"}}</label>
<label><input type="checkbox" name="feeds_weekdays" value="7" {{if hasInt .Cfg.Feeds.Weekdays 7}}checked{{end}}>{{T "weekday_sun"}}</label></div>
<div class="row"><label>{{T "feeds_times"}} <input name="feeds_times" value="{{join .Cfg.Feeds.Times ","}}" placeholder="09:00,18:00"></label></div>
<div class="row"><label>{{T "feeds_quiet_start"}} <input type="time" name="feeds_quiet_start" value="{{.Cfg.Feeds.QuietHoursStart}}"></label>
<label>{{T "feeds_quiet_end"}} <input type="time" name="feeds_quiet_end" value="{{.Cfg.Feeds.QuietHoursEnd}}"></label></div>
<div class="row"><label>{{T "feeds_period"}} <input type="number" min="1" max="365" name="feeds_period_days" value="{{.Cfg.Feeds.PeriodDays}}"></label>
<label>{{T "feeds_max_items"}} <input type="number" min="1" max="8" name="feeds_max_items" value="{{.Cfg.Feeds.MaxItems}}"></label>
<label>{{T "feeds_retention"}} <input type="number" min="1" max="365" name="feeds_retention_days" value="{{.Cfg.Feeds.RetentionDays}}"></label></div>
<button>{{T "save"}}</button>
</form>
</section>
{{end}}
<div class="table-wrap"><table><tr><th>{{T "name"}}</th><th>{{T "path"}}</th><th>{{T "readonly"}}</th></tr>
{{range .Cfg.Roots}}<tr><td>{{.Name}}</td><td>{{.Path}}</td><td>{{if .ReadOnly}}yes{{else}}no{{end}}</td></tr>{{end}}</table></div>
<p class="muted">config: {{.CfgPath}}</p>
{{template "foot.html" .}}{{end}}

{{define "logs.html"}}{{template "head.html" .}}
<h1>{{T "logs"}}</h1>
<div class="table-wrap"><table><tr><th>{{T "time"}}</th><th>{{T "username"}}</th><th>{{T "action"}}</th><th>{{T "info"}}</th></tr>
{{range .Audit}}<tr><td>{{fmtTime .At}}</td><td>{{.User}}</td><td>{{.Act}}</td><td>{{.Info}}</td></tr>{{end}}</table></div>
{{template "foot.html" .}}{{end}}

{{define "stats.html"}}{{template "head.html" .}}
<h1>{{T "stats"}}</h1>
<div class="grid">
<div class="card"><div class="muted">{{T "total_users"}}</div><div class="big">{{.UsersCount}}</div></div>
<div class="card"><div class="muted">{{T "total_roots"}}</div><div class="big">{{.RootsCount}}</div></div>
<div class="card"><div class="muted">{{T "total_sessions"}}</div><div class="big">{{.Sessions}}</div></div>
<div class="card"><div class="muted">{{T "audit_events"}}</div><div class="big">{{.AuditCount}}</div></div>
</div>
{{template "foot.html" .}}{{end}}

{{define "shares.html"}}{{template "head.html" .}}
<h1>Шары</h1>
<form method="post" action="{{route "/shares"}}" class="row">
<input type="hidden" name="action" value="create">
<select name="root">{{range .Roots}}<option value="{{.Name}}">{{.Name}} ({{.Path}})</option>{{end}}</select>
<input name="path" placeholder="путь (например, Photos)" style="min-width:220px">
<input name="hours" value="24" style="width:70px" title="часы">
<input name="password" placeholder="пароль (не обяз.)" style="width:150px">
<label><input type="checkbox" name="allow_upload"> загрузка</label>
<button>Создать ссылку</button>
</form>
<div class="table-wrap"><table><tr><th>Ссылка</th><th>Путь</th><th>Истекает</th><th>Пароль</th><th></th></tr>
{{range .Shares}}<tr><td><a href="{{route "/s"}}/{{.Token}}" target="_blank">{{route "/s"}}/{{.Token}}</a></td>
<td>{{.Root}}/{{.Path}}</td><td>{{fmtTime .ExpiresAt}}</td><td>{{if .PasswordHash}}да{{else}}нет{{end}}</td>
<td><form method="post" action="{{route "/shares"}}"><input type="hidden" name="action" value="revoke"><input type="hidden" name="token" value="{{.Token}}"><button>Отозвать</button></form></td></tr>
{{end}}</table></div>
{{template "foot.html" .}}{{end}}

{{define "media.html"}}{{template "head.html" .}}
<h1>Медиа</h1>
<form method="get" action="{{route "/media"}}" class="row">
<label>Папка: <select name="root">{{range .Cfg.Roots}}<option value="{{.Name}}" {{if eq .Name $.Root}}selected{{end}}>{{.Name}} ({{.Path}})</option>{{end}}</select></label>
<input name="path" value="{{.Path}}" placeholder="путь внутри (необязательно)" style="min-width:220px">
<button>Открыть</button>
</form>
{{if .Entries}}
<div class="gallery">
{{range .Entries}}
<a class="item" href="{{.FileURL}}" target="_blank">
{{if .IsVideo}}<div class="thumb vid">🎬</div>{{else}}<img class="thumb" src="{{.ThumbURL}}" loading="lazy" alt="{{.Name}}">{{end}}
<div class="cap"><span>{{.Name}}</span></div>
</a>{{end}}
</div>
{{else}}
<p class="muted">В этой папке нет фотографий или видео.</p>
{{end}}
{{template "foot.html" .}}{{end}}

{{define "backup.html"}}{{template "head.html" .}}
<h1>Бэкап</h1>
<form method="post" action="{{route "/backup"}}" class="row">
<input type="hidden" name="action" value="create">
<input name="name" placeholder="Название" required>
<input name="source" placeholder="Исходная папка (сервер)" required style="min-width:240px">
<input name="local_path" placeholder="Локальная папка/USB" style="min-width:180px">
<input name="webdav_url" placeholder="WebDAV URL (необяз.)" style="min-width:200px">
<input name="webdav_user" placeholder="WebDAV логин" style="width:130px">
<input name="webdav_pass" placeholder="WebDAV пароль" style="width:130px">
<input name="interval_hours" placeholder="интервал, ч (0=вручную)" style="width:180px">
<button>Создать задание</button>
</form>
<div class="table-wrap"><table><tr><th>Задание</th><th>Источник</th><th>Цели</th><th>Расписание</th><th>Последний запуск</th><th></th></tr>
{{range .Jobs}}<tr>
<td>{{.Name}}</td><td class="muted">{{.Source}}</td>
<td>{{range .Targets}}{{.Type}}: {{if eq .Type "local"}}{{.Path}}{{else}}{{.URL}}{{end}}<br>{{end}}</td>
<td>{{if .IntervalHours}}каждые {{.IntervalHours}} ч{{else}}вручную{{end}}</td>
<td>{{if .LastRun.IsZero}}—{{else}}{{fmtTime .LastRun}}{{end}}
{{with index $.Reports .ID}}<br><span class="muted">файлов: {{.Files}}, байт: {{.Bytes}}, пропущено: {{.Skipped}}, ошибок: {{len .Errors}}</span>{{end}}</td>
<td>
<form method="post" action="{{route "/backup"}}" style="display:inline"><input type="hidden" name="action" value="run"><input type="hidden" name="id" value="{{.ID}}"><button>Запустить</button></form>
<form method="post" action="{{route "/backup"}}" style="display:inline"><input type="hidden" name="action" value="delete"><input type="hidden" name="id" value="{{.ID}}"><button>Удалить</button></form>
<form method="post" action="{{route "/backup"}}" style="display:inline"><input type="hidden" name="action" value="restore"><input type="hidden" name="id" value="{{.ID}}"><input name="dest" placeholder="куда восстановить" style="width:150px"><button>Восстановить</button></form>
</td></tr>{{end}}</table></div>
{{template "foot.html" .}}{{end}}

{{define "register.html"}}{{template "head.html" .}}
<h1>Регистрация</h1>
{{if .Msg}}<p>{{.Msg}}</p>{{end}}
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<form method="post" action="{{route "/register"}}">
<label>Логин<br><input name="username" required minlength="3" maxlength="32"></label><br>
<label>Пароль (минимум 8 символов)<br><input type="password" name="password" required minlength="8"></label><br>
<button>Отправить заявку</button>
</form>
<p class="muted">После подтверждения администратором вы сможете войти.</p>
{{template "foot.html" .}}{{end}}

{{define "registrations.html"}}{{template "head.html" .}}
<h1>Заявки на регистрацию</h1>
{{if .Msg}}<p>{{.Msg}}</p>{{end}}
{{if .Pending}}
<div class="table-wrap"><table><tr><th>Логин</th><th>Создана</th><th></th></tr>
{{range .Pending}}<tr><td>{{.Name}}</td><td>{{fmtTime .CreatedAt}}</td><td>
<form method="post" action="{{route "/registrations"}}" style="display:inline"><input type="hidden" name="action" value="approve"><input type="hidden" name="id" value="{{.ID}}"><button>Принять</button></form>
<form method="post" action="{{route "/registrations"}}" style="display:inline"><input type="hidden" name="action" value="reject"><input type="hidden" name="id" value="{{.ID}}"><button>Отклонить</button></form>
</td></tr>{{end}}</table></div>
{{else}}<p class="muted">Заявок нет.</p>{{end}}
{{template "foot.html" .}}{{end}}

{{define "plugins.html"}}{{template "head.html" .}}
<h1>Плагины</h1>
{{if .Msg}}<p>{{.Msg}}</p>{{end}}
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<div class="table-wrap"><table><tr><th>Плагин</th><th>Версия</th><th>Тип</th><th>Доступен</th><th>Включён</th><th></th></tr>
{{range .Plugins}}<tr><td>{{.Name}}<br><span class="muted">{{.Description}}</span></td><td>{{.Version}}</td><td>{{.Kind}}</td>
<td>{{if .Available}}да{{else}}нет{{end}}</td><td>{{if .Enabled}}да{{else}}нет{{end}}</td>
<td>{{if .Available}}<form method="post" action="{{route "/plugins"}}"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="enabled" value="{{if .Enabled}}0{{else}}1{{end}}"><button>{{if .Enabled}}Выключить{{else}}Включить{{end}}</button></form>{{end}}</td></tr>{{end}}</table></div>
<h2>rclone</h2>
{{if .Rclone.Installed}}
<p>Версия: {{.Rclone.Version}}</p>
<p>Хранилища: {{range .Rclone.Remotes}}{{.}} {{else}}нет{{end}}</p>
<form method="post" action="{{route "/plugins"}}" class="row"><input type="hidden" name="action" value="rclone_sync"><input name="src" placeholder="источник (local или remote:путь)" style="min-width:220px"><input name="dst" placeholder="назначение" style="min-width:220px"><button>rclone sync</button></form>
{{else}}
<p class="muted">rclone не установлен. Установите: <code>opkg install rclone</code> (Entware) или <code>apt install rclone</code>.</p>
{{end}}
<p class="muted">Манифесты плагинов — <code>data/plugins/*.json</code>.</p>
{{template "foot.html" .}}{{end}}

{{define "ai.html"}}{{template "head.html" .}}
<h1>AI-распознавание</h1>
{{if .Msg}}<p>{{.Msg}}</p>{{end}}
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<h2>Бэкенд (OpenAI-совместимый)</h2>
<form method="post" action="{{route "/ai"}}" class="row">
<input type="hidden" name="action" value="save">
<input name="base_url" value="{{.Cfg.AIBaseURL}}" placeholder="http://host:8080" style="min-width:240px">
<input name="model" value="{{.Cfg.AIModel}}" placeholder="model id" style="min-width:220px">
<input name="key" value="{{.Cfg.AIKey}}" placeholder="API key (необяз.)" style="width:180px">
<button>Сохранить</button>
</form>
<p class="muted">Ключ хранится в config.json; пусто — без авторизации.</p>
<h2>Распознать изображение</h2>
<form method="post" action="{{route "/ai"}}" class="row">
<input type="hidden" name="action" value="describe">
<select name="root">{{range .Cfg.Roots}}<option value="{{.Name}}">{{.Name}}</option>{{end}}</select>
<input name="path" placeholder="путь к файлу (например, test.png)" style="min-width:260px">
<input name="prompt" placeholder="вопрос (необяз.)" style="min-width:220px">
<button>Распознать</button>
</form>
{{if .Result}}<h3>Ответ</h3><pre style="white-space:pre-wrap">{{.Result}}</pre>{{end}}
{{template "foot.html" .}}{{end}}
`

const loginHTML = `{{define "login.html"}}<!doctype html>
<html lang="{{.Lang}}" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Mounvix — {{T "login"}}</title><script src="{{.ThemePath}}"></script><link rel="stylesheet" href="{{.StylePath}}"></head>
<body class="auth-page"><a class="skip-link" href="#main-content">{{T "skip_content"}}</a>
<header class="auth-toolbar"><a class="brand" href="{{.HomePath}}">Mounvix</a>{{template "themeControl.html" .}}</header>
<main class="auth-main" id="main-content" tabindex="-1"><section class="auth-card" aria-labelledby="auth-title">
<h1 id="auth-title">Mounvix</h1>
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<p><a href="{{.RegisterPath}}">Регистрация</a></p>
<form method="post" action="{{.LoginPath}}">
<input type="hidden" name="next" value="{{.Next}}">
<label>{{T "username"}}<br><input name="username" autofocus required></label><br>
<label>{{T "password"}}<br><input type="password" name="password" required></label><br>
<button>{{T "signin"}}</button>
</form>
<p class="muted">Mounvix server</p>
</section></main></body></html>{{end}}`
