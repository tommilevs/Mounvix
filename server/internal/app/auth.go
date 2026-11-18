package app

import (
	"net/http"
	"time"
)

const cookieName = "mvx"

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.renderLogin(w, "", 200)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = r.ParseForm()
	name := r.PostFormValue("username")
	pass := r.PostFormValue("password")
	u, ok := a.store.Verify(name, pass)
	if !ok {
		a.store.Audit(name, "login_failed", r.RemoteAddr)
		a.renderLogin(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	}
	tok := a.store.CreateSession(u.Name)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 3600,
	})
	a.store.Audit(u.Name, "login", r.RemoteAddr)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if name, ok := a.store.SessionUser(c.Value); ok {
			a.store.Audit(name, "logout", r.RemoteAddr)
		}
		a.store.DropSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) renderLogin(w http.ResponseWriter, errMsg string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = a.tmpl.ExecuteTemplate(w, "login.html", map[string]any{
		"T":    a.T,
		"Err":  errMsg,
		"Lang": a.configSnapshot().Lang,
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
