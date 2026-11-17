package app

import (
	"net/http"
	"strings"

	"golang.org/x/net/webdav"
)

// handleDAV — WebDAV на /dav/<root>/... с Basic-аутентификацией и ACL.
func (a *App) handleDAV(w http.ResponseWriter, r *http.Request) {
	u, ok := a.basicUser(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="mounvix"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/dav/")
	name := strings.SplitN(rest, "/", 2)[0]
	root, ok := a.findRoot(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	perm := a.userPerm(u, name)
	if perm == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if perm == "r" && !isSafeMethod(r.Method) {
		http.Error(w, "read-only", http.StatusForbidden)
		return
	}
	h := &webdav.Handler{
		Prefix:     "/dav/" + name,
		FileSystem: webdav.Dir(root.Path),
		LockSystem: a.locks,
	}
	h.ServeHTTP(w, r)
}
