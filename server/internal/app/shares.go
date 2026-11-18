package app

import (
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/share"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

type sharesView struct {
	Title   string
	Lang    string
	User    *User
	Cfg     Config
	Version string
	Roots   []Root
	Shares  []*share.Share
}

// handleSharesPage — страница «Шары» в панели (создание/отзыв).
func (a *App) handleSharesPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		switch r.PostFormValue("action") {
		case "create":
			hours, _ := time.ParseDuration(r.PostFormValue("hours") + "h")
			if hours <= 0 {
				hours = 24 * time.Hour
			}
			s, err := a.shares.Create(
				r.PostFormValue("root"),
				strings.TrimSpace(r.PostFormValue("path")),
				u.Name,
				hours,
				r.PostFormValue("password"),
				r.PostFormValue("allow_upload") != "",
			)
			if err != nil {
				a.store.Audit(u.Name, "share_create_failed", err.Error())
			} else {
				a.store.Audit(u.Name, "share_create", s.Root+"/"+s.Path)
			}
		case "revoke":
			if a.shares.Revoke(r.PostFormValue("token")) {
				a.store.Audit(u.Name, "share_revoke", r.PostFormValue("token"))
			}
		}
		http.Redirect(w, r, "/shares", http.StatusSeeOther)
		return
	}
	cfg := a.configSnapshot()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "shares.html", &sharesView{
		Title: "Шары", Lang: cfg.Lang, Version: version.Version,
		User: u, Cfg: cfg, Roots: cfg.Roots, Shares: a.shares.List(),
	})
}

type shareCreateRequest struct {
	Root        string `json:"root"`
	Path        string `json:"path"`
	Hours       int    `json:"hours"`
	Password    string `json:"password"`
	AllowUpload bool   `json:"allow_upload"`
}

// handleSharesAPI — JSON API для приложения.
func (a *App) handleSharesAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"shares": a.shares.List()})
	case http.MethodPost:
		var req shareCreateRequest
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
		} else {
			_ = r.ParseForm()
			req.Root = r.PostFormValue("root")
			req.Path = r.PostFormValue("path")
			req.Hours = 24
			if h := r.PostFormValue("hours"); h != "" {
				if n, err := time.ParseDuration(h + "h"); err == nil {
					req.Hours = int(n.Hours())
				}
			}
			req.Password = r.PostFormValue("password")
			req.AllowUpload = r.PostFormValue("allow_upload") != ""
		}
		ttl := time.Duration(req.Hours) * time.Hour
		if ttl <= 0 {
			ttl = 24 * time.Hour
		}
		s, err := a.shares.Create(req.Root, req.Path, u.Name, ttl, req.Password, req.AllowUpload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.store.Audit(u.Name, "share_create", s.Root+"/"+s.Path)
		writeJSON(w, map[string]any{"token": s.Token, "url": "/s/" + s.Token})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) handleShareRevokeAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/v1/shares/")
	if a.shares.Revoke(token) {
		a.store.Audit(u.Name, "share_revoke", token)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.NotFound(w, r)
}

// safeJoin не позволяет выйти за пределы базового каталога.
func safeJoin(base, rel string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if cleaned == "." {
		cleaned = ""
	}
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("bad path")
	}
	full := filepath.Join(base, cleaned)
	b := filepath.Clean(base)
	if full != b && !strings.HasPrefix(full, b+string(os.PathSeparator)) {
		return "", fmt.Errorf("outside root")
	}
	return full, nil
}

