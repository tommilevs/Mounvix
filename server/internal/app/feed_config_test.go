package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/feeds"
)

func TestFeedConfigDefaultsAndRoundTrip(t *testing.T) {
	defaults := DefaultConfig()
	if defaults.Feeds.Enabled || defaults.Feeds.PeriodDays != 7 || defaults.Feeds.MaxItems != 8 || defaults.Feeds.RetentionDays != 30 {
		t.Fatalf("feed defaults = %+v; want disabled, 7 days, 8 items, 30-day retention", defaults.Feeds)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := `{"port":48100,"data_dir":"` + dir + `","lang":"ru","roots":[]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig legacy: %v", err)
	}
	if loaded.Feeds.Enabled || loaded.Feeds.PeriodDays != 7 || loaded.Feeds.MaxItems != 8 || loaded.Feeds.RetentionDays != 30 {
		t.Fatalf("legacy config feed defaults = %+v", loaded.Feeds)
	}
	partial := `{"port":48100,"data_dir":"` + dir + `","lang":"ru","roots":[],"feeds":{"enabled":true}}`
	if err := os.WriteFile(path, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err = LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig partial feeds: %v", err)
	}
	if !loaded.Feeds.Enabled || loaded.Feeds.PeriodDays != 7 || loaded.Feeds.MaxItems != 8 || loaded.Feeds.RetentionDays != 30 {
		t.Fatalf("partial feed config defaults = %+v", loaded.Feeds)
	}

	custom := DefaultConfig()
	custom.Feeds = feeds.Config{
		Enabled: true, RootNames: []string{"photos"}, Weekdays: []int{1, 3, 5}, Times: []string{"09:15", "18:30"},
		QuietHoursStart: "22:00", QuietHoursEnd: "08:00", PeriodDays: 14, MaxItems: 6, RetentionDays: 45,
	}
	if err := SaveConfig(path, custom); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	roundTrip, _, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig round-trip: %v", err)
	}
	if !reflect.DeepEqual(roundTrip.Feeds, custom.Feeds) {
		t.Fatalf("round-trip feed config = %+v, want %+v", roundTrip.Feeds, custom.Feeds)
	}
	var persisted map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted["feeds"]; !ok {
		t.Fatal("config.json has no feeds object")
	}
}

func feedSettingsValues() url.Values {
	return url.Values{
		"action":               {"feeds"},
		"feeds_enabled":        {"true"},
		"feeds_roots":          {"share"},
		"feeds_weekdays":       {"1"},
		"feeds_times":          {"09:15,18:30"},
		"feeds_quiet_start":    {"22:00"},
		"feeds_quiet_end":      {"08:00"},
		"feeds_period_days":    {"7"},
		"feeds_max_items":      {"8"},
		"feeds_retention_days": {"30"},
	}
}

func feedLoginClient(t *testing.T, srv *httptest.Server, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	resp, err := client.PostForm(srv.URL+"/login", url.Values{"username": {username}, "password": {password}})
	if err != nil {
		t.Fatalf("login %q: %v", username, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %q status = %d", username, resp.StatusCode)
	}
	return client
}

func TestSettingsFeedAdminOnly(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.store.AddUser("reader", "reader-pass", "user"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()

	reader := feedLoginClient(t, srv, "reader", "reader-pass")
	resp, err := reader.PostForm(srv.URL+"/settings", url.Values{"action": {"lang"}, "lang": {"en"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || a.cfg.Lang != "en" {
		t.Fatalf("non-admin language save status=%d lang=%q", resp.StatusCode, a.cfg.Lang)
	}
	resp, err = reader.PostForm(srv.URL+"/settings", feedSettingsValues())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin feed save status=%d, want 403", resp.StatusCode)
	}
	if a.cfg.Feeds.Enabled || len(a.cfg.Feeds.RootNames) != 0 {
		t.Fatalf("non-admin changed feed config: %+v", a.cfg.Feeds)
	}

	admin := feedLoginClient(t, srv, "admin", "1234567890")
	resp, err = admin.PostForm(srv.URL+"/settings", feedSettingsValues())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !a.cfg.Feeds.Enabled || !reflect.DeepEqual(a.cfg.Feeds.RootNames, []string{"share"}) {
		t.Fatalf("admin feed save status=%d config=%+v", resp.StatusCode, a.cfg.Feeds)
	}
	persisted, _, err := LoadConfig(a.cfgPath)
	if err != nil {
		t.Fatalf("load saved settings: %v", err)
	}
	if !reflect.DeepEqual(persisted.Feeds, a.cfg.Feeds) {
		t.Fatalf("persisted feed config = %+v, want %+v", persisted.Feeds, a.cfg.Feeds)
	}
}

func TestSettingsFeedValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(url.Values)
	}{
		{name: "unknown root", change: func(v url.Values) { v.Set("feeds_roots", "missing") }},
		{name: "invalid weekday", change: func(v url.Values) { v.Set("feeds_weekdays", "8") }},
		{name: "invalid time", change: func(v url.Values) { v.Set("feeds_times", "25:61") }},
		{name: "invalid enabled flag", change: func(v url.Values) { v.Set("feeds_enabled", "definitely") }},
		{name: "too many card items", change: func(v url.Values) { v.Set("feeds_max_items", "9") }},
		{name: "incomplete quiet hours", change: func(v url.Values) { v.Set("feeds_quiet_end", "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestApp(t)
			srv := httptest.NewServer(a)
			defer srv.Close()
			admin := feedLoginClient(t, srv, "admin", "1234567890")
			values := feedSettingsValues()
			tc.change(values)
			resp, err := admin.PostForm(srv.URL+"/settings", values)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("invalid feed config status=%d, want 400", resp.StatusCode)
			}
			if a.cfg.Feeds.Enabled {
				t.Fatalf("invalid feed config was applied: %+v", a.cfg.Feeds)
			}
		})
	}
}

func TestFeedSchedulerRespectsSlotsAndQuietHours(t *testing.T) {
	a, sharePath := newTestApp(t)
	now := time.Date(2025, time.January, 13, 12, 15, 0, 0, time.Local)
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	cfg := feeds.DefaultConfig()
	cfg.Enabled = true
	cfg.RootNames = []string{"share"}
	cfg.Weekdays = []int{weekday}
	cfg.Times = []string{now.Format("15:04")}
	cfg.QuietHoursStart = now.Add(-time.Minute).Format("15:04")
	cfg.QuietHoursEnd = now.Add(time.Minute).Format("15:04")
	a.cfgMu.Lock()
	a.cfg.Feeds = cfg
	a.cfgMu.Unlock()
	a.feeds.SetConfig(cfg)

	photo := filepath.Join(sharePath, "memory.jpg")
	if err := os.WriteFile(photo, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(photo, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := a.runFeedSchedulerOnce(context.Background(), now); err != nil {
		t.Fatalf("quiet-hour RunDue: %v", err)
	}
	cards, _ := a.feeds.List("admin", time.Time{}, 10)
	if len(cards) != 0 {
		t.Fatalf("cards inside quiet hours = %d, want 0", len(cards))
	}

	cfg.QuietHoursStart, cfg.QuietHoursEnd = "", ""
	a.cfgMu.Lock()
	a.cfg.Feeds = cfg
	a.cfgMu.Unlock()
	a.feeds.SetConfig(cfg)
	for i := 0; i < 2; i++ {
		runAt := now.Add(time.Duration(i) * 30 * time.Second)
		if err := a.runFeedSchedulerOnce(context.Background(), runAt); err != nil {
			t.Fatalf("RunDue %d: %v", i+1, err)
		}
	}
	cards, _ = a.feeds.List("admin", time.Time{}, 10)
	if len(cards) != 1 {
		t.Fatalf("cards after repeated due tick = %d, want one", len(cards))
	}
	if err := a.runFeedSchedulerOnce(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	cards, _ = a.feeds.List("admin", time.Time{}, 10)
	if len(cards) != 1 {
		t.Fatalf("cards on non-slot minute = %d, want one", len(cards))
	}
	if !strings.Contains(cards[0].Media[0].Path, "memory.jpg") {
		t.Fatalf("scheduler card media = %+v", cards[0].Media)
	}
}

func TestSettingsFeedControlsLocalizeForRussianAndEnglish(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	admin := feedLoginClient(t, srv, "admin", "1234567890")

	resp, err := admin.Get(srv.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Воспоминания") || !strings.Contains(string(body), "Корни для сканирования") {
		t.Fatalf("Russian feed settings missing: status=%d", resp.StatusCode)
	}

	resp, err = admin.PostForm(srv.URL+"/settings", url.Values{"action": {"lang"}, "lang": {"en"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = admin.Get(srv.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Memories") || !strings.Contains(string(body), "Roots to scan") {
		t.Fatalf("English feed settings missing: status=%d", resp.StatusCode)
	}
}
