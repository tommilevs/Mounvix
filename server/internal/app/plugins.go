package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

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

func (a *App) setPluginEnabled(actor, id string, enabled bool) error {
	if err := a.pluginReg.SetEnabledAs(actor, id, enabled); err != nil {
		return err
	}
	if enabled {
		a.jobManager.EnablePlugin(id)
		return nil
	}
	return a.jobManager.DisablePlugin(context.Background(), id)
}

func (a *App) handlePluginsPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, a.routePath("/login"), http.StatusSeeOther)
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
			v.Err = "rclone sync must be submitted as a confirmed asynchronous job using the configured source root"
		} else {
			id := r.PostFormValue("id")
			on := r.PostFormValue("enabled") == "1"
			if err := a.setPluginEnabled(u.Name, id, on); err != nil {
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
			ID              string `json:"id"`
			Enabled         bool   `json:"enabled"`
			Action          string `json:"action"`
			SourcePath      string `json:"source_path"`
			RemoteName      string `json:"remote_name"`
			DestinationPath string `json:"destination_path"`
			Confirmed       bool   `json:"confirmed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid JSON request", http.StatusBadRequest)
			return
		}
		if req.ID == "" && req.Action == "" {
			_ = r.ParseForm()
			req.ID = r.PostFormValue("id")
			req.Enabled = r.PostFormValue("enabled") == "1"
			req.Action = r.PostFormValue("action")
			req.SourcePath = r.PostFormValue("source_path")
			req.RemoteName = r.PostFormValue("remote_name")
			req.DestinationPath = r.PostFormValue("destination_path")
			req.Confirmed = r.PostFormValue("confirmed") == "1"
		}
		if req.Action == "rclone_sync" {
			record, err := a.enqueueRcloneJob(r.Context(), u.Name, plugins.RcloneJobRequest{SourcePath: req.SourcePath, RemoteName: req.RemoteName, DestinationPath: req.DestinationPath, Confirmed: req.Confirmed})
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			a.store.Audit(u.Name, "rclone_sync", record.RunID)
			w.WriteHeader(http.StatusAccepted)
			writeJSON(w, map[string]any{"job": record})
			return
		}
		if err := a.setPluginEnabled(u.Name, req.ID, req.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.store.Audit(u.Name, "plugin_toggle", req.ID)
		writeJSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) reconcileJobOutcomes(ctx context.Context) error {
	if a.jobStore == nil {
		return nil
	}
	page, err := a.jobStore.ListAfter(ctx, 0, 1000)
	if err != nil {
		return fmt.Errorf("read terminal plugin jobs: %w", err)
	}
	var result error
	for _, run := range page.Items {
		if !terminalPluginRun(run) {
			continue
		}
		if a.jobStore != nil && run.ActorID == "" {
			if actor, ok := a.jobStore.ActorForRun(run.RunID); ok {
				run.ActorID = actor
			}
		}
		if err := a.publishJobOutcome(run); err != nil {
			result = errors.Join(result, fmt.Errorf("reconcile job outcome %s: %w", run.RunID, err))
		}
	}
	return result
}

func terminalPluginRun(run plugins.JobRecord) bool {
	return run.Status == plugins.JobStatusSucceeded || run.Status == plugins.JobStatusFailed || run.Status == plugins.JobStatusCancelled || run.Status == plugins.JobStatusInterrupted
}

func auditedJobOperation(run plugins.JobRecord) (action, pluginID string) {
	switch run.Kind {
	case "plugin":
		return "run", run.PluginID
	case "rclone":
		return "run", "rclone"
	case "package":
		switch run.Operation {
		case "install", "update", "remove":
			pluginID = run.PluginID
			if pluginID == "" {
				pluginID = run.JobID
			}
			return run.Operation, pluginID
		}
	}
	return "", ""
}

func (a *App) publishJobOutcome(run plugins.JobRecord) error {
	var result error
	if a.pluginReg != nil {
		action, pluginID := auditedJobOperation(run)
		if action != "" && pluginID != "" {
			if run.ActorID == "" && a.jobStore != nil {
				run.ActorID, _ = a.jobStore.ActorForRun(run.RunID)
			}
			if run.ActorID != "" {
				if err := a.pluginReg.RecordAudit(plugins.AuditEvent{Actor: run.ActorID, Phase: "result", Action: action, PluginID: pluginID, RunID: run.RunID, Outcome: string(run.Status)}); err != nil {
					result = errors.Join(result, fmt.Errorf("audit job outcome %s: %w", run.RunID, err))
				}
			} else {
				result = errors.Join(result, fmt.Errorf("audit job outcome %s: actor unavailable", run.RunID))
			}
		}
	}
	if a.notifications != nil {
		if _, err := a.notifications.AppendJobOutcome(run); err != nil {
			result = errors.Join(result, fmt.Errorf("publish job notification %s: %w", run.RunID, err))
		}
	}
	kind := terminalHookKind(run)
	if kind == "" || a.hookStore == nil {
		return result
	}
	sum := sha256.Sum256([]byte(run.RunID + "\x00" + string(run.Status) + "\x00" + run.Outcome))
	event := plugins.HookEvent{
		EventID:  "job-" + hex.EncodeToString(sum[:16]),
		Kind:     kind,
		RunID:    run.RunID,
		PluginID: run.PluginID,
		Payload:  map[string]any{"run_id": run.RunID, "job_id": run.JobID, "plugin_id": run.PluginID, "outcome": string(run.Status)},
	}
	if err := a.hookStore.Append(event, a.hookSubscribers(kind, run.PluginID)); err != nil {
		return errors.Join(result, fmt.Errorf("persist job hook %s: %w", run.RunID, err))
	}
	if err := a.dispatchHook(event); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

func terminalHookKind(run plugins.JobRecord) string {
	outcome := string(run.Status)
	if outcome == string(plugins.JobStatusInterrupted) {
		outcome = string(plugins.JobStatusFailed)
	}
	switch run.Kind {
	case "plugin":
		return "plugin.job." + outcome
	case "rclone":
		return "rclone.sync." + outcome
	default:
		return ""
	}
}

func (a *App) hookSubscribers(kind, excludeID string) []string {
	var ids []string
	for _, plugin := range a.pluginReg.List() {
		if !plugin.Enabled || plugin.Manifest == nil || plugin.ID == excludeID {
			continue
		}
		for _, hook := range plugin.Manifest.Hooks {
			if hook == kind {
				ids = append(ids, plugin.ID)
				break
			}
		}
	}
	return ids
}

func (a *App) publishHook(event plugins.HookEvent) {
	if a.hookStore == nil {
		return
	}
	if err := a.hookStore.Append(event, a.hookSubscribers(event.Kind, event.PluginID)); err != nil {
		log.Printf("mounvix plugins: persist hook %s: %v", event.Kind, err)
		return
	}
	if err := a.dispatchHook(event); err != nil {
		log.Printf("mounvix plugins: dispatch hook %s: %v", event.Kind, err)
	}
}

func (a *App) dispatchHook(event plugins.HookEvent) error {
	if a.hookStore == nil || a.runtime == nil {
		return nil
	}
	var result error
	for _, plugin := range a.pluginReg.List() {
		if !plugin.Enabled || plugin.Manifest == nil || plugin.ID == event.PluginID {
			continue
		}
		pending, err := a.hookStore.Pending(plugin.ID)
		if err != nil {
			return err
		}
		for _, delivery := range pending {
			params, err := json.Marshal(map[string]any{"event_id": delivery.Event.EventID, "kind": delivery.Event.Kind, "created_at": delivery.Event.CreatedAt, "payload": delivery.Event.Payload, "run_id": delivery.Event.RunID})
			if err != nil {
				result = errors.Join(result, err)
				continue
			}
			parent := a.ctx
			if parent == nil {
				parent = context.Background()
			}
			ctx, cancel := context.WithTimeout(parent, 30*time.Second)
			_, callErr := a.runtime.Call(ctx, plugin.ID, "plugin.handleEvent", params)
			cancel()
			if callErr != nil {
				result = errors.Join(result, fmt.Errorf("deliver hook %s to %s: %w", delivery.Event.EventID, plugin.ID, callErr))
				continue
			}
			if err := a.hookStore.MarkDelivered(delivery.Event.EventID, plugin.ID); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}

func newHookEventID(kind string) string {
	sum := sha256.Sum256([]byte(kind + "\\x00" + time.Now().UTC().Format(time.RFC3339Nano)))
	return "hook-" + hex.EncodeToString(sum[:16])
}

func (a *App) enqueueRcloneJob(ctx context.Context, actor string, request plugins.RcloneJobRequest) (plugins.JobRecord, error) {
	if strings.TrimSpace(a.rcloneAdapter.SourceRoot) == "" {
		return plugins.JobRecord{}, errors.New("rclone source root is not configured")
	}
	remotes, err := a.rcloneAdapter.ListRemotes(ctx)
	if err != nil {
		return plugins.JobRecord{}, err
	}
	if _, _, err := plugins.ValidateRcloneJobRequest(a.rcloneAdapter.SourceRoot, remotes, request); err != nil {
		return plugins.JobRecord{}, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return plugins.JobRecord{}, fmt.Errorf("encode rclone request: %w", err)
	}
	return a.jobManager.Enqueue(ctx, plugins.JobRequest{ActorID: actor, Kind: "rclone", JobID: "rclone.sync", Operation: "sync", Payload: payload})
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
