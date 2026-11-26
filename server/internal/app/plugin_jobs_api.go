package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

const maxPluginAPIRequestBytes = 64 << 10

type apiErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writePluginAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	var body apiErrorBody
	body.Error.Code = code
	body.Error.Message = message
	_ = json.NewEncoder(w).Encode(body)
}

func writePluginAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *App) requirePluginAdmin(w http.ResponseWriter, r *http.Request) (*User, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	u, ok := a.cookieUser(r)
	if !ok {
		writePluginAPIError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return nil, false
	}
	if !u.IsAdmin() {
		writePluginAPIError(w, http.StatusForbidden, "forbidden", "administrator access required")
		return nil, false
	}
	return u, true
}

func (a *App) handlePluginCatalogAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requirePluginAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	catalog, status := a.currentPluginCatalog(r.Context())
	writePluginAPIJSON(w, http.StatusOK, struct {
		Catalog plugins.CatalogStatus  `json:"catalog"`
		Plugins []plugins.CatalogEntry `json:"plugins"`
	}{Catalog: status, Plugins: catalog.Plugins})
}

var pluginAPIID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type pendingPackageOperation struct {
	RunID     string            `json:"run_id"`
	Operation string            `json:"operation"`
	Status    plugins.JobStatus `json:"status"`
	Version   string            `json:"version,omitempty"`
	record    plugins.JobRecord `json:"-"`
}

type pluginInstallationView struct {
	ID                    string                   `json:"id"`
	Name                  string                   `json:"name"`
	EffectiveSource       plugins.PluginSource     `json:"effective_source"`
	EffectiveEnabled      bool                     `json:"effective_enabled"`
	Available             bool                     `json:"available"`
	PackagePresent        bool                     `json:"package_present"`
	PackageVersion        string                   `json:"package_version,omitempty"`
	PackageEnabled        bool                     `json:"package_enabled"`
	LegacyPresent         bool                     `json:"legacy_present"`
	LegacyVersion         string                   `json:"legacy_version,omitempty"`
	LegacyEnabled         bool                     `json:"legacy_enabled"`
	LegacyFallbackEnabled bool                     `json:"legacy_fallback_enabled"`
	BuiltinPresent        bool                     `json:"builtin_present"`
	BuiltinEnabled        bool                     `json:"builtin_enabled"`
	Shadowed              bool                     `json:"shadowed"`
	PendingOperation      *pendingPackageOperation `json:"pending_operation,omitempty"`
}

type pluginInstallationsResponse struct {
	Catalog plugins.CatalogStatus    `json:"catalog"`
	Items   []pluginInstallationView `json:"items"`
}

