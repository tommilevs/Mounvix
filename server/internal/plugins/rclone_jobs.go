package plugins

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const rcloneOutputLimit = 1 << 20

var rcloneRemotePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,63}$`)

// RcloneJobRequest is the constrained user request for a configured remote sync.
type RcloneJobRequest struct {
	SourcePath      string `json:"source_path"`
	RemoteName      string `json:"remote_name"`
	DestinationPath string `json:"destination_path"`
	Confirmed       bool   `json:"confirmed"`
}

// RunResult contains only bounded, redacted output from a successful or failed invocation.
type RunResult struct {
	Output string `json:"output,omitempty"`
}

// RcloneAdapter invokes only the configured rclone binary and preconfigured remotes.
type RcloneAdapter struct {
	SourceRoot      string
	LookPath        func(string) (string, error)
	ListRemotesFunc func(context.Context) ([]string, error)
}

// ValidateRcloneJobRequest proves paths remain beneath the configured source root and remote is known.
func ValidateRcloneJobRequest(sourceRoot string, remotes []string, request RcloneJobRequest) (string, string, error) {
	if !request.Confirmed {
		return "", "", errors.New("explicit confirmation required for destructive rclone sync")
	}
	if request.RemoteName == "" || request.SourcePath == "" || request.DestinationPath == "" {
		return "", "", errors.New("source, remote, and destination are required")
	}
	if !rcloneRemotePattern.MatchString(request.RemoteName) {
		return "", "", errors.New("invalid rclone remote name")
	}
	found := false
	for _, remote := range remotes {
		if strings.TrimSuffix(strings.TrimSpace(remote), ":") == request.RemoteName {
			found = true
			break
		}
	}
	if !found {
		return "", "", errors.New("unknown rclone remote")
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve rclone source root: %w", err)
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", "", errors.New("rclone source root is unavailable")
	}
	if err = validateSafeRelative(request.SourcePath); err != nil {
		return "", "", fmt.Errorf("invalid source path: %w", err)
	}
	if err = validateSafeRelative(request.DestinationPath); err != nil {
		return "", "", fmt.Errorf("invalid destination path: %w", err)
	}
	source, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(request.SourcePath)))
	if err != nil {
		return "", "", fmt.Errorf("resolve source path: %w", err)
	}
	source, err = filepath.Abs(source)
	if err != nil || !withinRoot(root, source) {
		return "", "", errors.New("source path escapes configured root")
	}
	st, err = os.Stat(source)
	if err != nil || !st.IsDir() {
		return "", "", errors.New("source path must be an existing directory")
	}
	return source, request.RemoteName + ":" + filepath.ToSlash(request.DestinationPath), nil
}
func validateSafeRelative(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:\x00") || filepath.IsAbs(value) {
		return errors.New("must be a safe relative path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, "-") {
			return errors.New("contains an unsafe path segment")
		}
	}
	return nil
}

// ListRemotes lists only names from rclone with a five-second timeout.
func (a RcloneAdapter) ListRemotes(ctx context.Context) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("rclone context is required")
	}
	path, err := a.lookPath()
	if err != nil {
		return nil, err
	}
	release, err := SharedProcessGate().Acquire(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("acquire rclone process gate: %w", err)
	}
	defer release()
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, "listremotes")
	var output cappedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	if err != nil {
		return nil, commandContextError("list rclone remotes", cctx, err)
	}
	if output.overflowed() {
		return nil, errors.New("rclone remote list exceeded 1 MiB")
	}
	names := []string{}
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSuffix(strings.TrimSpace(line), ":")
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// RunSync validates and executes a confirmed sync without a shell.
func (a RcloneAdapter) RunSync(ctx context.Context, request RcloneJobRequest) (RunResult, error) {
	if ctx == nil {
		return RunResult{}, errors.New("rclone context is required")
	}
	if strings.TrimSpace(a.SourceRoot) == "" {
		return RunResult{}, errors.New("rclone source root is not configured")
	}
	remotes, err := a.listRemotes(ctx)
	if err != nil {
		return RunResult{}, err
	}
	source, destination, err := ValidateRcloneJobRequest(a.SourceRoot, remotes, request)
	if err != nil {
		return RunResult{}, err
	}
	path, err := a.lookPath()
	if err != nil {
		return RunResult{}, err
	}
	release, err := SharedProcessGate().Acquire(ctx, false)
	if err != nil {
		return RunResult{}, fmt.Errorf("acquire rclone process gate: %w", err)
	}
	defer release()
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, "sync", source, destination, "--stats", "0")
	var output cappedBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	runErr := cmd.Run()
	safe := redactRcloneOutput(output.String())
	result := RunResult{Output: safe}
	if output.overflowed() {
		return result, errors.New("rclone output exceeded 1 MiB")
	}
	if runErr != nil {
		return result, commandContextError("rclone sync", cctx, runErr)
	}
	return result, nil
}

func commandContextError(operation string, ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s: %w", operation, ctxErr)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func (a RcloneAdapter) listRemotes(ctx context.Context) ([]string, error) {
	if a.ListRemotesFunc != nil {
		return a.ListRemotesFunc(ctx)
	}
	return a.ListRemotes(ctx)
}
func (a RcloneAdapter) lookPath() (string, error) {
	lookup := a.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	path, err := lookup("rclone")
	if err != nil {
		return "", errors.New("rclone not installed")
	}
	return path, nil
}

type cappedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buffer.Len()+len(p) > rcloneOutputLimit {
		remaining := rcloneOutputLimit - b.buffer.Len()
		if remaining > 0 {
			_, _ = b.buffer.Write(p[:remaining])
		}
		b.overflow = true
		return len(p), nil
	}
	return b.buffer.Write(p)
}
func (b *cappedBuffer) String() string   { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }
func (b *cappedBuffer) overflowed() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.overflow }
func (b *cappedBuffer) Len() int         { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.Len() }
func redactRcloneOutput(value string) string {
	return string(redactKnownSecrets([]byte(value)))
}
