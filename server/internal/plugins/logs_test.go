package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogStoreBoundsAndContainsRunLogs(t *testing.T) {
	store := NewLogStore(t.TempDir())
	payload := bytes.Repeat([]byte("x"), maxRunLogBytes+1)
	if err := store.WriteRun("plugins", "demo", "run-0123456789abcdef", payload); !errors.Is(err, ErrLogTooLarge) {
		t.Fatalf("WriteRun() error = %v, want ErrLogTooLarge", err)
	}
	if err := store.WriteRun("plugins", "../escape", "run-0123456789abcdef", []byte("secret")); err == nil {
		t.Fatal("path traversal plugin id accepted")
	}
	if err := store.WriteRun("plugins", "demo", "../escape", []byte("secret")); err == nil {
		t.Fatal("path traversal run id accepted")
	}
	if err := store.WriteRun("plugins", "demo", "run-0123456789abcdef", []byte("token=secret-value")); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadRun("plugins", "demo", "run-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "secret-value") {
		t.Fatal("known secret was not redacted")
	}
	if _, err := os.Stat(filepath.Join(store.root, "logs", "plugins", "demo")); err != nil {
		t.Fatalf("validated log directory missing: %v", err)
	}
}

func TestLogStoreRedactsAbsoluteFilesystemPaths(t *testing.T) {
	store := NewLogStore(t.TempDir())
	payload := []byte(`source=/var/lib/mounvix/private.db destination=C:\ProgramData\Mounvix\private.log open(/var/log/mounvix/error.log) CreateFile(C:\Windows\Temp\mounvix.log)`)
	if err := store.WriteRun("rclone", "rclone", "run-0123456789abcdef", payload); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadRun("rclone", "rclone", "run-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/var/lib/mounvix/private.db", `C:\ProgramData\Mounvix\private.log`, "/var/log/mounvix/error.log", `C:\Windows\Temp\mounvix.log`} {
		if strings.Contains(string(got), path) {
			t.Fatalf("absolute path leaked from run log: %q", got)
		}
	}
	if !strings.Contains(string(got), "[LOCAL PATH]") {
		t.Fatalf("redacted log lacks path marker: %q", got)
	}
}

func TestLogStoreRedactsStructuredAndAuthorizationSecrets(t *testing.T) {
	store := NewLogStore(t.TempDir())
	structured := []byte(`{"access_token":"long-secret-token","nested":{"password":"two words"},"authorization":"Bearer bearer-secret"}`)
	if err := store.WriteRun("plugins", "demo", "run-0123456789abcdef", structured); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadRun("plugins", "demo", "run-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("redacted structured log is invalid JSON: %s", got)
	}
	for _, secret := range []string{"long-secret-token", "two words", "bearer-secret"} {
		if bytes.Contains(got, []byte(secret)) {
			t.Fatalf("structured log leaked %q: %s", secret, got)
		}
	}

	multiline := []byte("{\"access_token\":\"ndjson-secret\"}\n{\"api_key\":\"second-ndjson-secret\"}\n")
	if err := store.WriteRun("plugins", "demo", "run-2123456789abcdef", multiline); err != nil {
		t.Fatal(err)
	}
	got, err = store.ReadRun("plugins", "demo", "run-2123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"ndjson-secret", "second-ndjson-secret"} {
		if bytes.Contains(got, []byte(secret)) {
			t.Fatalf("multiline JSON log leaked %q: %s", secret, got)
		}
	}

	plain := []byte("Authorization: Bearer plain-secret\npassword=second-secret\npassword=\"two words\"\nsecret='many words'")
	if err := store.WriteRun("plugins", "demo", "run-1123456789abcdef", plain); err != nil {
		t.Fatal(err)
	}
	got, err = store.ReadRun("plugins", "demo", "run-1123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"plain-secret", "second-secret", "words\"", "words'"} {
		if bytes.Contains(got, []byte(secret)) {
			t.Fatalf("plain log leaked %q: %s", secret, got)
		}
	}
}

func TestLogStoreRotatesAtTenRuns(t *testing.T) {
	store := NewLogStore(t.TempDir())
	for i := 0; i < maxLogRuns+1; i++ {
		runID := fmt.Sprintf("run-%016x", i)
		if err := store.WriteRun("rclone", "rclone", runID, []byte(runID)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.root, "logs", "rclone"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxLogRuns {
		t.Fatalf("retained %d logs, want %d", len(entries), maxLogRuns)
	}
}
