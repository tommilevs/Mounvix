package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleFeed отдаёт статический opkg-фид из data/feed (Packages.gz, *.ipk).
// Публичный (как и пакеты релиза), без листинга каталога и без выхода за пределы feed.
func (a *App) handleFeed(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/feed/")
	if rel == "" || strings.Contains(rel, "..") || strings.ContainsAny(rel, "\\\x00") {
		http.NotFound(w, r)
		return
	}
	root := filepath.Join(a.cfg.DataDir, "feed")
	full, err := safeJoin(root, rel)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
}