func (a *App) handlePluginInstallationsAPI(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requirePluginAdmin(w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/plugin-installations")
	if path == "" || path == "/" {
		if r.Method != http.MethodGet {
			writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		view, err := a.pluginInstallations(r.Context())
		if err != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin installation state is unavailable")
			return
		}
		writePluginAPIJSON(w, http.StatusOK, view)
		return
	}
	id := strings.TrimPrefix(path, "/")
	if id == "" || strings.Contains(id, "/") || !pluginAPIID.MatchString(id) {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin installation not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := a.pluginInstallation(r.Context(), id)
		if errors.Is(err, errPluginInstallationNotFound) {
			writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin installation not found")
			return
		}
		if err != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin installation state is unavailable")
			return
		}
		writePluginAPIJSON(w, http.StatusOK, map[string]any{"item": item})
	case http.MethodPut:
		var request struct {
			Version   string `json:"version"`
			Confirmed *bool  `json:"confirmed"`
		}
		if err := decodePluginAPIJSON(w, r, &request); err != nil {
			writePluginAPIError(w, err.status, err.code, err.message)
			return
		}
		if strings.TrimSpace(request.Version) == "" || request.Confirmed == nil || !*request.Confirmed {
			writePluginAPIError(w, http.StatusBadRequest, "invalid_request", "exact version and explicit confirmation are required")
			return
		}
		if !validPluginIdempotencyKey(r) {
			writePluginAPIError(w, http.StatusBadRequest, "invalid_idempotency_key", "one valid Idempotency-Key header is required")
			return
		}
		item, err := a.pluginInstallation(r.Context(), id)
		if err != nil && !errors.Is(err, errPluginInstallationNotFound) {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin installation state is unavailable")
			return
		}
		if err == nil && item.PackagePresent && item.PackageVersion == request.Version {
			writePluginAPIJSON(w, http.StatusOK, map[string]any{"item": item, "noop": true})
			return
		}
		operation := "install"
		if err == nil && item.PackagePresent {
			operation = "update"
		}
		jobRequest, err := packageInstallRequest(user.Name, id, operation, request.Version, strings.TrimSpace(r.Header.Get("Idempotency-Key")))
		if err != nil {
			writePluginAPIError(w, http.StatusBadRequest, "invalid_request", "package installation request is invalid")
			return
		}
		prior, found, lookupErr := a.jobManager.FindIdempotency(r.Context(), jobRequest)
		if errors.Is(lookupErr, plugins.ErrIdempotencyConflict) {
			writePluginAPIError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for a different request")
			return
		}
		if lookupErr != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "package installation state is unavailable")
			return
		}
		if found {
			writePluginAPIJSON(w, http.StatusAccepted, map[string]any{"job": prior})
			return
		}
		if item.PendingOperation != nil {
			pending := item.PendingOperation
			if (pending.Operation == "install" || pending.Operation == "update") && pending.Version == request.Version {
				writePluginAPIJSON(w, http.StatusAccepted, map[string]any{"job": pending.record})
				return
			}
			writePluginAPIError(w, http.StatusConflict, "package_busy", "a different package operation is already pending")
			return
		}
		activePluginJob, busyErr := a.hasActivePluginJob(r.Context(), id)
		if busyErr != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin job state is unavailable")
			return
		}
		if activePluginJob {
			writePluginAPIError(w, http.StatusConflict, "package_busy", "a plugin job is active")
			return
		}
		catalog, status := a.currentPluginCatalog(r.Context())
		if status.ReadOnly {
			writePluginAPIError(w, http.StatusConflict, "catalog_read_only", "the signed plugin catalog is unavailable; installation changes are disabled")
			return
		}
		if a.packageInstallExecutor == nil {
			writePluginAPIError(w, http.StatusConflict, "installation_unavailable", "package installation is unavailable on this platform")
			return
		}
		format, supported := a.packageInstallExecutor.InstallFormat()
		if !supported || format == "" || !a.packageInstallExecutor.CanInstall() {
			writePluginAPIError(w, http.StatusConflict, "installation_unavailable", "package installation is unavailable on this platform")
			return
		}
		var catalogVersion string
		for _, entry := range catalog.Plugins {
			if entry.ID == id {
				catalogVersion = entry.Version
				break
			}
		}
		if catalogVersion == "" {
			writePluginAPIError(w, http.StatusConflict, "catalog_plugin_unavailable", "plugin is not available in the signed catalog")
			return
		}
		if catalogVersion != request.Version {
			writePluginAPIError(w, http.StatusConflict, "catalog_version_unavailable", "the requested version is not present in the signed catalog")
			return
		}
		asset, assetErr := catalog.SelectAsset(id, format, version.Version, runtime.GOARCH)
		if assetErr != nil || asset.Version != request.Version {
			writePluginAPIError(w, http.StatusConflict, "catalog_asset_unavailable", "no verified package asset matches this platform and version")
			return
		}
		run, enqueueErr := a.jobManager.Enqueue(r.Context(), jobRequest)
		if errors.Is(enqueueErr, plugins.ErrIdempotencyConflict) {
			writePluginAPIError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for a different request")
			return
		}
		if errors.Is(enqueueErr, plugins.ErrJobQueueFull) {
			writePluginAPIError(w, http.StatusServiceUnavailable, "queue_full", "package job queue is full")
			return
		}
		if enqueueErr != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "package installation could not be queued")
			return
		}
		writePluginAPIJSON(w, http.StatusAccepted, map[string]any{"job": run})
	case http.MethodDelete:
		if !validPluginIdempotencyKey(r) {
			writePluginAPIError(w, http.StatusBadRequest, "invalid_idempotency_key", "one valid Idempotency-Key header is required")
			return
		}
		var request struct {
			ConfirmedLegacyFallback *bool `json:"confirmed_legacy_fallback"`
		}
		if err := decodeOptionalPluginAPIJSON(w, r, &request); err != nil {
			writePluginAPIError(w, err.status, err.code, err.message)
			return
		}
		item, err := a.pluginInstallation(r.Context(), id)
		if errors.Is(err, errPluginInstallationNotFound) || (err == nil && !item.PackagePresent) {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin installation state is unavailable")
			return
		}
		if item.PackageEnabled {
			writePluginAPIError(w, http.StatusConflict, "plugin_enabled", "disable the package source before removal")
			return
		}
		if item.PendingOperation != nil {
			if item.PendingOperation.Operation == "remove" {
				prior, found, lookupErr := a.jobManager.FindIdempotency(r.Context(), packageRemovalRequest(user.Name, id, strings.TrimSpace(r.Header.Get("Idempotency-Key")), request.ConfirmedLegacyFallback))
				if errors.Is(lookupErr, plugins.ErrIdempotencyConflict) {
					writePluginAPIError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for a different request")
					return
				}
				if lookupErr != nil {
					writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "package removal state is unavailable")
					return
				}
				if found {
					writePluginAPIJSON(w, http.StatusAccepted, map[string]any{"job": prior})
					return
				}
			}
			writePluginAPIError(w, http.StatusConflict, "package_busy", "a package operation is already pending")
			return
		}
		activePluginJob, busyErr := a.hasActivePluginJob(r.Context(), id)
		if busyErr != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin job state is unavailable")
			return
		}
		if activePluginJob {
			writePluginAPIError(w, http.StatusConflict, "package_busy", "a plugin job is active")
			return
		}
		if item.LegacyFallbackEnabled && (request.ConfirmedLegacyFallback == nil || !*request.ConfirmedLegacyFallback) {
			writePluginAPIError(w, http.StatusConflict, "legacy_fallback_confirmation_required", "explicit confirmation is required to expose the enabled legacy fallback")
			return
		}
		if a.packageInstaller == nil || !a.packageInstaller.CanRemove() {
			writePluginAPIError(w, http.StatusConflict, "removal_unavailable", "validated package removal is unavailable on this platform")
			return
		}
		run, err := a.jobManager.Enqueue(r.Context(), packageRemovalRequest(user.Name, id, strings.TrimSpace(r.Header.Get("Idempotency-Key")), request.ConfirmedLegacyFallback))
		if err != nil {
			if errors.Is(err, plugins.ErrIdempotencyConflict) {
				writePluginAPIError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for a different request")
				return
			}
			if errors.Is(err, plugins.ErrJobQueueFull) {
				writePluginAPIError(w, http.StatusServiceUnavailable, "queue_full", "package job queue is full")
				return
			}
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "package removal could not be queued")
			return
		}
		writePluginAPIJSON(w, http.StatusAccepted, map[string]any{"job": run})
	default:
		writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	_ = user
}

