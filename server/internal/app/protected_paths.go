package app

import (
	"path/filepath"
	"strings"
)

func resolvedPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			resolved = filepath.Clean(resolved)
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func pathWithin(base, candidate string) bool {
	relative, err := filepath.Rel(base, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// isAppDataPath protects application-owned state even when a configured share
// root contains DataDir or reaches it through a symlink.
func (a *App) isAppDataPath(path string) bool {
	dataDir := strings.TrimSpace(a.configSnapshot().DataDir)
	if dataDir == "" {
		return true
	}
	resolvedDataDir, err := resolvedPath(dataDir)
	if err != nil {
		return true
	}
	resolvedCandidate, err := resolvedPath(path)
	if err != nil {
		return true
	}
	return pathWithin(resolvedDataDir, resolvedCandidate)
}

// containsAppDataPath is used for destructive WebDAV operations on a parent of
// DataDir, which could otherwise remove or move the application state directory.
func (a *App) containsAppDataPath(path string) bool {
	dataDir := strings.TrimSpace(a.configSnapshot().DataDir)
	if dataDir == "" {
		return true
	}
	resolvedDataDir, err := resolvedPath(dataDir)
	if err != nil {
		return true
	}
	resolvedCandidate, err := resolvedPath(path)
	if err != nil {
		return true
	}
	return pathWithin(resolvedCandidate, resolvedDataDir)
}
