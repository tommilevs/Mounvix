package backup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalBackupAndIncremental(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.bin"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job, err := m.Create("test", src, []Target{{Type: "local", Path: dst}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := m.Run(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 2 || rep.Bytes != 9 {
		t.Fatalf("first run files=%d bytes=%d errors=%v", rep.Files, rep.Bytes, rep.Errors)
	}
	got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("copy a.txt: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "b.bin")); err != nil {
		t.Fatalf("nested copy: %v", err)
	}

	// Второй запуск без изменений — всё пропущено.
	rep2, err := m.Run(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Files != 0 || rep2.Skipped != 2 {
		t.Fatalf("incremental run files=%d skipped=%d", rep2.Files, rep2.Skipped)
	}

	// Новый файл после LastRun — копируется.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(src, "c.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep3, err := m.Run(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Files != 1 {
		t.Fatalf("third run files=%d", rep3.Files)
	}
	if _, err := os.Stat(filepath.Join(dst, "c.txt")); err != nil {
		t.Fatalf("new file not copied: %v", err)
	}
}

func TestWebDAVTarget(t *testing.T) {
	var mu sync.Mutex
	files := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			files[r.URL.Path] = string(b)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("webdav-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := New(t.TempDir())
	job, err := m.Create("wd", src, []Target{{Type: "webdav", URL: server.URL + "/dav/backup", User: "u", Password: "p"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := m.Run(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 1 || len(rep.Errors) != 0 {
		t.Fatalf("webdav run files=%d errors=%v", rep.Files, rep.Errors)
	}
	mu.Lock()
	body, ok := files["/dav/backup/hello.txt"]
	mu.Unlock()
	if !ok || body != "webdav-body" {
		t.Fatalf("webdav body = %q ok=%v files=%v", body, ok, files)
	}
}

func TestRestoreWebDAV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			p := r.URL.Path
			type ent struct {
				href string
				col  bool
			}
			var list []ent
			switch p {
			case "/dav/b":
				list = []ent{{"/dav/b", true}, {"/dav/b/a.txt", false}, {"/dav/b/sub/", true}}
			case "/dav/b/sub":
				list = []ent{{"/dav/b/sub", true}, {"/dav/b/sub/c.txt", false}}
			default:
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			var b strings.Builder
			b.WriteString(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`)
			for _, e := range list {
				b.WriteString("<D:response><D:href>" + e.href + "</D:href><D:propstat><D:prop><D:resourcetype>")
				if e.col {
					b.WriteString("<D:collection/>")
				}
				b.WriteString("</D:resourcetype><D:getcontentlength>4</D:getcontentlength></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>")
			}
			b.WriteString("</D:multistatus>")
			_, _ = w.Write([]byte(b.String()))
		case http.MethodGet:
			switch r.URL.Path {
			case "/dav/b/a.txt":
				_, _ = w.Write([]byte("v1"))
			case "/dav/b/sub/c.txt":
				_, _ = w.Write([]byte("v2"))
			default:
				w.WriteHeader(404)
			}
		default:
			w.WriteHeader(405)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0o755)
	m, _ := New(t.TempDir())
	job, err := m.Create("dav-restore", src, []Target{{Type: "webdav", URL: server.URL + "/dav/b"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	rep, err := m.Restore(context.Background(), job.ID, out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 2 || len(rep.Errors) != 0 {
		t.Fatalf("dav restore files=%d errors=%v", rep.Files, rep.Errors)
	}
	a, _ := os.ReadFile(filepath.Join(out, "a.txt"))
	c, _ := os.ReadFile(filepath.Join(out, "sub", "c.txt"))
	if string(a) != "v1" || string(c) != "v2" {
		t.Fatalf("dav restore contents a=%q c=%q", a, c)
	}
}
func TestRestoreLocal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	out := filepath.Join(dir, "restored")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("v1"), 0o644)
	m, _ := New(t.TempDir())
	job, err := m.Create("restore", src, []Target{{Type: "local", Path: dst}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	// Меняем источник; restore должен вернуть сохранённую версию.
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("v2"), 0o644)
	rep, err := m.Restore(context.Background(), job.ID, out)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Files != 1 || len(rep.Errors) != 0 {
		t.Fatalf("restore files=%d errors=%v", rep.Files, rep.Errors)
	}
	b, err := os.ReadFile(filepath.Join(out, "a.txt"))
	if err != nil || string(b) != "v1" {
		t.Fatalf("restored content=%q err=%v", b, err)
	}
}
func TestRunDue(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("sched"), 0o644)
	m, _ := New(t.TempDir())
	job, err := m.Create("sched", src, []Target{{Type: "local", Path: filepath.Join(dir, "dst")}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Только что созданное задание не должно запускаться сразу.
	if reps := m.RunDue(context.Background(), time.Now()); len(reps) != 0 {
		t.Fatalf("not due yet, got %d reports", len(reps))
	}
	// Искусственно «просрочиваем» интервал.
	m.mu.Lock()
	job.LastRun = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	reps := m.RunDue(context.Background(), time.Now())
	if len(reps) != 1 || reps[0].Files != 1 {
		t.Fatalf("due run reports=%v", reps)
	}
	// Сразу после запуска — снова не время.
	if reps := m.RunDue(context.Background(), time.Now()); len(reps) != 0 {
		t.Fatalf("ran twice, got %d", len(reps))
	}
}
func TestBadTargetReportsError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "x.txt"), []byte("x"), 0o644)
	m, _ := New(t.TempDir())
	job, _ := m.Create("bad", src, []Target{{Type: "ftp", Path: "nope"}}, 0)
	rep, err := m.Run(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) == 0 || !strings.Contains(rep.Errors[0], "unknown target") {
		t.Fatalf("expected unknown target error, got %v", rep.Errors)
	}
}
