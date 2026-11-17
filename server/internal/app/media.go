package app

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tommilevs/Mounvix/server/internal/media"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

type mediaEntry struct {
	Name     string
	Path     string
	ThumbURL string
	FileURL  string
	IsVideo  bool
}

type mediaView struct {
	Title   string
	Lang    string
	User    *User
	Cfg     Config
	Version string
	Root    string
	Path    string
	Entries []mediaEntry
}

func (a *App) mediaAuth(w http.ResponseWriter, r *http.Request) (*User, bool) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return u, true
}

func (a *App) resolveUserPath(w http.ResponseWriter, r *http.Request, u *User) (string, Root, string, bool) {
	rootName := r.URL.Query().Get("root")
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	root, ok := a.findRoot(rootName)
	if !ok {
		http.NotFound(w, r)
		return "", Root{}, "", false
	}
	if a.userPerm(u, rootName) == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", Root{}, "", false
	}
	full, err := safeJoin(root.Path, path)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", Root{}, "", false
	}
	return rootName, root, full, true
}

func (a *App) handleThumb(w http.ResponseWriter, r *http.Request) {
	u, ok := a.mediaAuth(w, r)
	if !ok {
		return
	}
	rootName, _, full, ok := a.resolveUserPath(w, r, u)
	if !ok {
		return
	}
	if !media.Supported(full) {
		http.Error(w, "unsupported media", http.StatusUnsupportedMediaType)
		return
	}
	cacheDir := filepath.Join(a.cfg.DataDir, "cache", "thumbs")
	maxW := 512
	if v := r.URL.Query().Get("w"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 64 && n <= 1024 {
			maxW = n
		}
	}
	out := media.CachePath(cacheDir, rootName, filepath.ToSlash(r.URL.Query().Get("path")), maxW)
	if _, err := os.Stat(out); err != nil {
		if _, err := media.Generate(full, out, maxW); err != nil {
			http.Error(w, "thumb failed", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, out)
}

func (a *App) handleFileStream(w http.ResponseWriter, r *http.Request) {
	u, ok := a.mediaAuth(w, r)
	if !ok {
		return
	}
	_, _, full, ok := a.resolveUserPath(w, r, u)
	if !ok {
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
	if ct := mimeByExt(full); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
}

func (a *App) handleMediaPage(w http.ResponseWriter, r *http.Request) {
	u, ok := a.cookieUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	v := &mediaView{Title: "Медиа", Lang: a.cfg.Lang, User: u, Cfg: a.cfg, Version: version.Version}
	rootName := r.URL.Query().Get("root")
	if rootName == "" && len(a.cfg.Roots) > 0 {
		rootName = a.cfg.Roots[0].Name
	}
	v.Root = rootName
	v.Path = strings.TrimSpace(r.URL.Query().Get("path"))
	if root, ok := a.findRoot(rootName); ok {
		if full, err := safeJoin(root.Path, v.Path); err == nil {
			if entries, err := os.ReadDir(full); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					child := filepath.ToSlash(filepath.Join(v.Path, e.Name()))
					isVideo := media.IsVideo(child)
					if !media.Supported(child) && !isVideo {
						continue
					}
					q := url.Values{"root": {rootName}, "path": {child}}
					v.Entries = append(v.Entries, mediaEntry{
						Name:     e.Name(),
						Path:     child,
						IsVideo:  isVideo,
						FileURL:  "/api/v1/file?" + q.Encode(),
						ThumbURL: "/api/v1/thumbs?" + q.Encode() + "&w=300",
					})
				}
			}
		}
	}
	sort.Slice(v.Entries, func(i, j int) bool { return v.Entries[i].Name < v.Entries[j].Name })
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.tmpl.ExecuteTemplate(w, "media.html", v)
}

func mimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".pdf":
		return "application/pdf"
	}
	return ""
}
