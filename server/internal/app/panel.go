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
		"nav_shares": "Шары", "nav_media": "Медиа", "nav_backup": "Бэкап", "nav_settings": "Настройки", "nav_logs": "Логи", "nav_stats": "Статистика", "logout": "Выход",
		"overview": "Обзор", "version": "Версия", "uptime": "Аптайм", "roots": "Папки",
		"users": "Пользователи", "sessions": "Сессии", "audit": "Аудит",
		"folders": "Доступ к папкам", "name": "Имя", "path": "Путь", "readonly": "Только чтение",
		"your_perm":   "Ваши права",
		"create_user": "Создать пользователя", "new_password": "Новый пароль", "role": "Роль",
		"apply": "Применить", "reset": "Сбросить пароль", "disable": "Отключить", "enable": "Включить",
		"lang": "Язык", "save": "Сохранить", "logs": "Логи", "time": "Время", "action": "Действие",
		"info": "Информация", "stats": "Статистика", "total_users": "Пользователей",
		"total_roots": "Папок", "total_sessions": "Активных сессий", "audit_events": "Событий аудита",
		"settings_note": "Корни задаются в config.json. Здесь можно сменить язык интерфейса.",
		"no_access":     "Нет доступа", "acl": "Доступ к папкам (чтение/запись)", "create": "Создать",
	}
}

func stringsEN() map[string]string {
	return map[string]string{
		"login": "Login", "username": "Username", "password": "Password", "signin": "Sign in",
		"nav_overview": "Overview", "nav_folders": "Folders access", "nav_users": "Users",
		"nav_shares": "Shares", "nav_media": "Media", "nav_backup": "Backup", "nav_settings": "Settings", "nav_logs": "Logs", "nav_stats": "Stats", "logout": "Log out",
		"overview": "Overview", "version": "Version", "uptime": "Uptime", "roots": "Folders",
		"users": "Users", "sessions": "Sessions", "audit": "Audit",
		"folders": "Folders access", "name": "Name", "path": "Path", "readonly": "Read-only",
		"your_perm":   "Your permission",
		"create_user": "Create user", "new_password": "New password", "role": "Role",
		"apply": "Apply", "reset": "Reset password", "disable": "Disable", "enable": "Enable",
		"lang": "Language", "save": "Save", "logs": "Logs", "time": "Time", "action": "Action",
		"info": "Info", "stats": "Stats", "total_users": "Users", "total_roots": "Folders",
		"total_sessions": "Active sessions", "audit_events": "Audit events",
		"settings_note": "Roots are set in config.json. Here you can change the interface language.",
		"no_access":     "No access", "acl": "Folder access (read/write)", "create": "Create",
	}
}

const styleCSS = `body{font-family:system-ui,sans-serif;margin:0;background:#f6f7f9;color:#17202a}
header{display:flex;gap:16px;align-items:center;padding:10px 16px;background:#10233b;color:#fff;flex-wrap:wrap}
header nav{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
header a{color:#dbe7f5;text-decoration:none}header a:hover{color:#fff}
main{padding:16px;max-width:1100px;margin:0 auto}
table{border-collapse:collapse;width:100%;background:#fff}
th,td{text-align:left;padding:6px 10px;border-bottom:1px solid #e6e9ee;font-size:14px}
th{background:#eef1f6}
.card{background:#fff;border:1px solid #e6e9ee;border-radius:10px;padding:12px;margin:8px 0}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:10px;margin:12px 0}
.big{font-size:22px;font-weight:600}.muted{color:#68727e;font-size:13px}
.row{display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin:6px 0}
input,select,button{font:inherit;padding:6px 8px;border:1px solid #cfd6df;border-radius:8px;background:#fff}
button{background:#1f4e79;color:#fff;border-color:#1f4e79;cursor:pointer}button:hover{background:#2a6396}
.login{display:flex;justify-content:center;align-items:center;min-height:100vh;background:#0f1b2d}
.login .card{width:340px}.login h1{color:#1f4e79}.err{color:#b00020}
footer{padding:12px 16px;color:#68727e;font-size:12px}
.gallery{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:12px;margin:16px 0}
.gallery .item{background:#fff;border:1px solid #e6e9ee;border-radius:10px;overflow:hidden;transition:transform .15s ease-out,box-shadow .15s ease-out}
.gallery .item:hover{transform:translateY(-2px);box-shadow:0 6px 18px rgba(15,27,45,.08)}
.gallery .thumb{display:block;width:100%;aspect-ratio:4/3;object-fit:cover;background:#eef1f6}
.gallery .cap{padding:8px 10px;font-size:13px;display:flex;justify-content:space-between;gap:8px}
.gallery .vid{display:flex;align-items:center;justify-content:center;height:100%;font-size:40px;color:#1f4e79;background:#eef1f6}
`

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
	return &view{
		User:    u,
		Cfg:     a.cfg,
		CfgPath: a.cfgPath,
		Version: version.Version,
		Uptime:  humanUptime(time.Since(a.started)),
		Lang:    a.cfg.Lang,
	}
}

