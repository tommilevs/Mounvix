package app

import (
	"encoding/json"
	"net/http"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

type pluginsView struct {
	Title   string
	Lang    string
	User    *User
	Cfg     Config
	Version string
	Plugins []*plugins.Plugin
	Rclone  plugins.RcloneStatus
	Msg     string
	Err     string
}

func (a *App) handlePluginsPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	v := &pluginsView{Title: "Плагины", Lang: a.cfg.Lang, Version: version.Version, User: u, Cfg: a.cfg}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		id := r.PostFormValue("id")
		on := r.PostFormValue("enabled") == "1"
		if err := a.pluginReg.SetEnabled(id, on); err != nil {
			v.Err = err.Error()
		} else {
			v.Msg = "Плагин обновлён: " + id
			a.store.Audit(u.Name, "plugin_toggle", id)
		}
	}
	v.Plugins = a.pluginReg.List()
	v.Rclone = a.pluginReg.RcloneStatus()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "plugins.html", v)
}

func (a *App) handlePluginsAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"plugins": a.pluginReg.List(), "rclone": a.pluginReg.RcloneStatus()})
	case http.MethodPost:
		var req struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == "" {
			_ = r.ParseForm()
			req.ID = r.PostFormValue("id")
			req.Enabled = r.PostFormValue("enabled") == "1"
		}
		if err := a.pluginReg.SetEnabled(req.ID, req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.store.Audit(u.Name, "plugin_toggle", req.ID)
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
