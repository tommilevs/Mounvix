package app

import (
	"net/http"
	"strings"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
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
	if r.Method != http.MethodPut {
		h.ServeHTTP(w, r)
		return
	}
	recorder := &davStatusWriter{ResponseWriter: w}
	h.ServeHTTP(recorder, r)
	status := recorder.status
	if status == 0 {
		status = http.StatusOK
	}
	if status >= 200 && status < 300 {
		relative := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/dav/"+name), "/")
		a.publishHook(plugins.HookEvent{EventID: newHookEventID("upload.done"), Kind: "upload.done", Payload: map[string]any{"root": name, "relative_path": relative, "size": r.ContentLength, "status": status}})
	}
}

type davStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *davStatusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *davStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
