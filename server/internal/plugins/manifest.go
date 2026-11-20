package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const maxManifestBytes = 64 * 1024

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Manifest struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	API            int             `json:"api"`
	Author         string          `json:"author"`
	Description    string          `json:"description"`
	MinApp         string          `json:"min_app,omitempty"`
	Arch           []string        `json:"arch,omitempty"`
	Icon           string          `json:"icon,omitempty"`
	Capabilities   Capabilities    `json:"capabilities,omitempty"`
	Menu           []MenuItem      `json:"menu,omitempty"`
	SettingsSchema json.RawMessage `json:"settings_schema,omitempty"`
	Pages          []Page          `json:"pages,omitempty"`
	Jobs           []Job           `json:"jobs,omitempty"`
	Hooks          []string        `json:"hooks,omitempty"`
	Endpoints      []Endpoint      `json:"endpoints,omitempty"`
	Executable     string          `json:"executable,omitempty"`
	RuntimeMethods []string        `json:"runtime_methods,omitempty"`
}

type Capabilities struct {
	FSPaths  []string `json:"fs_paths,omitempty"`
	Net      bool     `json:"net,omitempty"`
	Schedule bool     `json:"schedule,omitempty"`
}
type MenuItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}
type Page struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Path string `json:"path"`
}
type Job struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	Schedule       string `json:"schedule"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}
type Endpoint struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

func ParseManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	b, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return m, err
	}
	if len(b) > maxManifestBytes {
		return m, errors.New("manifest exceeds 64 KiB limit")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return m, errors.New("manifest must contain exactly one JSON object")
	}
	return m, nil
}

func (m Manifest) Validate(appVersion, arch string) error {
	if !pluginIDPattern.MatchString(m.ID) {
		return fmt.Errorf("invalid plugin id %q", m.ID)
	}
	if m.ID == "rclone" {
		return errors.New("rclone is a reserved builtin plugin id")
	}
	if m.Name == "" || m.Version == "" || m.Author == "" || m.Description == "" || m.Executable == "" {
		return errors.New("name, version, author, description, and executable are required")
	}
	if m.API != 1 {
		return fmt.Errorf("unsupported plugin api %d", m.API)
	}
	if m.MinApp != "" {
		minimum, err := parseVersion(m.MinApp)
		if err != nil {
			return fmt.Errorf("invalid min_app version: %w", err)
		}
		if appVersion != "" && !strings.EqualFold(appVersion, "dev") {
			current, err := parseVersion(appVersion)
			if err != nil {
				return fmt.Errorf("invalid application version: %w", err)
			}
			if compareVersion(current, minimum) < 0 {
				return fmt.Errorf("plugin requires app version %s", m.MinApp)
			}
		}
	}
	if arch != "" && len(m.Arch) != 0 {
		found := false
		for _, candidate := range m.Arch {
			if candidate == arch {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("plugin does not support architecture %s", arch)
		}
	}
	if m.Executable != "" {
		if err := validateRelativePath(m.Executable); err != nil {
			return fmt.Errorf("executable: %w", err)
		}
	}
	for _, p := range m.Capabilities.FSPaths {
		if !strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, "..") {
			return fmt.Errorf("invalid fs capability path %q", p)
		}
	}
	ids := map[string]bool{}
	for _, v := range m.Menu {
		if err := uniqueID(ids, v.ID, "menu"); err != nil {
			return err
		}
		if v.Label == "" {
			return errors.New("menu label is required")
		}
		if v.Path != "" {
			if err := validateRelativePath(v.Path); err != nil {
				return fmt.Errorf("menu path: %w", err)
			}
		}
	}
	ids = map[string]bool{}
	for _, v := range m.Pages {
		if err := uniqueID(ids, v.ID, "page"); err != nil {
			return err
		}
		if v.Path == "" {
			return fmt.Errorf("page %q path is required", v.ID)
		}
		if err := validateRelativePath(v.Path); err != nil {
			return fmt.Errorf("page %q path: %w", v.ID, err)
		}
	}
	ids = map[string]bool{}
	for _, v := range m.Jobs {
		if err := uniqueID(ids, v.ID, "job"); err != nil {
			return err
		}
		if v.TimeoutSeconds < 1 || v.TimeoutSeconds > 600 {
			return fmt.Errorf("job %q timeout must be between 1 and 600 seconds", v.ID)
		}
		if err := validateSchedule(v.Schedule); err != nil {
			return fmt.Errorf("job %q schedule: %w", v.ID, err)
		}
	}
	ids = map[string]bool{}
	for _, v := range m.Endpoints {
		if err := uniqueID(ids, v.ID, "endpoint"); err != nil {
			return err
		}
		method := strings.ToUpper(v.Method)
		if method != http.MethodGet && method != http.MethodPost && method != http.MethodPut && method != http.MethodDelete && method != http.MethodPatch {
			return fmt.Errorf("endpoint %q has unsupported method", v.ID)
		}
		if err := validateRelativePath(v.Path); err != nil {
			return fmt.Errorf("endpoint %q path: %w", v.ID, err)
		}
	}
	seenHooks := map[string]bool{}
	for _, hook := range m.Hooks {
		if seenHooks[hook] {
			return fmt.Errorf("duplicate hook %q", hook)
		}
		seenHooks[hook] = true
		if !allowedHook(hook) {
			return fmt.Errorf("unsupported hook %q", hook)
		}
	}
	seenMethods := map[string]bool{}
	for _, method := range m.RuntimeMethods {
		if !supportedRuntimeMethod(method) {
			return fmt.Errorf("unsupported runtime method %q", method)
		}
		if seenMethods[method] {
			return fmt.Errorf("duplicate runtime method %q", method)
		}
		seenMethods[method] = true
	}
	if len(m.SettingsSchema) != 0 && !json.Valid(m.SettingsSchema) {
		return errors.New("settings_schema must be valid JSON")
	}
	return nil
}

func validateRelativePath(value string) error {
	if value == "" || strings.Contains(value, "\\") || strings.Contains(value, "\x00") || strings.HasPrefix(value, "/") || path.IsAbs(value) {
		return errors.New("path must be a safe relative path")
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return errors.New("path must not traverse outside the plugin root")
	}
	return nil
}

func validateSchedule(value string) error {
	fields := strings.Fields(value)
	if len(fields) != 5 {
		return errors.New("schedule must contain five cron fields")
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, field := range fields {
		if field == "*" {
			continue
		}
		n, err := strconv.Atoi(field)
		if err != nil || n < limits[i][0] || n > limits[i][1] {
			return fmt.Errorf("invalid field %q", field)
		}
	}
	return nil
}

func uniqueID(seen map[string]bool, id, kind string) error {
	if !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("invalid %s id %q", kind, id)
	}
	if seen[id] {
		return fmt.Errorf("duplicate %s id %q", kind, id)
	}
	seen[id] = true
	return nil
}

func allowedHook(hook string) bool {
	switch hook {
	case "upload.done", "backup.done", "plugin.job.succeeded", "plugin.job.failed", "plugin.job.cancelled", "rclone.sync.succeeded", "rclone.sync.failed", "rclone.sync.cancelled":
		return true
	default:
		return false
	}
}

func parseVersion(value string) ([3]int, error) {
	var parsed [3]int
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	if value == "" || len(parts) > len(parsed) {
		return parsed, fmt.Errorf("invalid version %q", value)
	}
	for i, part := range parts {
		if part == "" {
			return parsed, fmt.Errorf("invalid version %q", value)
		}
		component, err := strconv.Atoi(part)
		if err != nil || component < 0 {
			return parsed, fmt.Errorf("invalid version %q", value)
		}
		parsed[i] = component
	}
	return parsed, nil
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
