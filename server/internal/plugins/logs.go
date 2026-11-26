package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const (
	maxRunLogBytes = 1 << 20
	maxLogRuns     = 10
)

var (
	ErrLogTooLarge             = errors.New("plugin run log exceeds 1 MiB")
	runIDPattern               = regexp.MustCompile(`^run-[a-f0-9]{16,64}$`)
	secretPattern              = regexp.MustCompile(`(?i)([a-z0-9_-]*(?:token|password|secret|api[_-]?key|private[_-]?key)[a-z0-9_-]*\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s,;]+)`)
	authorizationSecretPattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:"(?:bearer|basic)\s+(?:\\.|[^"\\])*"|'(?:bearer|basic)\s+(?:\\.|[^'\\])*'|(?:bearer|basic)\s+[^\s,;]+|"[^"\\]*(?:\\.[^"\\]*)*"|'[^'\\]*(?:\\.[^'\\]*)*'|[^\s,;]+)`)
)

var (
	unixAbsolutePathPattern    = regexp.MustCompile(`(^|[\s"'=(])(/[^\s"'<>|?*]+)`)
	windowsAbsolutePathPattern = regexp.MustCompile(`(?i)(^|[\s"'=:(])([a-z]:\\[^\s"'<>|?*]*)`)
)

// LogStore stores bounded, redacted output for generated run identifiers.
type LogStore struct {
	root string
	mu   sync.Mutex
}

// NewLogStore creates a store rooted at the application data directory.
func NewLogStore(dataDir string) *LogStore { return &LogStore{root: dataDir} }

func redactKnownSecrets(output []byte) []byte {
	if redacted, ok := redactJSONLine(output); ok {
		return redactLocalPaths(redacted)
	}
	lines := bytes.SplitAfter(output, []byte("\n"))
	for i, line := range lines {
		if redacted, ok := redactJSONLine(line); ok {
			lines[i] = redacted
			continue
		}
		lines[i] = redactPlainTextSecrets(line)
	}
	return redactLocalPaths(bytes.Join(lines, nil))
}

func redactLocalPaths(output []byte) []byte {
	redacted := unixAbsolutePathPattern.ReplaceAll(output, []byte("$1[LOCAL PATH]"))
	return windowsAbsolutePathPattern.ReplaceAll(redacted, []byte("$1[LOCAL PATH]"))
}

func redactJSONLine(line []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, false
	}
	var structured any
	if err := json.Unmarshal(trimmed, &structured); err != nil {
		return nil, false
	}
	redactStructuredSecrets(structured)
	redacted, err := json.Marshal(structured)
	if err != nil {
		return nil, false
	}
	start := bytes.Index(line, trimmed)
	if start < 0 {
		return nil, false
	}
	end := start + len(trimmed)
	result := make([]byte, 0, len(line)-len(trimmed)+len(redacted))
	result = append(result, line[:start]...)
	result = append(result, redacted...)
	result = append(result, line[end:]...)
	return result, true
}

func redactPlainTextSecrets(output []byte) []byte {
	redacted := authorizationSecretPattern.ReplaceAll(output, []byte("$1[REDACTED]"))
	return secretPattern.ReplaceAll(redacted, []byte("$1[REDACTED]"))
}

func redactStructuredSecrets(value any) {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
			if strings.Contains(normalized, "token") || strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "authorization") || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "privatekey") || strings.Contains(normalized, "credential") {
				current[key] = "[REDACTED]"
				continue
			}
			redactStructuredSecrets(item)
		}
	case []any:
		for _, item := range current {
			redactStructuredSecrets(item)
		}
	}
}

func closeTemporaryLog(tmp *os.File, operationErr error) error {
	if err := tmp.Close(); err != nil {
		return errors.Join(operationErr, fmt.Errorf("close temporary plugin log: %w", err))
	}
	return operationErr
}

// WriteRun atomically writes a bounded run log under its validated owner directory.
func (s *LogStore) WriteRun(kind, pluginID, runID string, output []byte) (writeErr error) {
	if len(output) > maxRunLogBytes {
		return ErrLogTooLarge
	}
	dir, err := s.runDirectory(kind, pluginID)
	if err != nil {
		return err
	}
	if !runIDPattern.MatchString(runID) {
		return errors.New("invalid generated run id")
	}
	clean := redactKnownSecrets(output)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create plugin log directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".run-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary plugin log: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			if err := os.Remove(tmpName); err != nil && !errors.Is(err, os.ErrNotExist) {
				writeErr = errors.Join(writeErr, fmt.Errorf("remove temporary plugin log: %w", err))
			}
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return closeTemporaryLog(tmp, fmt.Errorf("set temporary plugin log permissions: %w", err))
	}
	if _, err := tmp.Write(clean); err != nil {
		return closeTemporaryLog(tmp, fmt.Errorf("write plugin log: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return closeTemporaryLog(tmp, fmt.Errorf("sync plugin log: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close plugin log: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, runID+".log")); err != nil {
		return fmt.Errorf("publish plugin log: %w", err)
	}
	removeTemp = false
	return s.rotate(dir)
}

// ReadRun returns one validated run log without exposing local paths.
func (s *LogStore) ReadRun(kind, pluginID, runID string) ([]byte, error) {
	dir, err := s.runDirectory(kind, pluginID)
	if err != nil {
		return nil, err
	}
	if !runIDPattern.MatchString(runID) {
		return nil, errors.New("invalid generated run id")
	}
	contents, err := os.ReadFile(filepath.Join(dir, runID+".log"))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxRunLogBytes {
		return nil, ErrLogTooLarge
	}
	return contents, nil
}

func (s *LogStore) runDirectory(kind, pluginID string) (string, error) {
	switch kind {
	case "plugins":
		if !pluginIDPattern.MatchString(pluginID) || pluginID == "rclone" {
			return "", errors.New("invalid plugin id")
		}
		return filepath.Join(s.root, "logs", "plugins", pluginID), nil
	case "rclone":
		if pluginID != "rclone" {
			return "", errors.New("invalid rclone log owner")
		}
		return filepath.Join(s.root, "logs", "rclone"), nil
	default:
		return "", errors.New("invalid log kind")
	}
}

func (s *LogStore) rotate(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, info)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].ModTime().Equal(files[j].ModTime()) {
			return files[i].Name() < files[j].Name()
		}
		return files[i].ModTime().Before(files[j].ModTime())
	})
	for len(files) > maxLogRuns {
		if err := os.Remove(filepath.Join(dir, files[0].Name())); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
}
