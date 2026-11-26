package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRcloneSyncArgs(t *testing.T) {
	bin := t.TempDir()
	logFile := filepath.Join(bin, "args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + logFile + "\necho SYNCED\n"
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.RcloneSync(context.Background(), "/data/фото", "/backup/out")
	if err != nil {
		t.Fatalf("sync: %v (%s)", err, out)
	}
	if !strings.Contains(out, "SYNCED") {
		t.Fatalf("output: %q", out)
	}
	b, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{"sync", "/data/фото", "/backup/out", "--stats", "0"}
	if len(args) < len(want) {
		t.Fatalf("args = %v", args)
	}
	for i, w := range want {
		if args[i] != w {
			t.Fatalf("arg %d = %q, want %q (all %v)", i, args[i], w, args)
		}
	}
}

func TestRcloneSyncDoesNotRunWhileSharedProcessGateIsHeld(t *testing.T) {
	bin := t.TempDir()
	logFile := filepath.Join(bin, "rclone-called")
	script := "#!/bin/sh\nprintf called > " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	registry, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	releaseGate, err := SharedProcessGate().Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGate()

	if _, err := registry.RcloneSync(context.Background(), "/source", "/target"); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("RcloneSync() error = %v, want ErrRuntimeBusy", err)
	}
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Fatalf("rclone executed despite busy process gate; stat error = %v", err)
	}
}

func TestRcloneStatusDoesNotRunWhileSharedProcessGateIsHeld(t *testing.T) {
	bin := t.TempDir()
	logFile := filepath.Join(bin, "rclone-called")
	script := "#!/bin/sh\nprintf called >> " + logFile + "\necho status\n"
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	registry, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	releaseGate, err := SharedProcessGate().Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseGate()

	status := registry.RcloneStatus()
	if status.Error != ErrRuntimeBusy.Error() {
		t.Fatalf("RcloneStatus().Error = %q, want %q", status.Error, ErrRuntimeBusy)
	}
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Fatalf("rclone status command ran despite busy process gate; stat error = %v", err)
	}
}

func TestRcloneSyncRejectsFlagPaths(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.RcloneSync(context.Background(), "--dry-run", "/x"); err == nil {
		t.Fatal("flag-like src accepted")
	}
	if _, err := r.RcloneSync(context.Background(), "/x", ""); err == nil {
		t.Fatal("empty dst accepted")
	}
}
