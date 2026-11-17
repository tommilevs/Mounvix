package app

import "net/http"

// handleBackupRestoreAPI — восстановление из локальной цели бэкапа в dest.
func (a *App) handleBackupRestoreAPI(w http.ResponseWriter, r *http.Request) {
	u, ok := a.backupAuth(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	dest := r.URL.Query().Get("dest")
	if id == "" || dest == "" {
		_ = r.ParseForm()
		if id == "" {
			id = r.PostFormValue("id")
		}
		if dest == "" {
			dest = r.PostFormValue("dest")
		}
	}
	if id == "" || dest == "" {
		http.Error(w, "id and dest required", http.StatusBadRequest)
		return
	}
	rep, err := a.backups.Restore(r.Context(), id, dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.store.Audit(u.Name, "backup_restore", id+" -> "+dest)
	writeJSON(w, map[string]any{"report": rep})
}