func (a *App) render(w http.ResponseWriter, page string, v *view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.ExecuteTemplate(w, page, v); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (a *App) handleStyle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(styleCSS))
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	v := a.viewFor(r)
	v.Title = a.T("overview")
	v.RootsCount = len(a.cfg.Roots)
	v.UsersCount = len(a.store.Users())
	v.Sessions = a.store.SessionCount()
	v.AuditCount = a.store.AuditCount()
	v.Audit = a.store.AuditTail(8)
	a.render(w, "dashboard.html", v)
}

func (a *App) handleFolders(w http.ResponseWriter, r *http.Request) {
	v := a.viewFor(r)
	v.Title = a.T("folders")
	for _, rt := range a.cfg.Roots {
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
		http.Redirect(w, r, "/users", http.StatusSeeOther)
		return
	}
	v := a.viewFor(r)
	v.Title = a.T("users")
	v.Users = a.store.Users()
	a.render(w, "users.html", v)
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if lang := r.PostFormValue("lang"); lang == "ru" || lang == "en" {
			a.cfg.Lang = lang
			if err := SaveConfig(a.cfgPath, a.cfg); err != nil {
				a.store.Audit("admin", "settings_save_failed", err.Error())
			}
			if u, ok := a.cookieUser(r); ok {
				a.store.Audit(u.Name, "settings_lang", lang)
			}
		}
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
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
	v.RootsCount = len(a.cfg.Roots)
	v.UsersCount = len(a.store.Users())
	v.Sessions = a.store.SessionCount()
	v.AuditCount = a.store.AuditCount()
	a.render(w, "stats.html", v)
}

func mustTemplates(a *App) *template.Template {
	t := template.New("root").Funcs(template.FuncMap{
		"T": func(k string) string { return a.T(k) },
		"fmtTime": func(t time.Time) string {
			return t.Format("02.01 15:04:05")
		},
	})
	return template.Must(t.Parse(panelHTML + loginHTML))
}

