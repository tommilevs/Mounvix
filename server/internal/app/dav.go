package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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
		FileSystem: protectedWebDAVFS{root: root.Path, app: a},
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

type protectedWebDAVFS struct {
	root string
	app  *App
}

func (fs protectedWebDAVFS) resolve(name string) (string, error) {
	relative := strings.TrimLeft(filepath.ToSlash(name), "/")
	full, err := safeJoin(fs.root, relative)
	if err != nil {
		return "", os.ErrPermission
	}
	return full, nil
}

func (fs protectedWebDAVFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	full, err := fs.resolve(name)
	if err != nil {
		return err
	}
	if fs.app.isAppDataPath(full) {
		return os.ErrPermission
	}
	return webdav.Dir(fs.root).Mkdir(ctx, name, perm)
}

func (fs protectedWebDAVFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	full, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	if fs.app.isAppDataPath(full) {
		return nil, os.ErrNotExist
	}
	file, err := webdav.Dir(fs.root).OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	return protectedWebDAVFile{File: file, dir: full, app: fs.app}, nil
}

func (fs protectedWebDAVFS) RemoveAll(ctx context.Context, name string) error {
	full, err := fs.resolve(name)
	if err != nil {
		return err
	}
	if fs.app.isAppDataPath(full) || fs.app.containsAppDataPath(full) {
		return os.ErrPermission
	}
	return webdav.Dir(fs.root).RemoveAll(ctx, name)
}

func (fs protectedWebDAVFS) Rename(ctx context.Context, oldName, newName string) error {
	oldPath, err := fs.resolve(oldName)
	if err != nil {
		return err
	}
	newPath, err := fs.resolve(newName)
	if err != nil {
		return err
	}
	if fs.app.isAppDataPath(oldPath) || fs.app.containsAppDataPath(oldPath) ||
		fs.app.isAppDataPath(newPath) || fs.app.containsAppDataPath(newPath) {
		return os.ErrPermission
	}
	return webdav.Dir(fs.root).Rename(ctx, oldName, newName)
}

func (fs protectedWebDAVFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	full, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	if fs.app.isAppDataPath(full) {
		return nil, os.ErrNotExist
	}
	return webdav.Dir(fs.root).Stat(ctx, name)
}

type protectedWebDAVFile struct {
	webdav.File
	dir string
	app *App
}

func (f protectedWebDAVFile) Readdir(count int) ([]os.FileInfo, error) {
	entries, err := f.File.Readdir(count)
	visible := entries[:0]
	for _, entry := range entries {
		if f.app.isAppDataPath(filepath.Join(f.dir, entry.Name())) {
			continue
		}
		visible = append(visible, entry)
	}
	return visible, err
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
