package app

import (
	"encoding/json"
	"net/http"
	"strings"

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
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	cfg := a.configSnapshot()
	v := &pluginsView{Title: "Плагины", Lang: cfg.Lang, Version: version.Version, User: u, Cfg: cfg}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if r.PostFormValue("action") == "rclone_sync" {
			out, err := a.pluginReg.RcloneSync(r.Context(),
				strings.TrimSpace(r.PostFormValue("src")), strings.TrimSpace(r.PostFormValue("dst")))
			if err != nil {
				v.Err = "rclone: " + err.Error()
			} else {
				v.Msg = "rclone: " + truncate(out, 400)
				a.store.Audit(u.Name, "rclone_sync", r.PostFormValue("src")+" -> "+r.PostFormValue("dst"))
			}
		} else {
			id := r.PostFormValue("id")
			on := r.PostFormValue("enabled") == "1"
			if err := a.pluginReg.SetEnabled(id, on); err != nil {
				v.Err = err.Error()
			} else {
				v.Msg = "Плагин обновлён: " + id
				a.store.Audit(u.Name, "plugin_toggle", id)
			}
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
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"plugins": a.pluginReg.List(), "rclone": a.pluginReg.RcloneStatus()})
	case http.MethodPost:
		var req struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
			Action  string `json:"action"`
			Src     string `json:"src"`
			Dst     string `json:"dst"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == "" && req.Action == "" {
			_ = r.ParseForm()
			req.ID = r.PostFormValue("id")
			req.Enabled = r.PostFormValue("enabled") == "1"
			req.Action = r.PostFormValue("action")
			req.Src = r.PostFormValue("src")
			req.Dst = r.PostFormValue("dst")
		}
		if req.Action == "rclone_sync" {
			out, err := a.pluginReg.RcloneSync(r.Context(), req.Src, req.Dst)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			a.store.Audit(u.Name, "rclone_sync", req.Src+" -> "+req.Dst)
			writeJSON(w, map[string]any{"ok": true, "output": out})
			return
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

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
