package app

import (
	"net/http"
	"time"
)

const cookieName = "mvx"

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.renderLogin(w, "", http.StatusOK, a.safeReturnRoute(r.URL.Query().Get("next")))
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	next := a.safeReturnRoute(r.PostFormValue("next"))
	name := r.PostFormValue("username")
	pass := r.PostFormValue("password")
	u, ok := a.store.Verify(name, pass)
	if !ok {
		a.store.Audit(name, "login_failed", r.RemoteAddr)
		a.renderLogin(w, "Неверный логин или пароль", http.StatusUnauthorized, next)
		return
	}
	tok := a.store.CreateSession(u.Name)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     a.sessionCookiePath(),
		HttpOnly: true,
		Secure:   a.sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 3600,
	})
	a.store.Audit(u.Name, "login", r.RemoteAddr)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if name, ok := a.store.SessionUser(c.Value); ok {
			a.store.Audit(name, "logout", r.RemoteAddr)
		}
		a.store.DropSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: a.sessionCookiePath(), Secure: a.sessionCookieSecure(r), MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, a.routePath("/login"), http.StatusSeeOther)
}

func (a *App) renderLogin(w http.ResponseWriter, errMsg string, status int, next string) {
	cfg := a.configSnapshot()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.ExecuteTemplate(w, "login.html", map[string]any{
		"T": a.T, "Err": errMsg, "Lang": cfg.Lang, "Next": next,
		"ThemePath": a.routePath("/theme.js"), "StylePath": a.routePath("/style.css"),
		"HomePath": a.routePath("/"), "RegisterPath": a.routePath("/register"), "LoginPath": a.routePath("/login"),
	})
}

func (a *App) T(key string) string {
	m := stringsRU()
	if a.configSnapshot().Lang == "en" {
		m = stringsEN()
	}
	if v, ok := m[key]; ok {
		return v
	}
	return key
}

var _ = time.Now
