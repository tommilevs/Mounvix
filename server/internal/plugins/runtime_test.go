package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func makeRuntimeTestFixture(t *testing.T, pluginID, manifestExecutable string, mode os.FileMode, contents []byte) *Runtime {
	t.Helper()
	dataDir, err := os.MkdirTemp("", "mv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dataDir); err != nil {
			t.Errorf("remove runtime test directory: %v", err)
		}
	})
	pluginRoot := filepath.Join(dataDir, "installed")
	executable := filepath.Join(pluginRoot, pluginID, filepath.FromSlash(manifestExecutable))
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if contents == nil {
		helper, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		contents = []byte(fmt.Sprintf("#!/bin/sh\nexec %q -test.run='^TestRuntimeProcessHelper$'\n", helper))
	}
	if err := os.WriteFile(executable, contents, mode); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{ID: pluginID, API: 1, Executable: manifestExecutable, RuntimeMethods: []string{"plugin.invoke", "plugin.runJob"}}
	plugin := &Plugin{ID: pluginID, Enabled: true, Available: true, Source: SourcePackage, Command: executable, Manifest: manifest}
	registry := &Registry{
		pluginRoot:     pluginRoot,
		packageEnabled: map[string]bool{pluginID: true},
		sources: map[PluginSource]map[string]*Plugin{
			SourcePackage: {pluginID: plugin},
			SourceLegacy:  {},
			SourceBuiltin: {},
		},
	}
	return NewRuntime(dataDir, registry)
}

func TestRuntimeCallRejectsUnsupportedMethod(t *testing.T) {
	registry := &Registry{sources: map[PluginSource]map[string]*Plugin{
		SourcePackage: {"demo": {ID: "demo", Available: true, Enabled: true, Source: SourcePackage, Command: "/bin/true", Manifest: &Manifest{ID: "demo", API: 1, RuntimeMethods: []string{"plugin.invoke"}}}},
		SourceLegacy:  {}, SourceBuiltin: {},
	}}
	runtime := NewRuntime(t.TempDir(), registry)
	_, err := runtime.Call(context.Background(), "demo", "plugin.runJob", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("unsupported runtime method was accepted")
	}
}

func TestManifestValidateRuntimeMethods(t *testing.T) {
	m := validManifest()
	m.RuntimeMethods = []string{"plugin.invoke", "plugin.runJob", "plugin.handleEvent", "plugin.renderPage"}
	if err := m.Validate("1.0.0", "amd64"); err != nil {
		t.Fatalf("valid runtime methods rejected: %v", err)
	}
	m.RuntimeMethods = []string{"host.log"}
	if err := m.Validate("1.0.0", "amd64"); err == nil {
		t.Fatal("host-only method was accepted as a plugin runtime method")
	}
}

func TestRuntimeOneShotProcessLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket integration is covered by the Windows cross-build")
	}
	dataDir := t.TempDir()
	pluginRoot := filepath.Join(dataDir, "installed")
	binDir := filepath.Join(pluginRoot, "demo", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexec %q -test.run='^TestRuntimeProcessHelper$'\n", helper)
	executable := filepath.Join(binDir, "plugin")
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{ID: "demo", API: 1, Executable: "bin/plugin", RuntimeMethods: []string{"plugin.invoke", "plugin.runJob"}}
	registry := &Registry{pluginRoot: pluginRoot, packageEnabled: map[string]bool{"demo": true}, sources: map[PluginSource]map[string]*Plugin{
		SourcePackage: {"demo": {ID: "demo", Enabled: true, Available: true, Source: SourcePackage, Command: executable, Manifest: manifest}},
		SourceLegacy:  {}, SourceBuiltin: {},
	}}
	runtime := NewRuntime(dataDir, registry)
	for _, method := range []string{"plugin.invoke", "plugin.runJob"} {
		result, err := runtime.Call(context.Background(), "demo", method, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Call(%s): %v", method, err)
		}
		if string(result) != `{"ok":true}` {
			t.Fatalf("Call(%s) result = %s", method, result)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := runtime.Call(ctx, "demo", "plugin.invoke", json.RawMessage(`{"hang":true}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call error = %v, want context.Canceled", err)
	}
	socketDir := filepath.Join(dataDir, "run", "plugins")
	entries, err := os.ReadDir(socketDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("runtime socket leaked: %v", entries)
	}
}

func TestRuntimeRejectsIdentityAndAPIMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket integration is covered by the Windows cross-build")
	}
	for _, pluginID := range []string{"wrong-id", "wrong-api"} {
		t.Run(pluginID, func(t *testing.T) {
			runtime := makeRuntimeTestFixture(t, pluginID, "bin/plugin", 0o700, nil)
			_, err := runtime.Call(context.Background(), pluginID, "plugin.invoke", json.RawMessage(`{}`))
			if err == nil || !strings.Contains(err.Error(), "identity or API mismatch") {
				t.Fatalf("Call() error = %v, want initialize identity/API mismatch", err)
			}
		})
	}
}

func TestRuntimeChildExitBeforeSocketConnection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket integration is covered by the Windows cross-build")
	}
	pluginRuntime := makeRuntimeTestFixture(t, "exit", "bin/plugin", 0o700, nil)
	_, err := pluginRuntime.Call(context.Background(), "exit", "plugin.invoke", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "plugin exited before connecting") {
		t.Fatalf("Call() error = %v, want child-exit error", err)
	}
}

func TestRuntimeCallHonorsDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket integration is covered by the Windows cross-build")
	}
	pluginRuntime := makeRuntimeTestFixture(t, "demo", "bin/plugin", 0o700, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := pluginRuntime.Call(ctx, "demo", "plugin.invoke", json.RawMessage(`{"hang":true}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestRuntimeRejectsExecutableTraversal(t *testing.T) {
	pluginRuntime := makeRuntimeTestFixture(t, "demo", "../outside", 0o700, []byte("unused"))
	if _, err := pluginRuntime.Call(context.Background(), "demo", "plugin.invoke", json.RawMessage(`{}`)); err == nil {
		t.Fatal("runtime accepted executable path traversal")
	}
}

func TestRuntimeRejectsUnsupportedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable permission test is not applicable on Windows")
	}
	pluginRuntime := makeRuntimeTestFixture(t, "demo", "bin/plugin", 0o600, []byte("not executable\n"))
	if _, err := pluginRuntime.Call(context.Background(), "demo", "plugin.invoke", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "start plugin process") {
		t.Fatalf("Call() error = %v, want unsupported binary start error", err)
	}
}

func TestRuntimeCapsProcessStdoutAndStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket integration is covered by the Windows cross-build")
	}
	pluginRuntime := makeRuntimeTestFixture(t, "demo", "bin/plugin", 0o700, nil)
	_, err := pluginRuntime.Call(context.Background(), "demo", "plugin.invoke", json.RawMessage(`{"stdout":true,"stderr":true}`))
	if err == nil || !strings.Contains(err.Error(), "stdout/stderr exceeds 1 MiB") {
		t.Fatalf("Call() error = %v, want stdout/stderr limit error", err)
	}
}

func TestRuntimeProcessHelper(t *testing.T) {
	pluginID := os.Getenv("MOUNVIX_PLUGIN_ID")
	if pluginID == "" || runtime.GOOS == "windows" || pluginID == "exit" {
		return
	}
	socket := os.Getenv("MOUNVIX_SOCKET")
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close runtime helper connection: %v", err)
		}
	}()
	reader := bufio.NewReader(conn)
	initialize, err := readRPCFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	request, err := decodeRPCMessage(initialize)
	if err != nil {
		t.Fatal(err)
	}
	identityID := pluginID
	apiVersion := runtimeAPI
	if pluginID == "wrong-id" {
		identityID = "different"
	}
	if pluginID == "wrong-api" {
		apiVersion++
	}
	identity, err := json.Marshal(map[string]any{"plugin_id": identityID, "api_version": apiVersion, "methods": []string{"plugin.invoke", "plugin.runJob"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRPCFrame(conn, rpcResponse(request.ID, identity, nil)); err != nil {
		t.Fatal(err)
	}
	callFrame, err := readRPCFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	call, err := decodeRPCMessage(callFrame)
	if err != nil {
		t.Fatal(err)
	}
	if call.Method != "plugin.invoke" && call.Method != "plugin.runJob" {
		t.Fatalf("unexpected method %q", call.Method)
	}
	var args map[string]bool
	if err := json.Unmarshal(call.Params, &args); err != nil {
		t.Fatal(err)
	}
	if args["hang"] {
		select {}
	}
	if args["stdout"] {
		if _, err := fmt.Print(strings.Repeat("o", maxRunLogBytes+1)); err != nil {
			t.Fatal(err)
		}
	}
	if args["stderr"] {
		if _, err := fmt.Fprint(os.Stderr, strings.Repeat("e", maxRunLogBytes+1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeRPCFrame(conn, rpcResponse(call.ID, json.RawMessage(`{"ok":true}`), nil)); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeOutputIsCapped(t *testing.T) {
	writer := &limitedOutput{}
	input := make([]byte, maxRunLogBytes+10)
	if count, err := writer.Write(input); err != nil || count != len(input) {
		t.Fatalf("Write() = %d, %v", count, err)
	}
	if len(writer.data) != maxRunLogBytes || !writer.overflow {
		t.Fatalf("output retained %d bytes, overflow=%v", len(writer.data), writer.overflow)
	}
}

func nilProcessGateContext() context.Context { return nil }

func TestProcessGateRejectsNilContext(t *testing.T) {
	gate := &ProcessGate{sem: make(chan struct{}, 1)}
	if _, err := gate.Acquire(nilProcessGateContext(), false); err == nil {
		t.Fatal("Acquire accepted a nil context")
	}
}

func TestProcessGateRejectsConcurrentInteractiveOperation(t *testing.T) {
	gate := &ProcessGate{sem: make(chan struct{}, 1)}
	release, err := gate.Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := gate.Acquire(context.Background(), false); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("Acquire() error = %v, want ErrRuntimeBusy", err)
	}
}

func TestRuntimeCallUsesSharedProcessGate(t *testing.T) {
	release, err := SharedProcessGate().Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	pluginRuntime := makeRuntimeTestFixture(t, "demo", "bin/plugin", 0o700, nil)
	if _, err := pluginRuntime.Call(context.Background(), "demo", "plugin.invoke", json.RawMessage(`{}`)); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("Runtime.Call() error = %v, want ErrRuntimeBusy while shared gate is held", err)
	}
}

func TestRuntimeCallRejectsUnknownPlugin(t *testing.T) {
	runtime := NewRuntime(t.TempDir(), &Registry{})
	_, err := runtime.Call(context.Background(), "missing", "plugin.invoke", nil)
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("Call() error = %v, want ErrPluginNotFound", err)
	}
}
