package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/backup"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

type backupView struct {
	Title   string
	Lang    string
	User    *User
	Cfg     Config
	Version string
	Jobs    []*backup.Job
	Reports map[string]backup.Report
}

func (a *App) backupAuth(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return u, true
}

func (a *App) handleBackupPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		switch r.PostFormValue("action") {
		case "create":
			src := strings.TrimSpace(r.PostFormValue("source"))
			targets := make([]backup.Target, 0, 2)
			if p := strings.TrimSpace(r.PostFormValue("local_path")); p != "" {
				targets = append(targets, backup.Target{Type: "local", Path: p})
			}
			if u := strings.TrimSpace(r.PostFormValue("webdav_url")); u != "" {
				targets = append(targets, backup.Target{
					Type: "webdav", URL: u,
					User: r.PostFormValue("webdav_user"), Password: r.PostFormValue("webdav_pass"),
				})
			}
			name := strings.TrimSpace(r.PostFormValue("name"))
			interval, _ := strconv.Atoi(r.PostFormValue("interval_hours"))
			if _, err := a.backups.Create(name, src, targets, interval); err != nil {
				a.store.Audit(u.Name, "backup_create_failed", err.Error())
			} else {
				a.store.Audit(u.Name, "backup_create", name+" -> "+src)
			}
		case "run":
			id := r.PostFormValue("id")
			if _, err := a.backups.Run(r.Context(), id); err != nil {
				a.store.Audit(u.Name, "backup_run_failed", id+": "+err.Error())
			} else {
				a.store.Audit(u.Name, "backup_run", id)
			}
		case "restore":
			id := r.PostFormValue("id")
			dest := strings.TrimSpace(r.PostFormValue("dest"))
			if _, err := a.backups.Restore(r.Context(), id, dest); err != nil {
				a.store.Audit(u.Name, "backup_restore_failed", id+": "+err.Error())
			} else {
				a.store.Audit(u.Name, "backup_restore", id+" -> "+dest)
			}
		case "delete":
			if a.backups.Delete(r.PostFormValue("id")) {
				a.store.Audit(u.Name, "backup_delete", r.PostFormValue("id"))
			}
		}
		http.Redirect(w, r, "/backup", http.StatusSeeOther)
		return
	}
	reports := map[string]backup.Report{}
	for _, j := range a.backups.List() {
		if rep, ok := a.backups.LastReport(j.ID); ok {
			reports[j.ID] = rep
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "backup.html", &backupView{
		Title: "Бэкап", Lang: a.cfg.Lang, Version: version.Version,
		User: u, Cfg: a.cfg, Jobs: a.backups.List(), Reports: reports,
	})
}

type backupCreateRequest struct {
	Name          string          `json:"name"`
	Source        string          `json:"source"`
	Targets       []backup.Target `json:"targets"`
	IntervalHours int             `json:"interval_hours"`
}

func (a *App) handleBackupJobsAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.backupAuth(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"jobs": a.backups.List()})
	case http.MethodPost:
		var req backupCreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		job, err := a.backups.Create(req.Name, req.Source, req.Targets, req.IntervalHours)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.store.Audit(u.Name, "backup_create", job.Name+" -> "+job.Source)
		writeJSON(w, map[string]any{"job": job})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) handleBackupRunAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.backupAuth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		_ = r.ParseForm()
		id = r.PostFormValue("id")
	}
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	started := time.Now()
	rep, err := a.backups.Run(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.store.Audit(u.Name, "backup_run", id)
	writeJSON(w, map[string]any{"report": rep, "elapsed_ms": time.Since(started).Milliseconds()})
}

func (a *App) handleBackupReportAPI(w http.ResponseWriter, r *http.Request) {
	_, ok := a.backupAuth(w, r)
	if !ok {
		return
	}
	id := r.URL.Query().Get("id")
	rep, found := a.backups.LastReport(id)
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, rep)
}
