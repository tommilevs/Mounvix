package plugins

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
)

func listenPluginSocket(directory string) (net.Listener, string, func() error, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", nil, fmt.Errorf("listen on loopback plugin socket: %w", err)
	}
	cleanup := func() error {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("close plugin loopback listener: %w", err)
		}
		return nil
	}
	return listener, listener.Addr().String(), cleanup, nil
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
	return cmd.Process.Signal(os.Interrupt)
}
