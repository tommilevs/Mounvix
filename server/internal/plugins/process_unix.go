//go:build !windows

package plugins

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func listenPluginSocket(directory string) (net.Listener, string, func() error, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, "", nil, fmt.Errorf("create plugin runtime directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, "", nil, err
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", nil, err
	}
	path := filepath.Join(directory, "p-"+hex.EncodeToString(random[:])+".sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, "", nil, fmt.Errorf("listen on plugin socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, "", nil, errors.Join(err, cleanupUnixSocket(listener, path))
	}
	cleanup := func() error { return cleanupUnixSocket(listener, path) }
	return listener, path, cleanup, nil
}

func cleanupUnixSocket(listener net.Listener, path string) error {
	var cleanupErr error
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close plugin socket listener: %w", err))
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove plugin socket: %w", err))
	}
	return cleanupErr
}

func startPluginProcess(executable, pluginID string, api int, socket, dataDir string) (*exec.Cmd, error) {
	cmd := exec.Command(executable)
	cmd.Env = []string{fmt.Sprintf("MOUNVIX_PLUGIN_ID=%s", pluginID), fmt.Sprintf("MOUNVIX_API_VERSION=%d", api), "MOUNVIX_SOCKET=" + socket, "MOUNVIX_DATA_DIR=" + dataDir}
	return cmd, nil
}

func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}