var errPluginInstallationNotFound = errors.New("plugin installation not found")

func (a *App) pluginInstallations(ctx context.Context) (pluginInstallationsResponse, error) {
	page, err := a.jobStore.ListAfter(ctx, 0, 1000)
	if err != nil {
		return pluginInstallationsResponse{}, err
	}
	pending := map[string]*pendingPackageOperation{}
	for _, run := range page.Items {
		if run.Kind != "package" || (run.Status != plugins.JobStatusQueued && run.Status != plugins.JobStatusRunning) {
			continue
		}
		if run.Operation == "install" || run.Operation == "update" {
			fullRun, found, lookupErr := a.jobStore.GetPendingPackageOperation(ctx, run.RunID)
			if lookupErr != nil {
				return pluginInstallationsResponse{}, lookupErr
			}
			if !found {
				continue
			}
			run = fullRun
		}
		id := run.PluginID
		if id == "" {
			id = run.JobID
		}
		if pluginAPIID.MatchString(id) {
			var packageVersion string
			if run.Operation == "install" || run.Operation == "update" {
				var request struct {
					Version string `json:"version"`
				}
				if json.Unmarshal(run.Payload, &request) == nil {
					packageVersion = request.Version
				}
			}
			pending[id] = &pendingPackageOperation{RunID: run.RunID, Operation: run.Operation, Status: run.Status, Version: packageVersion, record: run}
		}
	}
	items := a.pluginReg.List()
	views := make([]pluginInstallationView, 0, len(items)+len(pending))
	seen := make(map[string]struct{}, len(items))
	for _, effective := range items {
		seen[effective.ID] = struct{}{}
		view := pluginInstallationView{ID: effective.ID, Name: effective.Name, EffectiveSource: effective.Source, EffectiveEnabled: effective.Enabled, Available: effective.Available}
		if packagePlugin, ok := a.pluginReg.LookupSource(effective.ID, plugins.SourcePackage); ok {
			view.PackagePresent = true
			view.PackageVersion = packagePlugin.Version
			view.PackageEnabled = packagePlugin.Enabled
		}
		if legacyPlugin, ok := a.pluginReg.LookupSource(effective.ID, plugins.SourceLegacy); ok {
			view.LegacyPresent = true
			view.LegacyVersion = legacyPlugin.Version
			view.LegacyEnabled = legacyPlugin.Enabled
		}
		if builtinPlugin, ok := a.pluginReg.LookupSource(effective.ID, plugins.SourceBuiltin); ok {
			view.BuiltinPresent = true
			view.BuiltinEnabled = builtinPlugin.Enabled
		}
		view.Shadowed = view.PackagePresent && view.LegacyPresent
		view.LegacyFallbackEnabled = view.Shadowed && view.LegacyEnabled
		view.PendingOperation = pending[effective.ID]
		views = append(views, view)
	}
	pendingIDs := make([]string, 0, len(pending))
	for id := range pending {
		if _, exists := seen[id]; !exists {
			pendingIDs = append(pendingIDs, id)
		}
	}
	sort.Strings(pendingIDs)
	for _, id := range pendingIDs {
		views = append(views, pluginInstallationView{ID: id, PendingOperation: pending[id]})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	_, catalogStatus := a.currentPluginCatalog(ctx)
	return pluginInstallationsResponse{Catalog: catalogStatus, Items: views}, nil
}

func (a *App) pluginInstallation(ctx context.Context, id string) (pluginInstallationView, error) {
	response, err := a.pluginInstallations(ctx)
	if err != nil {
		return pluginInstallationView{}, err
	}
	for _, item := range response.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return pluginInstallationView{}, errPluginInstallationNotFound
}

func (a *App) executePackageInstall(ctx context.Context, job plugins.JobRecord) (string, error) {
	if a == nil || ctx == nil {
		return "", errors.New("package install context is unavailable")
	}
	if job.Operation != "install" && job.Operation != "update" {
		return "", errors.New("unsupported package install operation")
	}
	pluginID := job.PluginID
	if pluginID == "" {
		pluginID = job.JobID
	}
	if !pluginAPIID.MatchString(pluginID) || pluginID == "rclone" {
		return "", errors.New("invalid package plugin id")
	}
	var request struct {
		Version   string `json:"version"`
		Confirmed *bool  `json:"confirmed"`
	}
	if err := json.Unmarshal(job.Payload, &request); err != nil || strings.TrimSpace(request.Version) == "" || request.Confirmed == nil || !*request.Confirmed {
		return "", errors.New("invalid package install request")
	}
	a.pluginOperationMu.Lock()
	defer a.pluginOperationMu.Unlock()
	catalog, status := a.currentPluginCatalog(ctx)
	if status.ReadOnly || !catalog.Verified() {
		return "", errors.New("signed plugin catalog is unavailable")
	}
	if a.packageInstallExecutor == nil {
		return "", errors.New("package installation is unavailable on this platform")
	}
	format, supported := a.packageInstallExecutor.InstallFormat()
	if !supported || format == "" || !a.packageInstallExecutor.CanInstall() {
		return "", errors.New("package installation is unavailable on this platform")
	}
	var catalogVersion string
	for _, entry := range catalog.Plugins {
		if entry.ID == pluginID {
			catalogVersion = entry.Version
			break
		}
	}
	if catalogVersion == "" || catalogVersion != request.Version {
		return "", errors.New("requested package version is not present in the signed catalog")
	}
	asset, err := catalog.SelectAsset(pluginID, format, version.Version, runtime.GOARCH)
	if err != nil || asset.Version != request.Version {
		return "", errors.New("verified package asset is unavailable for this platform and version")
	}
	packagePlugin, packagePresent := a.pluginReg.LookupSource(pluginID, plugins.SourcePackage)
	if packagePresent && packagePlugin.Version == request.Version {
		return "package already at requested version", nil
	}
	if job.Operation == "install" && packagePresent {
		return "", errors.New("package already exists; update operation is required")
	}
	if job.Operation == "update" && !packagePresent {
		return "", errors.New("package is not installed")
	}
	wasEnabled := packagePresent && packagePlugin.Enabled
	if err := a.jobManager.DisablePlugin(ctx, pluginID); err != nil {
		return "", fmt.Errorf("stop plugin jobs before package install: %w", err)
	}
	if err := a.packageInstallExecutor.InstallCatalogPackage(ctx, pluginID, asset); err != nil {
		installErr := fmt.Errorf("install verified package: %w", err)
		if wasEnabled {
			if disableErr := a.pluginReg.SetSourceEnabledAs("system", pluginID, plugins.SourcePackage, false); disableErr != nil {
				installErr = errors.Join(installErr, fmt.Errorf("persist disabled package state after failed update: %w", disableErr))
			} else {
				installErr = fmt.Errorf("%w; package disabled after failed update", installErr)
			}
		}
		return "", installErr
	}
	if err := a.pluginReg.ReloadPackages(); err != nil {
		return "package installed but plugin registry refresh failed", fmt.Errorf("refresh package registry: %w", err)
	}
	if wasEnabled {
		a.jobManager.EnablePlugin(pluginID)
	}
	return fmt.Sprintf("package %s at version %s", job.Operation, request.Version), nil
}

func (a *App) hasActivePluginJob(ctx context.Context, pluginID string) (bool, error) {
	if a.jobStore == nil {
		return false, errors.New("plugin job journal is unavailable")
	}
	page, err := a.jobStore.ListAfter(ctx, 0, 1000)
	if err != nil {
		return false, err
	}
	for _, run := range page.Items {
		if run.Kind == "plugin" && run.PluginID == pluginID && (run.Status == plugins.JobStatusQueued || run.Status == plugins.JobStatusRunning) {
			return true, nil
		}
	}
	return false, nil
}

func packageRemovalRequest(actor, id, idempotencyKey string, confirmed *bool) plugins.JobRequest {
	payload := json.RawMessage(`{"confirmed_legacy_fallback":false}`)
	if confirmed != nil && *confirmed {
		payload = json.RawMessage(`{"confirmed_legacy_fallback":true}`)
	}
	return plugins.JobRequest{ActorID: actor, Kind: "package", PluginID: id, JobID: id, Operation: "remove", Payload: payload, IdempotencyKey: idempotencyKey}
}

func packageInstallRequest(actor, id, operation, packageVersion, idempotencyKey string) (plugins.JobRequest, error) {
	payload, err := json.Marshal(struct {
		Version   string `json:"version"`
		Confirmed bool   `json:"confirmed"`
	}{Version: packageVersion, Confirmed: true})
	if err != nil {
		return plugins.JobRequest{}, fmt.Errorf("encode package installation request: %w", err)
	}
	return plugins.JobRequest{ActorID: actor, Kind: "package", PluginID: id, JobID: id, Operation: operation, Payload: payload, IdempotencyKey: idempotencyKey}, nil
}

func validPluginIdempotencyKey(r *http.Request) bool {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return false
	}
	key := strings.TrimSpace(values[0])
	if key == "" || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

var pluginRunID = regexp.MustCompile(`^run-[a-f0-9]{16,64}$`)

func (a *App) handlePluginJobsAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requirePluginAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	page, err := a.pluginJobsPage(r)
	if err != nil {
		writePluginAPIError(w, http.StatusBadRequest, "invalid_cursor", "job cursor or page size is invalid")
		return
	}
	writePluginAPIJSON(w, http.StatusOK, page)
}

