package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRcloneJobValidationRequiresKnownRemoteAndSafePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "safe"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, request := range []RcloneJobRequest{
		{SourcePath: "../escape", RemoteName: "known", DestinationPath: "backup", Confirmed: true},
		{SourcePath: "safe", RemoteName: "unknown", DestinationPath: "backup", Confirmed: true},
		{SourcePath: "safe", RemoteName: "known", DestinationPath: "../escape", Confirmed: true},
		{SourcePath: "safe", RemoteName: "known", DestinationPath: "backup", Confirmed: false},
	} {
		if _, _, err := ValidateRcloneJobRequest(root, []string{"known"}, request); err == nil {
			t.Fatalf("unsafe request accepted: %+v", request)
		}
	}
	if _, _, err := ValidateRcloneJobRequest(root, []string{"known"}, RcloneJobRequest{SourcePath: "safe", RemoteName: "known", DestinationPath: "backup/sub", Confirmed: true}); err != nil {
		t.Fatalf("safe request rejected: %v", err)
	}
}

func TestRcloneOutputIsCappedAndSecretsAreRedacted(t *testing.T) {
	var output cappedBuffer
	payload := make([]byte, rcloneOutputLimit+8)
	n, err := output.Write(payload)
	if err != nil || n != len(payload) || !output.overflow || output.Len() != rcloneOutputLimit {
		t.Fatalf("capped write n=%d len=%d overflow=%v err=%v", n, output.Len(), output.overflow, err)
	}
	for _, tc := range []struct {
		name   string
		output string
		secret string
	}{
		{name: "plain assignment", output: "password=topsecret", secret: "topsecret"},
		{name: "quoted assignment", output: `password="quoted-secret"`, secret: "quoted-secret"},
		{name: "authorization header", output: "Authorization: Bearer bearer-secret", secret: "bearer-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactRcloneOutput(tc.output); strings.Contains(got, tc.secret) {
				t.Fatalf("secret leaked in output: %q", got)
			}
		})
	}
}

func TestRcloneAdapterUsesFixedArgvAndBoundsOutput(t *testing.T) {
	bin := t.TempDir()
	logFile := filepath.Join(bin, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + logFile + "\"\nprintf ok\n"
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := RcloneAdapter{SourceRoot: root, LookPath: func(string) (string, error) { return filepath.Join(bin, "rclone"), nil }, ListRemotesFunc: func(context.Context) ([]string, error) { return []string{"known"}, nil }}
	marker := filepath.Join(t.TempDir(), "rclone-injection-marker")
	destination := "archive;touch " + filepath.ToSlash(marker)
	if _, err := adapter.RunSync(context.Background(), RcloneJobRequest{SourcePath: "src", RemoteName: "known", DestinationPath: destination, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "sync\n" + filepath.Join(root, "src") + "\nknown:" + destination + "\n--stats\n0\n"
	if string(args) != want {
		t.Fatalf("argv=%q want=%q", args, want)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shell metacharacters executed; marker stat error=%v", err)
	}
}

func TestRcloneAdapterHonorsCallerTimeout(t *testing.T) {
	bin := t.TempDir()
	rclone := filepath.Join(bin, "rclone")
	if err := os.WriteFile(rclone, []byte("#!/bin/sh\nexec sleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter := RcloneAdapter{
		SourceRoot:      root,
		LookPath:        func(string) (string, error) { return rclone, nil },
		ListRemotesFunc: func(context.Context) ([]string, error) { return []string{"known"}, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := adapter.RunSync(ctx, RcloneJobRequest{SourcePath: "src", RemoteName: "known", DestinationPath: "archive", Confirmed: true})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunSync error=%v, want context deadline exceeded", err)
	}
}
