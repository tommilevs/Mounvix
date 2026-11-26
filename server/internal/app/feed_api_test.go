package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/feeds"
)

func newFeedAPITestApp(t *testing.T) (*App, []feeds.Root, map[string]string) {
	t.Helper()
	base := t.TempDir()
	publicPath := filepath.Join(base, "public")
	privatePath := filepath.Join(base, "private")
	for _, path := range []string{publicPath, privatePath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(base, "data")
	cfg.Roots = []Root{
		{Name: "shared", Path: publicPath},
		{Name: "private", Path: privatePath},
	}
	a, err := New(cfg, filepath.Join(cfg.DataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.AddUser("alice", "alice-pass", "user"); err != nil {
		t.Fatal(err)
	}
	a.store.SetACL("alice", map[string]string{"shared": "r"})
	roots := []feeds.Root{{Name: "shared", Path: publicPath}, {Name: "private", Path: privatePath}}
	return a, roots, map[string]string{"shared": publicPath, "private": privatePath}
}

func apiSessionClient(t *testing.T, srv *httptest.Server, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	payload, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(srv.URL+"/api/v1/session", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create API session: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("API session status=%d, want 200", resp.StatusCode)
	}
	return client
}

func writeFeedFixture(t *testing.T, root, name string, modified time.Time) {
	t.Helper()
	path := filepath.Join(root, name)
	imageData := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			imageData.Set(x, y, color.RGBA{R: 64, G: 128, B: 192, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageData); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func TestAPISessionCreatesMounvixCookie(t *testing.T) {
	a, _, _ := newFeedAPITestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	payload := []byte(`{"username":"alice","password":"alice-pass"}`)
	resp, err := http.Post(srv.URL+"/api/v1/session", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status=%d, want 200", resp.StatusCode)
	}
	var sessionCookie *http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == cookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly || sessionCookie.Path != "/" || sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.MaxAge != 12*60*60 {
		t.Fatalf("mvx cookie = %+v", sessionCookie)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "alice-pass") || strings.Contains(string(body), "password") {
		t.Fatalf("session response exposed credential data: %q", body)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil || result["ok"] != true {
		t.Fatalf("session response = %q, err=%v", body, err)
	}
}

func TestAPISessionRejectsInvalidCredentials(t *testing.T) {
	a, _, _ := newFeedAPITestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	payload := []byte(`{"username":"alice","password":"wrong"}`)
	resp, err := http.Post(srv.URL+"/api/v1/session", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid credentials status=%d, want 401", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatalf("invalid credentials set cookies: %+v", resp.Cookies())
	}
}

func TestFeedsAPIRequiresSessionAndValidatesCursor(t *testing.T) {
	a, _, _ := newFeedAPITestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/feeds")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous feed list status=%d, want 401", resp.StatusCode)
	}
	client := apiSessionClient(t, srv, "alice", "alice-pass")
	resp, err = client.Get(srv.URL + "/api/v1/feeds?since=not-a-time")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d, want 400", resp.StatusCode)
	}
	resp, err = client.Get(srv.URL + "/api/v1/feeds?limit=0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("zero limit status=%d, want 400", resp.StatusCode)
	}
	request, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/feeds/unknown/read", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous read status=%d, want 401", resp.StatusCode)
	}
}

func TestFeedsAPIFiltersACLAndMarksRead(t *testing.T) {
	a, roots, paths := newFeedAPITestApp(t)
	slot1 := time.Now().UTC().Add(-8 * 24 * time.Hour).Truncate(time.Minute)
	slot2 := time.Now().UTC().Truncate(time.Minute)
	writeFeedFixture(t, paths["shared"], "visible.jpg", slot1.Add(-time.Hour))
	writeFeedFixture(t, paths["private"], "hidden.jpg", slot2.Add(-time.Hour))
	cfg := feeds.DefaultConfig()
	cfg.Enabled = true
	cfg.RootNames = []string{"shared", "private"}
	cfg.PeriodDays = 7
	a.feeds.SetConfig(cfg)
	if _, created, err := a.feeds.Generate(context.Background(), roots, slot1); err != nil || !created {
		t.Fatalf("generate shared card: created=%v err=%v", created, err)
	}
	if _, created, err := a.feeds.Generate(context.Background(), roots, slot2); err != nil || !created {
		t.Fatalf("generate private card: created=%v err=%v", created, err)
	}

	srv := httptest.NewServer(a)
	defer srv.Close()
	client := apiSessionClient(t, srv, "alice", "alice-pass")
	resp, err := client.Get(srv.URL + "/api/v1/feeds?limit=8")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("feed list status=%d body=%q", resp.StatusCode, body)
	}
	var result struct {
		Items []struct {
			ID     string `json:"id"`
			IsRead bool   `json:"is_read"`
			Media  []struct {
				Root         string `json:"root"`
				Path         string `json:"path"`
				ThumbnailURL string `json:"thumbnail_url"`
				FileURL      string `json:"file_url"`
			} `json:"media"`
		} `json:"items"`
		NextSince time.Time `json:"next_since"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode feed list: %v body=%q", err, body)
	}
	if len(result.Items) != 1 || len(result.Items[0].Media) != 1 {
		t.Fatalf("ACL-filtered cards = %+v; want only one shared-root card", result.Items)
	}
	if result.NextSince.IsZero() {
		t.Fatal("feed response omitted the synchronization cursor")
	}
	item := result.Items[0].Media[0]
	if item.Root != "shared" || item.Path != "visible.jpg" || item.FileURL == "" || !strings.HasPrefix(item.FileURL, "/api/v1/file?") || !strings.HasPrefix(item.ThumbnailURL, "/api/v1/thumbs?") {
		t.Fatalf("visible media = %+v", item)
	}
	if strings.Contains(string(body), paths["shared"]) || strings.Contains(string(body), paths["private"]) || strings.Contains(string(body), "hidden.jpg") {
		t.Fatalf("feed response leaked private or absolute path: %s", body)
	}

	fileResp, err := client.Get(srv.URL + item.FileURL)
	if err != nil {
		t.Fatal(err)
	}
	fileBody, err := io.ReadAll(fileResp.Body)
	fileResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if fileResp.StatusCode != http.StatusOK || len(fileBody) == 0 {
		t.Fatalf("protected file status=%d body=%d bytes", fileResp.StatusCode, len(fileBody))
	}
	thumbResp, err := client.Get(srv.URL + item.ThumbnailURL)
	if err != nil {
		t.Fatal(err)
	}
	thumbBody, err := io.ReadAll(thumbResp.Body)
	thumbResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if thumbResp.StatusCode != http.StatusOK || len(thumbBody) == 0 {
		t.Fatalf("protected thumbnail status=%d body=%d bytes", thumbResp.StatusCode, len(thumbBody))
	}
	privateURL := "/api/v1/file?root=private&path=hidden.jpg"
	privateResp, err := client.Get(srv.URL + privateURL)
	if err != nil {
		t.Fatal(err)
	}
	privateResp.Body.Close()
	if privateResp.StatusCode != http.StatusForbidden {
		t.Fatalf("private file status=%d, want 403", privateResp.StatusCode)
	}

	readPath := "/api/v1/feeds/" + result.Items[0].ID + "/read"
	readResp, err := client.Post(srv.URL+readPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	readResp.Body.Close()
	if readResp.StatusCode != http.StatusNoContent {
		t.Fatalf("mark-read status=%d, want 204", readResp.StatusCode)
	}
	readResp, err = client.Post(srv.URL+readPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	readResp.Body.Close()
	if readResp.StatusCode != http.StatusNoContent {
		t.Fatalf("repeat mark-read status=%d, want 204", readResp.StatusCode)
	}
	resp, err = client.Get(srv.URL + "/api/v1/feeds")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || !result.Items[0].IsRead {
		t.Fatalf("read state after mark = %+v", result.Items)
	}
	resp, err = client.Post(srv.URL+"/api/v1/feeds/not-found/read", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown-card mark status=%d, want 404", resp.StatusCode)
	}
}

func TestFeedAPIConcurrentWithSettingsSave(t *testing.T) {
	a, _, _ := newFeedAPITestApp(t)
	if err := a.store.AddUser("feed-admin", "admin-pass", "admin"); err != nil {
		t.Fatal(err)
	}
	readerToken := a.store.CreateSession("alice")
	adminToken := a.store.CreateSession("feed-admin")
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 50; i++ {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
			req.AddCookie(&http.Cookie{Name: cookieName, Value: readerToken})
			a.handleFeedsAPI(httptest.NewRecorder(), req)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 50; i++ {
			body := strings.NewReader("action=feeds&feeds_enabled=false&feeds_roots=shared&feeds_weekdays=1&feeds_times=12%3A00&feeds_period_days=7&feeds_max_items=8&feeds_retention_days=30")
			req := httptest.NewRequest(http.MethodPost, "/settings", body)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: cookieName, Value: adminToken})
			a.handleSettings(httptest.NewRecorder(), req)
		}
	}()
	close(start)
	wg.Wait()
}

func TestFeedsAPIFiltersOutCardsWithoutAccessibleMedia(t *testing.T) {
	a, roots, paths := newFeedAPITestApp(t)
	slot := time.Now().UTC().Truncate(time.Minute)
	writeFeedFixture(t, paths["private"], "hidden.jpg", slot.Add(-time.Hour))
	cfg := feeds.DefaultConfig()
	cfg.Enabled = true
	cfg.RootNames = []string{"shared", "private"}
	a.feeds.SetConfig(cfg)
	hiddenCard, created, err := a.feeds.Generate(context.Background(), roots, slot)
	if err != nil || !created {
		t.Fatalf("Generate: created=%v err=%v", created, err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()
	client := apiSessionClient(t, srv, "alice", "alice-pass")
	resp, err := client.Get(srv.URL + "/api/v1/feeds")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(result.Items) != 0 {
		t.Fatalf("feed list status=%d cards=%d; want empty", resp.StatusCode, len(result.Items))
	}
	readResp, err := client.Post(srv.URL+"/api/v1/feeds/"+hiddenCard.ID+"/read", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	readResp.Body.Close()
	if readResp.StatusCode != http.StatusNotFound {
		t.Fatalf("read hidden card status=%d, want 404", readResp.StatusCode)
	}
}