func (a *App) pluginJobsPage(r *http.Request) (plugins.JobPage, error) {
	values := r.URL.Query()
	limit := 50
	if provided, ok := values["limit"]; ok {
		if len(provided) != 1 {
			return plugins.JobPage{}, errors.New("multiple page limits")
		}
		parsed, err := strconv.Atoi(provided[0])
		if err != nil || parsed < 1 || parsed > 100 {
			return plugins.JobPage{}, errors.New("page limit out of range")
		}
		limit = parsed
	}
	if afterValues, present := values["after"]; present {
		if len(afterValues) != 1 {
			return plugins.JobPage{}, errors.New("multiple cursors")
		}
		after, err := strconv.ParseUint(afterValues[0], 10, 64)
		if err != nil {
			return plugins.JobPage{}, errors.New("invalid cursor")
		}
		page, err := a.jobStore.ListAfter(r.Context(), after, limit)
		if err != nil {
			return plugins.JobPage{}, err
		}
		if after > page.LatestSeq {
			return plugins.JobPage{}, errors.New("cursor is ahead of journal")
		}
		return page, nil
	}
	page, err := a.jobStore.ListAfter(r.Context(), 0, 1000)
	if err != nil {
		return plugins.JobPage{}, err
	}
	page.Reset = false
	if len(page.Items) > limit {
		page.Items = page.Items[len(page.Items)-limit:]
	}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].Seq
	} else {
		page.NextCursor = page.LatestSeq
	}
	return page, nil
}

