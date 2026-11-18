package app

import (
	"embed"
	"net/http"
)

//go:embed assets/panel.css assets/theme.js
var panelAssets embed.FS

func (a *App) handleStyle(w http.ResponseWriter, r *http.Request) {
	servePanelAsset(w, "assets/panel.css", "text/css; charset=utf-8")
}

func (a *App) handleThemeScript(w http.ResponseWriter, r *http.Request) {
	servePanelAsset(w, "assets/theme.js", "text/javascript; charset=utf-8")
}

func servePanelAsset(w http.ResponseWriter, name, contentType string) {
	asset, err := panelAssets.ReadFile(name)
	if err != nil {
		http.Error(w, "panel asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(asset)
}
