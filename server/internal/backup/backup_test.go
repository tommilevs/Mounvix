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
	job, err := m.Create("test", src, []Target{{Type: "local", Path: dst}})
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
	job, err := m.Create("wd", src, []Target{{Type: "webdav", URL: server.URL + "/dav/backup", User: "u", Password: "p"}})
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

func TestBadTargetReportsError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "x.txt"), []byte("x"), 0o644)
	m, _ := New(t.TempDir())
	job, _ := m.Create("bad", src, []Target{{Type: "ftp", Path: "nope"}})
	rep, err := m.Run(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) == 0 || !strings.Contains(rep.Errors[0], "unknown target") {
		t.Fatalf("expected unknown target error, got %v", rep.Errors)
	}
}