func (a *App) handlePluginJobDetailAPI(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requirePluginAdmin(w, r)
	if !ok {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
	parts := strings.Split(tail, "/")
	if len(parts) == 0 || !pluginRunID.MatchString(parts[0]) {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	run, err := a.findPluginJob(r.Context(), parts[0])
	if errors.Is(err, errPluginJobNotFound) {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	if err != nil {
		writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "job journal is unavailable")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		writePluginAPIJSON(w, http.StatusOK, map[string]any{"job": run})
		return
	}
	if len(parts) != 2 {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "job resource not found")
		return
	}
	switch {
	case parts[1] == "logs" && r.Method == http.MethodGet:
		a.handlePluginJobLogs(w, run)
	case parts[1] == "cancellations" && r.Method == http.MethodPost:
		a.handlePluginJobCancelAPI(w, r, user, run)
	default:
		writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

var errPluginJobNotFound = errors.New("plugin job not found")

func (a *App) findPluginJob(ctx context.Context, runID string) (plugins.JobRecord, error) {
	page, err := a.jobStore.ListAfter(ctx, 0, 1000)
	if err != nil {
		return plugins.JobRecord{}, err
	}
	for _, item := range page.Items {
		if item.RunID == runID {
			return item, nil
		}
	}
	return plugins.JobRecord{}, errPluginJobNotFound
}

func (a *App) handlePluginJobLogs(w http.ResponseWriter, run plugins.JobRecord) {
	if run.Kind == "package" {
		writePluginAPIJSON(w, http.StatusNotFound, map[string]any{
			"error":   map[string]string{"code": "log_unavailable", "message": "package job logs are unavailable"},
			"summary": run.Summary,
		})
		return
	}
	kind, pluginID := "plugins", run.PluginID
	if run.Kind == "rclone" {
		kind, pluginID = "rclone", "rclone"
	} else if run.Kind != "plugin" {
		writePluginAPIError(w, http.StatusNotFound, "log_unavailable", "logs are unavailable for this job")
		return
	}
	logs := plugins.NewLogStore(a.configSnapshot().DataDir)
	contents, err := logs.ReadRun(kind, pluginID, run.RunID)
	if err != nil {
		writePluginAPIError(w, http.StatusNotFound, "log_unavailable", "logs are unavailable for this job")
		return
	}
	writePluginAPIJSON(w, http.StatusOK, map[string]any{"run_id": run.RunID, "log": string(contents)})
}

func (a *App) handlePluginJobCancelAPI(w http.ResponseWriter, r *http.Request, user *User, run plugins.JobRecord) {
	pluginID := run.PluginID
	if run.Kind == "rclone" {
		pluginID = "rclone"
	} else if run.Kind == "package" && pluginID == "" {
		pluginID = run.JobID
	}
	intent := plugins.AuditEvent{Actor: user.Name, Phase: "intent", Action: "cancel", PluginID: pluginID, RunID: run.RunID, Outcome: "requested"}
	if err := a.pluginReg.RecordAudit(intent); err != nil {
		writePluginAPIError(w, http.StatusServiceUnavailable, "audit_unavailable", "cancellation intent could not be recorded")
		return
	}
	if err := a.jobManager.Cancel(r.Context(), run.RunID); err != nil {
		result := intent
		result.Phase = "result"
		result.Outcome = "failed"
		if auditErr := a.pluginReg.RecordAudit(result); auditErr != nil {
			writePluginAPIError(w, http.StatusServiceUnavailable, "audit_unavailable", "cancellation result could not be recorded")
			return
		}
		writePluginAPIError(w, http.StatusServiceUnavailable, "cancel_failed", "job cancellation did not complete")
		return
	}
	current, err := a.findPluginJob(r.Context(), run.RunID)
	if err != nil {
		writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "job journal is unavailable")
		return
	}
	result := intent
	result.Phase = "result"
	result.Outcome = string(current.Status)
	if err := a.pluginReg.RecordAudit(result); err != nil {
		writePluginAPIError(w, http.StatusServiceUnavailable, "audit_unavailable", "cancellation result could not be recorded")
		return
	}
	writePluginAPIJSON(w, http.StatusOK, map[string]any{"job": current})
}

type pluginSourcePatch struct {
	Source  plugins.PluginSource `json:"source"`
	Enabled *bool                `json:"enabled"`
}

type pluginSourceView struct {
	ID        string               `json:"id"`
	Name      string               `json:"name"`
	Version   string               `json:"version"`
	Source    plugins.PluginSource `json:"source"`
	Enabled   bool                 `json:"enabled"`
	Available bool                 `json:"available"`
}

func pluginAPIContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (a *App) handlePluginJobRunAPI(w http.ResponseWriter, r *http.Request) {
	user, ok := a.requirePluginAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/"), "/")
	if len(parts) != 4 || !pluginAPIID.MatchString(parts[0]) || parts[1] != "jobs" || !pluginAPIID.MatchString(parts[2]) || parts[3] != "runs" {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin job not found")
		return
	}
	pluginID, jobID := parts[0], parts[2]
	plugin, exists := a.pluginReg.LookupSource(pluginID, plugins.SourcePackage)
	if !exists || plugin.Manifest == nil {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin job not found")
		return
	}
	if !plugin.Enabled || !plugin.Available {
		writePluginAPIError(w, http.StatusConflict, "plugin_unavailable", "plugin jobs are not available")
		return
	}
	if !pluginAPIContains(plugin.Manifest.RuntimeMethods, "plugin.runJob") {
		writePluginAPIError(w, http.StatusConflict, "unsupported_method", "plugin does not expose job execution")
		return
	}
	jobFound := false
	for _, job := range plugin.Manifest.Jobs {
		if job.ID == jobID {
			jobFound = true
			break
		}
	}
	if !jobFound {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin job not found")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPluginAPIRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writePluginAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		} else {
			writePluginAPIError(w, http.StatusBadRequest, "invalid_request", "request body could not be read")
		}
		return
	}
	if len(body) != 0 {
		writePluginAPIError(w, http.StatusBadRequest, "invalid_request", "plugin job run requests must have an empty body")
		return
	}
	if !validPluginIdempotencyKey(r) {
		writePluginAPIError(w, http.StatusBadRequest, "invalid_idempotency_key", "one valid Idempotency-Key header is required")
		return
	}
	payload, err := json.Marshal(struct {
		Method string `json:"method"`
		Params struct {
			JobID string `json:"job_id"`
		} `json:"params"`
	}{Method: "plugin.runJob", Params: struct {
		JobID string `json:"job_id"`
	}{JobID: jobID}})
	if err != nil {
		writePluginAPIError(w, http.StatusInternalServerError, "internal_error", "plugin job request could not be encoded")
		return
	}
	run, err := a.jobManager.Enqueue(r.Context(), plugins.JobRequest{ActorID: user.Name, Kind: "plugin", PluginID: pluginID, JobID: jobID, Operation: "run", Payload: payload, IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key"))})
	if err != nil {
		switch {
		case errors.Is(err, plugins.ErrIdempotencyConflict):
			writePluginAPIError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key was already used for a different request")
		case errors.Is(err, plugins.ErrPluginJobsDisabled):
			writePluginAPIError(w, http.StatusConflict, "plugin_disabled", "plugin jobs are disabled")
		case errors.Is(err, plugins.ErrJobQueueFull):
			writePluginAPIError(w, http.StatusServiceUnavailable, "queue_full", "plugin job queue is full")
		default:
			writePluginAPIError(w, http.StatusServiceUnavailable, "unavailable", "plugin job could not be queued")
		}
		return
	}
	writePluginAPIJSON(w, http.StatusAccepted, map[string]any{"job": run})
}

func (a *App) handlePluginSourceAPI(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/"), "/jobs/") {
		a.handlePluginJobRunAPI(w, r)
		return
	}
	user, ok := a.requirePluginAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPatch {
		writePluginAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/")
	if id == "" || strings.Contains(id, "/") {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin not found")
		return
	}
	if _, found := a.pluginReg.LookupSource(id, plugins.SourcePackage); !found {
		if _, found = a.pluginReg.LookupSource(id, plugins.SourceLegacy); !found {
			if _, found = a.pluginReg.LookupSource(id, plugins.SourceBuiltin); !found {
				writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin not found")
				return
			}
		}
	}
	var request pluginSourcePatch
	if err := decodePluginAPIJSON(w, r, &request); err != nil {
		writePluginAPIError(w, err.status, err.code, err.message)
		return
	}
	if request.Enabled == nil || (request.Source != plugins.SourcePackage && request.Source != plugins.SourceLegacy && request.Source != plugins.SourceBuiltin) {
		writePluginAPIError(w, http.StatusBadRequest, "invalid_request", "source and enabled are required")
		return
	}
	plugin, exists := a.pluginReg.LookupSource(id, request.Source)
	if !exists {
		writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin source not found")
		return
	}
	if err := a.setPluginSourceEnabled(r.Context(), user.Name, id, request.Source, *request.Enabled); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writePluginAPIError(w, http.StatusNotFound, "not_found", "plugin source not found")
			return
		}
		writePluginAPIError(w, http.StatusConflict, "state_conflict", "plugin state could not be changed")
		return
	}
	view := pluginSourceView{ID: plugin.ID, Name: plugin.Name, Version: plugin.Version, Source: request.Source, Enabled: *request.Enabled, Available: plugin.Available}
	writePluginAPIJSON(w, http.StatusOK, map[string]any{"plugin": view})
}

type pluginAPIError struct {
	status  int
	code    string
	message string
}

func decodeOptionalPluginAPIJSON(w http.ResponseWriter, r *http.Request, target any) *pluginAPIError {
	r.Body = http.MaxBytesReader(w, r.Body, maxPluginAPIRequestBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &pluginAPIError{status: http.StatusRequestEntityTooLarge, code: "request_too_large", message: "request body is too large"}
		}
		return &pluginAPIError{status: http.StatusBadRequest, code: "invalid_request", message: "request body could not be read"}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &pluginAPIError{status: http.StatusBadRequest, code: "invalid_request", message: "invalid JSON request"}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return &pluginAPIError{status: http.StatusBadRequest, code: "invalid_request", message: "request must contain one JSON value"}
	}
	return nil
}

func decodePluginAPIJSON(w http.ResponseWriter, r *http.Request, target any) *pluginAPIError {
	r.Body = http.MaxBytesReader(w, r.Body, maxPluginAPIRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &pluginAPIError{status: http.StatusRequestEntityTooLarge, code: "request_too_large", message: "request body is too large"}
		}
		if errors.Is(err, io.EOF) {
			return &pluginAPIError{status: http.StatusBadRequest, code: "invalid_request", message: "JSON body is required"}
		}
		return &pluginAPIError{status: http.StatusBadRequest, code: "invalid_request", message: "invalid JSON request"}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return &pluginAPIError{status: http.StatusBadRequest, code: "invalid_request", message: "request must contain one JSON value"}
	}
	return nil
}
