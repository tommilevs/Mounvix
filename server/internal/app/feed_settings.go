package app

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func parseFeedConfig(r *http.Request, current FeedConfig, roots []Root) (FeedConfig, error) {
	cfg := current
	enabled, boolErr := parseFeedBool(r.PostFormValue("feeds_enabled"))
	if boolErr != nil {
		return FeedConfig{}, boolErr
	}
	cfg.Enabled = enabled
	cfg.RootNames = uniqueStrings(r.PostForm["feeds_roots"])
	cfg.Weekdays = nil
	seenDays := map[int]bool{}
	for _, raw := range r.PostForm["feeds_weekdays"] {
		day, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || day < 1 || day > 7 {
			return FeedConfig{}, fmt.Errorf("invalid weekday %q", raw)
		}
		if seenDays[day] {
			return FeedConfig{}, fmt.Errorf("duplicate weekday %d", day)
		}
		seenDays[day] = true
		cfg.Weekdays = append(cfg.Weekdays, day)
	}
	cfg.Times = nil
	seenTimes := map[string]bool{}
	for _, raw := range strings.Split(r.PostFormValue("feeds_times"), ",") {
		slot := strings.TrimSpace(raw)
		if slot == "" {
			continue
		}
		if _, err := time.Parse("15:04", slot); err != nil {
			return FeedConfig{}, fmt.Errorf("invalid feed time %q", slot)
		}
		if seenTimes[slot] {
			return FeedConfig{}, fmt.Errorf("duplicate feed time %q", slot)
		}
		seenTimes[slot] = true
		cfg.Times = append(cfg.Times, slot)
	}
	cfg.QuietHoursStart = strings.TrimSpace(r.PostFormValue("feeds_quiet_start"))
	cfg.QuietHoursEnd = strings.TrimSpace(r.PostFormValue("feeds_quiet_end"))
	var err error
	if cfg.PeriodDays, err = parseFeedInt(r.PostFormValue("feeds_period_days"), current.PeriodDays); err != nil {
		return FeedConfig{}, fmt.Errorf("period days: %w", err)
	}
	if cfg.MaxItems, err = parseFeedInt(r.PostFormValue("feeds_max_items"), current.MaxItems); err != nil {
		return FeedConfig{}, fmt.Errorf("max items: %w", err)
	}
	if cfg.RetentionDays, err = parseFeedInt(r.PostFormValue("feeds_retention_days"), current.RetentionDays); err != nil {
		return FeedConfig{}, fmt.Errorf("retention days: %w", err)
	}
	if err := validateFeedConfig(cfg, roots); err != nil {
		return FeedConfig{}, err
	}
	return cfg, nil
}

func parseFeedBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "off", "0", "no":
		return false, nil
	case "true", "on", "1", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("invalid enabled flag %q", value)
	}
}

func parseFeedInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", value)
	}
	return parsed, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func validateFeedConfig(cfg FeedConfig, roots []Root) error {
	knownRoots := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root.Name != "" && root.Path != "" {
			knownRoots[root.Name] = true
		}
	}
	for _, name := range cfg.RootNames {
		if !knownRoots[name] {
			return fmt.Errorf("unknown feed root %q", name)
		}
	}
	if cfg.Enabled {
		if len(cfg.RootNames) == 0 {
			return fmt.Errorf("at least one feed root is required")
		}
		if len(cfg.Weekdays) == 0 || len(cfg.Times) == 0 {
			return fmt.Errorf("at least one weekday and time are required")
		}
	}
	for _, day := range cfg.Weekdays {
		if day < 1 || day > 7 {
			return fmt.Errorf("weekday must be between 1 and 7")
		}
	}
	for _, slot := range cfg.Times {
		if _, err := time.Parse("15:04", slot); err != nil {
			return fmt.Errorf("invalid feed time %q", slot)
		}
	}
	if (cfg.QuietHoursStart == "") != (cfg.QuietHoursEnd == "") {
		return fmt.Errorf("both quiet hour boundaries are required")
	}
	for _, quietTime := range []string{cfg.QuietHoursStart, cfg.QuietHoursEnd} {
		if quietTime == "" {
			continue
		}
		if _, err := time.Parse("15:04", quietTime); err != nil {
			return fmt.Errorf("invalid quiet-hour time %q", quietTime)
		}
	}
	if cfg.PeriodDays < 1 || cfg.PeriodDays > 365 {
		return fmt.Errorf("period days must be between 1 and 365")
	}
	if cfg.MaxItems < 1 || cfg.MaxItems > 8 {
		return fmt.Errorf("max items must be between 1 and 8")
	}
	if cfg.RetentionDays < 1 || cfg.RetentionDays > 365 {
		return fmt.Errorf("retention days must be between 1 and 365")
	}
	return nil
}