const panelHTML = `{{define "head.html"}}<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Mounvix — {{.Title}}</title><link rel="stylesheet" href="/style.css"></head><body>
{{if .User}}<header><b>Mounvix</b><nav>
<a href="/">{{T "nav_overview"}}</a><a href="/folders">{{T "nav_folders"}}</a><a href="/users">{{T "nav_users"}}</a>
<a href="/shares">{{T "nav_shares"}}</a><a href="/media">{{T "nav_media"}}</a><a href="/backup">{{T "nav_backup"}}</a><a href="/settings">{{T "nav_settings"}}</a><a href="/logs">{{T "nav_logs"}}</a><a href="/stats">{{T "nav_stats"}}</a>
<form method="post" action="/logout"><button>{{T "logout"}}</button></form>
</nav></header>{{end}}<main>
{{end}}
{{define "foot.html"}}</main><footer>Mounvix {{.Version}}</footer></body></html>{{end}}

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
<table><tr><th>{{T "time"}}</th><th>{{T "username"}}</th><th>{{T "action"}}</th><th>{{T "info"}}</th></tr>
{{range .Audit}}<tr><td>{{fmtTime .At}}</td><td>{{.User}}</td><td>{{.Act}}</td><td>{{.Info}}</td></tr>{{end}}</table>
{{template "foot.html" .}}{{end}}

{{define "folders.html"}}{{template "head.html" .}}
<h1>{{T "folders"}}</h1>
<table><tr><th>{{T "name"}}</th><th>{{T "path"}}</th><th>{{T "readonly"}}</th><th>{{T "your_perm"}}</th></tr>
{{range .Roots}}<tr><td>{{.Name}}</td><td>{{.Path}}</td><td>{{if .ReadOnly}}yes{{else}}no{{end}}</td><td>{{if .Perm}}{{.Perm}}{{else}}—{{end}}</td></tr>{{end}}</table>
{{template "foot.html" .}}{{end}}

{{define "users.html"}}{{template "head.html" .}}
<h1>{{T "users"}}</h1>
<form method="post" action="/users" class="row">
<input type="hidden" name="action" value="create">
<input name="username" placeholder="{{T "username"}}">
<input type="password" name="password" placeholder="{{T "password"}}">
<select name="role"><option value="user">user</option><option value="admin">admin</option></select>
<button>{{T "create_user"}}</button>
</form>
{{range $u := .Users}}
<section class="card">
<h3>{{$u.Name}} <span class="muted">{{$u.Role}}{{if $u.Disabled}} · disabled{{end}}</span></h3>
<form method="post" action="/users" class="row"><input type="hidden" name="action" value="reset">
<input type="hidden" name="username" value="{{$u.Name}}">
<input type="password" name="newpass" placeholder="{{T "new_password"}}"><button>{{T "reset"}}</button></form>
<form method="post" action="/users" class="row"><input type="hidden" name="action" value="toggle">
<input type="hidden" name="username" value="{{$u.Name}}"><button>{{T "disable"}}</button></form>
<form method="post" action="/users" class="row"><input type="hidden" name="action" value="acl">
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
<form method="post" action="/settings" class="row">
<input type="hidden" name="action" value="lang">
<label>{{T "lang"}}
<select name="lang"><option value="ru" {{if eq .Lang "ru"}}selected{{end}}>Русский</option>
<option value="en" {{if eq .Lang "en"}}selected{{end}}>English</option></select></label>
<button>{{T "save"}}</button>
</form>
<table><tr><th>{{T "name"}}</th><th>{{T "path"}}</th><th>{{T "readonly"}}</th></tr>
{{range .Cfg.Roots}}<tr><td>{{.Name}}</td><td>{{.Path}}</td><td>{{if .ReadOnly}}yes{{else}}no{{end}}</td></tr>{{end}}</table>
<p class="muted">config: {{.CfgPath}}</p>
{{template "foot.html" .}}{{end}}

{{define "logs.html"}}{{template "head.html" .}}
<h1>{{T "logs"}}</h1>
<table><tr><th>{{T "time"}}</th><th>{{T "username"}}</th><th>{{T "action"}}</th><th>{{T "info"}}</th></tr>
{{range .Audit}}<tr><td>{{fmtTime .At}}</td><td>{{.User}}</td><td>{{.Act}}</td><td>{{.Info}}</td></tr>{{end}}</table>
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
<form method="post" action="/shares" class="row">
<input type="hidden" name="action" value="create">
<select name="root">{{range .Roots}}<option value="{{.Name}}">{{.Name}} ({{.Path}})</option>{{end}}</select>
<input name="path" placeholder="путь (например, Photos)" style="min-width:220px">
<input name="hours" value="24" style="width:70px" title="часы">
<input name="password" placeholder="пароль (не обяз.)" style="width:150px">
<label><input type="checkbox" name="allow_upload"> загрузка</label>
<button>Создать ссылку</button>
</form>
<table><tr><th>Ссылка</th><th>Путь</th><th>Истекает</th><th>Пароль</th><th></th></tr>
{{range .Shares}}<tr><td><a href="/s/{{.Token}}" target="_blank">/s/{{.Token}}</a></td>
<td>{{.Root}}/{{.Path}}</td><td>{{fmtTime .ExpiresAt}}</td><td>{{if .PasswordHash}}да{{else}}нет{{end}}</td>
<td><form method="post" action="/shares"><input type="hidden" name="action" value="revoke"><input type="hidden" name="token" value="{{.Token}}"><button>Отозвать</button></form></td></tr>
{{end}}</table>
{{template "foot.html" .}}{{end}}

{{define "media.html"}}{{template "head.html" .}}
<h1>Медиа</h1>
<form method="get" action="/media" class="row">
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
<form method="post" action="/backup" class="row">
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
<table><tr><th>Задание</th><th>Источник</th><th>Цели</th><th>Расписание</th><th>Последний запуск</th><th></th></tr>
{{range .Jobs}}<tr>
<td>{{.Name}}</td><td class="muted">{{.Source}}</td>
<td>{{range .Targets}}{{.Type}}: {{if eq .Type "local"}}{{.Path}}{{else}}{{.URL}}{{end}}<br>{{end}}</td>
<td>{{if .IntervalHours}}каждые {{.IntervalHours}} ч{{else}}вручную{{end}}</td>
<td>{{if .LastRun.IsZero}}—{{else}}{{fmtTime .LastRun}}{{end}}
{{with index $.Reports .ID}}<br><span class="muted">файлов: {{.Files}}, байт: {{.Bytes}}, пропущено: {{.Skipped}}, ошибок: {{len .Errors}}</span>{{end}}</td>
<td>
<form method="post" action="/backup" style="display:inline"><input type="hidden" name="action" value="run"><input type="hidden" name="id" value="{{.ID}}"><button>Запустить</button></form>
<form method="post" action="/backup" style="display:inline"><input type="hidden" name="action" value="delete"><input type="hidden" name="id" value="{{.ID}}"><button>Удалить</button></form>
</td></tr>{{end}}</table>
{{template "foot.html" .}}{{end}}
`

const loginHTML = `{{define "login.html"}}<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Mounvix — {{T "login"}}</title><link rel="stylesheet" href="/style.css"></head>
<body class="login"><div class="card">
<h1>Mounvix</h1>
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
<form method="post" action="/login">
<label>{{T "username"}}<br><input name="username" autofocus required></label><br>
<label>{{T "password"}}<br><input type="password" name="password" required></label><br>
<button>{{T "signin"}}</button>
</form>
<p class="muted">Mounvix server</p>
</div></body></html>{{end}}`
