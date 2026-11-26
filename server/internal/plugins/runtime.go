package plugins

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrRuntimeBusy             = errors.New("plugin runtime is busy")
	ErrPluginNotFound          = errors.New("plugin not found")
	ErrPluginPageUnavailable   = errors.New("plugin page unavailable")
	ErrRuntimeMethodNotAllowed = errors.New("plugin runtime method is not allowed")
	ErrJobQueueFull            = errors.New("plugin job queue is full")
)

const (
	runtimeAPI         = 1
	runtimeCallTimeout = 30 * time.Second
	runtimeJobTimeout  = 10 * time.Minute
	processGracePeriod = 5 * time.Second
	maxPendingJobs     = 100
)

// ProcessGate serializes plugin, rclone and package-manager child processes.
type ProcessGate struct {
	sem     chan struct{}
	mu      sync.Mutex
	waiters int
}

var sharedProcessGate = &ProcessGate{sem: make(chan struct{}, 1)}

// SharedProcessGate returns the process-wide child-operation gate.
func SharedProcessGate() *ProcessGate { return sharedProcessGate }

// Acquire obtains exclusive access, returning ErrRuntimeBusy for interactive contention.
func (g *ProcessGate) Acquire(ctx context.Context, queued bool) (func(), error) {
	if g == nil {
		return nil, errors.New("process gate is required")
	}
	if ctx == nil {
		return nil, errors.New("process gate context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !queued {
		select {
		case g.sem <- struct{}{}:
			if err := ctx.Err(); err != nil {
				<-g.sem
				return nil, err
			}
			return func() { <-g.sem }, nil
		default:
			return nil, ErrRuntimeBusy
		}
	}
	g.mu.Lock()
	if g.waiters >= maxPendingJobs {
		g.mu.Unlock()
		return nil, ErrJobQueueFull
	}
	g.waiters++
	g.mu.Unlock()
	defer func() { g.mu.Lock(); g.waiters--; g.mu.Unlock() }()
	select {
	case g.sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.sem
			return nil, err
		}
		return func() { <-g.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Runtime launches one isolated process per validated plugin call.
type Runtime struct {
	dataDir  string
	registry *Registry
	gate     *ProcessGate
	settings func(pluginID, key string) (json.RawMessage, error)
	logStore *LogStore
}

// NewRuntime configures the runtime using the registry and data directory.
func NewRuntime(dataDir string, registry *Registry) *Runtime {
	return &Runtime{dataDir: dataDir, registry: registry, gate: SharedProcessGate(), logStore: NewLogStore(dataDir)}
}

// SetSettingsGetter installs access to non-secret settings exposed through host.settings.get.
func (r *Runtime) SetSettingsGetter(getter func(pluginID, key string) (json.RawMessage, error)) {
	r.settings = getter
}

// Call starts a one-shot plugin process and invokes one manifest-allowed JSON-RPC method.
func (r *Runtime) Call(ctx context.Context, pluginID, method string, params json.RawMessage) (json.RawMessage, error) {
	return r.CallWithRunID(ctx, pluginID, method, params, "")
}

// CallWithRunID associates runtime output with a durable job run when runID is provided.
func (r *Runtime) CallWithRunID(ctx context.Context, pluginID, method string, params json.RawMessage, runID string) (json.RawMessage, error) {
	if runID != "" && !runIDPattern.MatchString(runID) {
		return nil, errors.New("invalid runtime run ID")
	}
	if ctx == nil {
		return nil, errors.New("runtime context is required")
	}
	if !pluginIDPattern.MatchString(pluginID) {
		return nil, errors.New("invalid plugin id")
	}
	if r.registry == nil {
		return nil, ErrPluginNotFound
	}
	plugin, ok := r.registry.LookupSource(pluginID, SourcePackage)
	if !ok || plugin.Manifest == nil {
		return nil, ErrPluginNotFound
	}
	if !plugin.Enabled || !plugin.Available {
		return nil, errors.New("plugin is disabled or unavailable")
	}
	if !supportedRuntimeMethod(method) || !contains(plugin.Manifest.RuntimeMethods, method) {
		return nil, ErrRuntimeMethodNotAllowed
	}
	if !filepath.IsAbs(plugin.Command) || r.registry.pluginRoot == "" {
		return nil, errors.New("plugin executable must be an absolute package path")
	}
	if err := validateRuntimeExecutable(r.registry.pluginRoot, plugin); err != nil {
		return nil, err
	}

	release, err := r.gate.Acquire(ctx, method == "plugin.runJob")
	if err != nil {
		return nil, err
	}
	defer release()
	callTimeout := runtimeCallTimeout
	if method == "plugin.runJob" {
		callTimeout = runtimeJobTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	result, output, runID, callErr := r.runOneShot(callCtx, plugin, method, params, runID)
	if len(output) != 0 && r.logStore != nil {
		if logErr := r.logStore.WriteRun("plugins", pluginID, runID, output); callErr == nil && logErr != nil {
			callErr = logErr
		}
	}
	return result, callErr
}

func (r *Runtime) runOneShot(ctx context.Context, plugin *Plugin, method string, params json.RawMessage, runID string) (result json.RawMessage, output []byte, resultRunID string, runErr error) {
	if runID == "" {
		var runBytes [16]byte
		if _, err := rand.Read(runBytes[:]); err != nil {
			return nil, nil, "", fmt.Errorf("generate run id: %w", err)
		}
		runID = "run-" + hex.EncodeToString(runBytes[:])
	}
	resultRunID = runID
	listener, socket, cleanup, err := listenPluginSocket(filepath.Join(r.dataDir, "run", "plugins"))
	if err != nil {
		return nil, nil, runID, err
	}
	defer func() {
		if err := cleanup(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("cleanup plugin socket: %w", err))
		}
	}()
	cmd, err := startPluginProcess(plugin.Command, plugin.ID, runtimeAPI, socket, r.dataDir)
	if err != nil {
		return nil, nil, runID, err
	}
	stdout := &limitedOutput{}
	stderr := &limitedOutput{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, runID, fmt.Errorf("start plugin process: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	finished := false
	defer func() {
		if finished {
			return
		}
		if err := terminateProcess(cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
			runErr = errors.Join(runErr, fmt.Errorf("terminate plugin process: %w", err))
		}
		select {
		case waitErr := <-wait:
			if waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
				runErr = errors.Join(runErr, fmt.Errorf("wait for plugin process: %w", waitErr))
			}
		case <-time.After(processGracePeriod):
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				runErr = errors.Join(runErr, fmt.Errorf("kill plugin process: %w", err))
			}
			if waitErr := <-wait; waitErr != nil && !errors.Is(waitErr, os.ErrProcessDone) {
				runErr = errors.Join(runErr, fmt.Errorf("wait for killed plugin process: %w", waitErr))
			}
		}
	}()

	handshakeDeadline := time.Now().Add(5 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}
	deadlineListener, ok := listener.(interface{ SetDeadline(time.Time) error })
	if !ok {
		return nil, nil, runID, errors.New("plugin listener does not support deadlines")
	}
	if err := deadlineListener.SetDeadline(handshakeDeadline); err != nil {
		return nil, nil, runID, fmt.Errorf("set plugin handshake deadline: %w", err)
	}
	accepted := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		accepted <- struct {
			conn net.Conn
			err  error
		}{conn, acceptErr}
	}()
	var conn net.Conn
	select {
	case outcome := <-accepted:
		if outcome.err != nil {
			if ctxErr := runtimeContextError(ctx); ctxErr != nil {
				return nil, outputBytes(stdout, stderr), runID, ctxErr
			}
			return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("accept plugin connection: %w", outcome.err)
		}
		conn = outcome.conn
	case <-ctx.Done():
		return nil, outputBytes(stdout, stderr), runID, ctx.Err()
	case err := <-wait:
		finished = true
		if err == nil {
			return nil, outputBytes(stdout, stderr), runID, errors.New("plugin exited before connecting")
		}
		return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("plugin exited before connecting: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			runErr = errors.Join(runErr, fmt.Errorf("close plugin connection: %w", err))
		}
	}()
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("close plugin listener: %w", err)
	}
	watchDone := make(chan struct{})
	watchResult := make(chan error, 1)
	defer func() {
		close(watchDone)
		if err := <-watchResult; err != nil && !errors.Is(err, net.ErrClosed) {
			runErr = errors.Join(runErr, fmt.Errorf("close plugin connection on cancellation: %w", err))
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			watchResult <- conn.Close()
		case <-watchDone:
			watchResult <- nil
		}
	}()
	if err := conn.SetDeadline(deadlineOr(ctx, time.Now().Add(runtimeCallTimeout))); err != nil {
		return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("set plugin connection deadline: %w", err)
	}
	transport := &rpcConn{conn: conn, reader: bufio.NewReaderSize(conn, 64*1024), pluginID: plugin.ID}
	transport.settings = func(key string) (json.RawMessage, error) {
		if r.settings == nil {
			return nil, errors.New("settings unavailable")
		}
		return r.settings(plugin.ID, key)
	}
	initParams, err := json.Marshal(map[string]any{"plugin_id": plugin.ID, "api_version": runtimeAPI, "server_version": "1"})
	if err != nil {
		return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("encode plugin identity: %w", err)
	}
	initResult, err := transport.call("plugin.initialize", initParams)
	if err != nil {
		if ctxErr := runtimeContextError(ctx); ctxErr != nil {
			return nil, outputBytes(stdout, stderr), runID, ctxErr
		}
		return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("plugin initialize: %w", err)
	}
	var identity struct {
		PluginID   string   `json:"plugin_id"`
		APIVersion int      `json:"api_version"`
		Methods    []string `json:"methods"`
	}
	if err := json.Unmarshal(initResult, &identity); err != nil || identity.PluginID != plugin.ID || identity.APIVersion != runtimeAPI {
		return nil, outputBytes(stdout, stderr), runID, errors.New("plugin initialize identity or API mismatch")
	}
	if !contains(identity.Methods, method) {
		return nil, outputBytes(stdout, stderr), runID, ErrRuntimeMethodNotAllowed
	}
	result, err = transport.call(method, params)
	if err != nil {
		if ctxErr := runtimeContextError(ctx); ctxErr != nil {
			return nil, outputBytes(stdout, stderr), runID, ctxErr
		}
		return nil, outputBytes(stdout, stderr), runID, err
	}
	select {
	case waitErr := <-wait:
		finished = true
		if waitErr != nil {
			return nil, outputBytes(stdout, stderr), runID, fmt.Errorf("plugin process failed: %w", waitErr)
		}
	case <-ctx.Done():
		return nil, outputBytes(stdout, stderr), runID, ctx.Err()
	}
	if stdout.overflow || stderr.overflow || len(stdout.data)+len(stderr.data) > maxRunLogBytes {
		return nil, outputBytes(stdout, stderr), runID, errors.New("plugin stdout/stderr exceeds 1 MiB")
	}
	return result, outputBytes(stdout, stderr), runID, nil
}

func validateRuntimeExecutable(pluginRoot string, plugin *Plugin) error {
	if err := validateRelativePath(plugin.Manifest.Executable); err != nil {
		return fmt.Errorf("invalid plugin executable path: %w", err)
	}
	root, err := filepath.Abs(pluginRoot)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve plugin root: %w", err)
	}
	packageRoot, err := filepath.EvalSymlinks(filepath.Join(root, plugin.ID))
	if err != nil {
		return fmt.Errorf("resolve plugin package: %w", err)
	}
	packageRoot, err = filepath.Abs(packageRoot)
	if err != nil || !withinRoot(root, packageRoot) {
		return errors.New("plugin package escapes plugin root")
	}
	executable, err := filepath.EvalSymlinks(filepath.Join(packageRoot, filepath.FromSlash(plugin.Manifest.Executable)))
	if err != nil {
		return fmt.Errorf("resolve plugin executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil || !withinRoot(packageRoot, executable) {
		return errors.New("plugin executable escapes package root")
	}
	if filepath.Clean(executable) != filepath.Clean(plugin.Command) {
		return errors.New("plugin executable does not match validated manifest")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("plugin executable is unavailable")
	}
	return nil
}

func supportedRuntimeMethod(method string) bool {
	switch method {
	case "plugin.invoke", "plugin.runJob", "plugin.handleEvent", "plugin.renderPage":
		return true
	default:
		return false
	}
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}

func deadlineOr(ctx context.Context, fallback time.Time) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return fallback
}

func runtimeContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

type limitedOutput struct {
	data     []byte
	overflow bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	remaining := maxRunLogBytes - len(w.data)
	if remaining > 0 {
		count := len(p)
		if count > remaining {
			count = remaining
		}
		w.data = append(w.data, p[:count]...)
	}
	if len(p) > remaining {
		w.overflow = true
	}
	return len(p), nil
}
func outputBytes(stdout, stderr *limitedOutput) []byte {
	out := make([]byte, 0, len(stdout.data)+len(stderr.data))
	out = append(out, stdout.data...)
	out = append(out, stderr.data...)
	return out
}

var _ io.Writer = (*limitedOutput)(nil)