// handlePublicShare — публичная страница ссылки /s/<token> и стриминг файла (Range).
func (a *App) handlePublicShare(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/s/")
	if token == "" || strings.Contains(token, "/") {
		http.NotFound(w, r)
		return
	}
	s := a.shares.Get(token)
	if s == nil {
		http.Error(w, "Ссылка не найдена или истекла", http.StatusNotFound)
		return
	}
	if s.PasswordHash != "" && !a.shares.CheckCookie(token, cookieValue(r, a.shares.CookieName(token))) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			if cookie, ok := a.shares.Unlock(token, r.PostFormValue("password")); ok {
				http.SetCookie(w, &http.Cookie{
					Name: a.shares.CookieName(token), Value: cookie, Path: "/",
					Expires: s.ExpiresAt, HttpOnly: true, SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, r.URL.RequestURI(), http.StatusSeeOther)
				return
			}
			a.renderSharePassword(w, token, "Неверный пароль", http.StatusUnauthorized)
			return
		}
		a.renderSharePassword(w, token, "", http.StatusOK)
		return
	}
	root, ok := a.findRoot(s.Root)
	if !ok {
		http.NotFound(w, r)
		return
	}
	sub := r.URL.Query().Get("path")
	target, err := safeJoin(root.Path, filepath.ToSlash(filepath.Join(s.Path, sub)))
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	st, err := os.Stat(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if st.IsDir() {
		a.renderShareDir(w, s, target, sub)
		return
	}
	f, err := os.Open(target)
	if err != nil {
		http.Error(w, "open failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(target))); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(target)+"\"")
	http.ServeContent(w, r, filepath.Base(target), st.ModTime(), f)
}

func (a *App) renderSharePassword(w http.ResponseWriter, token, errMsg string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html lang="ru"><head><meta charset="utf-8">`+
		`<meta name="viewport" content="width=device-width,initial-scale=1"><title>Mounvix — защищённая ссылка</title></head>`+
		`<body style="font-family:system-ui;background:#0f1b2d;color:#fff;display:flex;align-items:center;justify-content:center;min-height:100vh">`+
		`<form method="post" style="background:#fff;color:#17202a;padding:20px;border-radius:10px;width:300px">`+
		`<h3 style="margin-top:0">Защищённая ссылка</h3>%s`+
		`<label>Пароль<br><input type="password" name="password" required style="width:100%%;padding:6px"></label><br><br>`+
		`<button style="width:100%%;padding:8px">Открыть</button></form></body></html>`,
		func() string {
			if errMsg == "" {
				return ""
			}
			return `<p style="color:#b00020">` + html.EscapeString(errMsg) + `</p>`
		}())
}

func (a *App) renderShareDir(w http.ResponseWriter, s *share.Share, target, sub string) {
	entries, err := os.ReadDir(target)
	if err != nil {
		http.Error(w, "read dir failed", http.StatusInternalServerError)
		return
	}
	type row struct {
		Name  string
		IsDir bool
		Size  int64
		URL   string
	}
	rows := make([]row, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(sub, e.Name()))
		rows = append(rows, row{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  info.Size(),
			URL:   "/s/" + s.Token + "?path=" + urlQueryEscape(rel),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="ru"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1"><title>Mounvix — ссылка</title></head>`)
	b.WriteString(`<body style="font-family:system-ui;max-width:800px;margin:20px auto;padding:0 12px">`)
	b.WriteString(`<h2>Общий доступ: ` + html.EscapeString(filepath.Base(target)) + `</h2>`)
	if sub != "" {
		parent := filepath.ToSlash(filepath.Dir(sub))
		if parent == "." {
			parent = ""
		}
		b.WriteString(`<p><a href="/s/` + s.Token + `?path=` + urlQueryEscape(parent) + `">← Наверх</a></p>`)
	}
	b.WriteString(`<ul style="list-style:none;padding:0">`)
	for _, r := range rows {
		icon := "📄"
		if r.IsDir {
			icon = "📁"
		}
		b.WriteString(`<li style="padding:6px 0;border-bottom:1px solid #e6e9ee"><a href="` + r.URL + `">` + icon + ` ` + html.EscapeString(r.Name) + `</a></li>`)
	}
	b.WriteString(`</ul></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// urlQueryEscape — минимальное экранирование для query-параметра.
func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/':
			b.WriteRune(r)
		default:
			b.WriteString("%")
			b.WriteString(fmt.Sprintf("%02X", r))
		}
	}
	return b.String()
}
