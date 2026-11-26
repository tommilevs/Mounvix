package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"golang.org/x/net/websocket"
)

func newTestAppWithMount(t *testing.T, basePath, publicScheme string) *App {
	t.Helper()
	root := t.TempDir()
	share := root + "/share"
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = root + "/data"
	seedTestAdmin(t, cfg.DataDir)
	cfg.Roots = []Root{{Name: "share", Path: share}}
	cfg.PublicBasePath = basePath
	cfg.PublicScheme = publicScheme
	a, err := New(cfg, cfg.DataDir+"/config.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	return a
}

func TestMountedNotificationsRoutesAndLoginReturn(t *testing.T) {
	a := newTestAppWithMount(t, "/mounvix", "https")
	login := func(next string) *httptest.ResponseRecorder {
		form := url.Values{"username": {"admin"}, "password": {testAdminPassword}, "next": {next}}
		req := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	rec := login("/mounvix/api/v1/notifications")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/mounvix/api/v1/notifications" {
		t.Fatalf("safe login return status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	var cookie *http.Cookie
	for _, candidate := range rec.Result().Cookies() {
		if candidate.Name == cookieName {
			cookie = candidate
		}
	}
	if cookie == nil || cookie.Path != "/mounvix" || !cookie.Secure {
		t.Fatalf("mounted session cookie=%+v", cookie)
	}
	dashboard := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/", nil)
	dashboard.AddCookie(cookie)
	dashboardRec := httptest.NewRecorder()
	a.ServeHTTP(dashboardRec, dashboard)
	if dashboardRec.Code != http.StatusOK {
		t.Fatalf("mounted dashboard status=%d body=%q", dashboardRec.Code, dashboardRec.Body.String())
	}
	for _, route := range []string{`src="/mounvix/theme.js"`, `href="/mounvix/style.css"`, `href="/mounvix/folders"`, `action="/mounvix/logout"`} {
		if !strings.Contains(dashboardRec.Body.String(), route) {
			t.Errorf("mounted dashboard did not prefix %q", route)
		}
	}
	for _, next := range []string{"//evil.test", "https://evil.test", `\\evil.test`, "/outside", "/mounvix/../outside", "/mounvix/%2e%2e/outside"} {
		if got := login(next).Header().Get("Location"); got != "/mounvix/" {
			t.Errorf("unsafe login return %q redirected to %q", next, got)
		}
	}
	apiLogin := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/api/v1/session", strings.NewReader(`{"username":"admin","password":"`+testAdminPassword+`"}`))
	apiLogin.Header.Set("Content-Type", "application/json")
	apiLoginRec := httptest.NewRecorder()
	a.ServeHTTP(apiLoginRec, apiLogin)
	if apiLoginRec.Code != http.StatusOK {
		t.Fatalf("API login status=%d body=%q", apiLoginRec.Code, apiLoginRec.Body.String())
	}
	var apiCookie *http.Cookie
	for _, candidate := range apiLoginRec.Result().Cookies() {
		if candidate.Name == cookieName {
			apiCookie = candidate
		}
	}
	if apiCookie == nil || apiCookie.Path != "/mounvix" || !apiCookie.Secure {
		t.Fatalf("mounted API session cookie=%+v", apiCookie)
	}
	badLogin := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/api/v1/session", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	badLogin.Header.Set("Content-Type", "application/json")
	badLoginRec := httptest.NewRecorder()
	a.ServeHTTP(badLoginRec, badLogin)
	if badLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid API login status=%d body=%q", badLoginRec.Code, badLoginRec.Body.String())
	}

	prefixed := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/api/v1/notifications", nil)
	prefixed.AddCookie(cookie)
	prefixed.Header.Set("Origin", "https://example.test")
	prefixedRec := httptest.NewRecorder()
	a.ServeHTTP(prefixedRec, prefixed)
	if prefixedRec.Code != http.StatusOK {
		t.Fatalf("prefixed API status=%d body=%q", prefixedRec.Code, prefixedRec.Body.String())
	}
	unprefixed := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications", nil)
	unprefixed.AddCookie(cookie)
	unprefixed.Header.Set("Origin", "https://example.test")
	unprefixed.Header.Set("X-Forwarded-Prefix", "/mounvix")
	unprefixedRec := httptest.NewRecorder()
	a.ServeHTTP(unprefixedRec, unprefixed)
	if unprefixedRec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed API status=%d, want 404", unprefixedRec.Code)
	}
}

func TestMountedPanelMutationRedirectKeepsBasePath(t *testing.T) {
	a := newTestAppWithMount(t, "/mounvix", "")
	req := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/users", strings.NewReader("action=reset&username=missing&newpass=long-password"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: a.store.CreateSession("admin")})
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/mounvix/users" {
		t.Fatalf("mounted users redirect status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestMountedLogoutRevokesSessionAndClearsScopedCookie(t *testing.T) {
	a := newTestAppWithMount(t, "/mounvix", "https")
	token := a.store.CreateSession("admin")
	logout := httptest.NewRequest(http.MethodPost, "http://example.test/mounvix/logout", nil)
	logout.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	logoutRec := httptest.NewRecorder()
	a.ServeHTTP(logoutRec, logout)
	if logoutRec.Code != http.StatusSeeOther || logoutRec.Header().Get("Location") != "/mounvix/login" {
		t.Fatalf("logout status=%d location=%q", logoutRec.Code, logoutRec.Header().Get("Location"))
	}
	cleared := false
	for _, cookie := range logoutRec.Result().Cookies() {
		if cookie.Name == cookieName && cookie.Path == "/mounvix" && cookie.MaxAge < 0 && cookie.Secure {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("logout did not clear mount-scoped session cookie: %v", logoutRec.Result().Cookies())
	}
	replay := httptest.NewRequest(http.MethodGet, "http://example.test/mounvix/users", nil)
	replay.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	replayRec := httptest.NewRecorder()
	a.ServeHTTP(replayRec, replay)
	if replayRec.Code != http.StatusSeeOther || !strings.HasPrefix(replayRec.Header().Get("Location"), "/mounvix/login?next=") {
		t.Fatalf("revoked session replay status=%d location=%q", replayRec.Code, replayRec.Header().Get("Location"))
	}
}

func TestRoutePathAppliesConfiguredPublicBasePath(t *testing.T) {
	a := &App{cfg: Config{PublicBasePath: "/mounvix"}}
	for path, want := range map[string]string{
		"/": "/mounvix/", "/login": "/mounvix/login", "/notifications": "/mounvix/notifications",
		"/api/v1/file?root=share&path=photo.jpg": "/mounvix/api/v1/file?root=share&path=photo.jpg",
		"/api/v1/notifications/ws":               "/mounvix/api/v1/notifications/ws",
	} {
		if got := a.routePath(path); got != want {
			t.Errorf("routePath(%q)=%q, want %q", path, got, want)
		}
	}
}

func TestNotificationsAPIEnforcesAdminAndSameOrigin(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	if err := a.store.AddUser("reader", "reader-pass", "user"); err != nil {
		t.Fatal(err)
	}
	adminCookie := a.store.CreateSession("admin")
	readerCookie := a.store.CreateSession("reader")
	expiredCookie := a.store.CreateSession("admin")
	a.store.mu.Lock()
	a.store.sessions[expiredCookie] = session{User: "admin", Exp: time.Now().Add(-time.Minute)}
	a.store.mu.Unlock()
	request := func(cookie, origin, secFetch, referer string) int {
		req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if secFetch != "" {
			req.Header.Set("Sec-Fetch-Site", secFetch)
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, tc := range []struct {
		name    string
		cookie  string
		origin  string
		fetch   string
		referer string
		want    int
	}{
		{name: "missing session", origin: "http://example.test", want: http.StatusUnauthorized},
		{name: "non-admin", cookie: readerCookie, origin: "http://example.test", want: http.StatusForbidden},
		{name: "expired session", cookie: expiredCookie, origin: "http://example.test", want: http.StatusUnauthorized},
		{name: "foreign origin", cookie: adminCookie, origin: "http://evil.test", want: http.StatusForbidden},
		{name: "null origin", cookie: adminCookie, origin: "null", want: http.StatusForbidden},
		{name: "missing browser context", cookie: adminCookie, want: http.StatusForbidden},
		{name: "same-origin fetch metadata", cookie: adminCookie, fetch: "same-origin", want: http.StatusOK},
		{name: "same-origin referer", cookie: adminCookie, referer: "http://example.test/page", want: http.StatusOK},
		{name: "foreign referer", cookie: adminCookie, referer: "http://evil.test/page", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := request(tc.cookie, tc.origin, tc.fetch, tc.referer); got != tc.want {
				t.Fatalf("GET notifications status=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestLegacyPluginMutationsRequireSameOrigin(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	token := a.store.CreateSession("admin")
	for _, tc := range []struct {
		name   string
		path   string
		body   string
		type_  string
		origin string
		want   int
	}{
		{name: "api foreign origin", path: "/api/v1/plugins", body: `{"id":"missing","enabled":true}`, type_: "application/json", origin: "http://evil.test", want: http.StatusForbidden},
		{name: "api missing origin", path: "/api/v1/plugins", body: `{"id":"missing","enabled":true}`, type_: "application/json", want: http.StatusForbidden},
		{name: "api same origin reaches handler", path: "/api/v1/plugins", body: `{"id":"missing","enabled":true}`, type_: "application/json", origin: "http://example.test", want: http.StatusBadRequest},
		{name: "form foreign origin", path: "/plugins", body: "id=missing&enabled=1", type_: "application/x-www-form-urlencoded", origin: "http://evil.test", want: http.StatusForbidden},
		{name: "form missing origin", path: "/plugins", body: "id=missing&enabled=1", type_: "application/x-www-form-urlencoded", want: http.StatusForbidden},
		{name: "form same origin reaches handler", path: "/plugins", body: "id=missing&enabled=1", type_: "application/x-www-form-urlencoded", origin: "http://example.test", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://example.test"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.type_)
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("mutation status=%d want=%d body=%q", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestOriginGuardCoversPluginInstallationJobAndRcloneMutations(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPatch, path: "/api/v1/plugins/demo"},
		{method: http.MethodPut, path: "/api/v1/plugin-installations/demo"},
		{method: http.MethodDelete, path: "/api/v1/plugin-installations/demo"},
		{method: http.MethodPost, path: "/api/v1/plugin-installations"},
		{method: http.MethodPost, path: "/api/v1/rclone-jobs/run-1"},
		{method: http.MethodPost, path: "/api/v1/jobs/run-1/cancellations"},
		{method: http.MethodPost, path: "/api/v1/plugins/demo/jobs/daily/runs"},
		{method: http.MethodPost, path: "/api/v1/rclone-jobs"},
	} {
		for _, origin := range []struct {
			name  string
			value string
		}{{name: "missing"}, {name: "null", value: "null"}, {name: "foreign", value: "http://evil.test"}} {
			t.Run(tc.method+" "+tc.path+" "+origin.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, "http://example.test"+tc.path, nil)
				if origin.value != "" {
					req.Header.Set("Origin", origin.value)
				}
				rec := httptest.NewRecorder()
				a.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("mutation with Origin %q status=%d want=403", origin.value, rec.Code)
				}
			})
		}
	}
	req := httptest.NewRequest(http.MethodPatch, "http://example.test/api/v1/plugins/demo", nil)
	req.Header.Set("Origin", "http://example.test")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: a.store.CreateSession("admin")})
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("same-origin future route status=%d, want unimplemented-route 404", rec.Code)
	}
}

func TestNotificationsReadAPIIsIdempotentAndPerUser(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	if err := a.store.AddUser("second-admin", "admin-pass", "admin"); err != nil {
		t.Fatal(err)
	}
	event, err := a.notifications.AppendJobOutcome(plugins.JobRecord{
		RunID: "run-read-0123456789", Kind: "plugin", PluginID: "demo", JobID: "backup",
		Status: plugins.JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	postRead := func(eventID, token, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://example.test/api/v1/notifications/"+eventID+"/read", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	adminToken := a.store.CreateSession("admin")
	if rec := postRead(event.EventID, adminToken, "http://evil.test"); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin mark read status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec := postRead(event.EventID, adminToken, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("missing-Origin mark read status=%d body=%q", rec.Code, rec.Body.String())
	}
	for range 2 {
		if rec := postRead(event.EventID, adminToken, "http://example.test"); rec.Code != http.StatusNoContent {
			t.Fatalf("mark read status=%d body=%q", rec.Code, rec.Body.String())
		}
	}
	if rec := postRead("unknown-event", adminToken, "http://example.test"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown read status=%d body=%q", rec.Code, rec.Body.String())
	}
	getPage := func(token string) notificationAPIPage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		req.Header.Set("Origin", "http://example.test")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET notifications status=%d body=%q", rec.Code, rec.Body.String())
		}
		var page notificationAPIPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := getPage(adminToken)
	secondAdmin := getPage(a.store.CreateSession("second-admin"))
	if len(first.Items) != 1 || !first.Items[0].IsRead || first.UnreadCount != 0 {
		t.Fatalf("first user's read state=%+v", first)
	}
	if len(secondAdmin.Items) != 1 || secondAdmin.Items[0].IsRead || secondAdmin.UnreadCount != 1 {
		t.Fatalf("read state leaked between users=%+v", secondAdmin)
	}
}

func TestNotificationsWebSocketOnlyExposesEventIDsAndSequence(t *testing.T) {
	a := newTestAppWithMount(t, "/mounvix", "")
	defer a.Close()
	srv := httptest.NewServer(a)
	defer srv.Close()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(srv.URL, "http")+"/mounvix/api/v1/notifications/ws", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if config.Header == nil {
		config.Header = make(http.Header)
	}
	config.Header.Set("Cookie", (&http.Cookie{Name: cookieName, Value: a.store.CreateSession("admin")}).String())
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("connect notifications WebSocket: %v", err)
	}
	defer conn.Close()
	if _, err := a.notifications.AppendJobOutcome(plugins.JobRecord{
		RunID: "run-ws-0123456789", Kind: "plugin", PluginID: "demo", JobID: "sensitive-job",
		Status: plugins.JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := websocket.JSON.Receive(conn, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 3 {
		t.Fatalf("WebSocket payload leaked fields: %s", mustJSON(payload))
	}
	for _, key := range []string{"type", "event_id", "seq"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("WebSocket payload key %q missing: %s", key, mustJSON(payload))
		}
	}
	if _, ok := payload["summary"]; ok {
		t.Fatalf("summary leaked over WebSocket: %s", mustJSON(payload))
	}
}

func TestNotificationsWebSocketRejectsForeignOriginBeforeUpgrade(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	srv := httptest.NewServer(a)
	defer srv.Close()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(srv.URL, "http")+"/api/v1/notifications/ws", "http://evil.test")
	if err != nil {
		t.Fatal(err)
	}
	if config.Header == nil {
		config.Header = make(http.Header)
	}
	config.Header.Set("Cookie", (&http.Cookie{Name: cookieName, Value: a.store.CreateSession("admin")}).String())
	if conn, err := websocket.DialConfig(config); err == nil {
		conn.Close()
		t.Fatal("foreign-origin WebSocket unexpectedly upgraded")
	}
}

func TestNotificationsWebSocketRequiresAdminAndExplicitSameOrigin(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	if err := a.store.AddUser("reader", "reader-pass", "user"); err != nil {
		t.Fatal(err)
	}
	adminToken := a.store.CreateSession("admin")
	readerToken := a.store.CreateSession("reader")
	for _, tc := range []struct {
		name   string
		token  string
		origin string
		want   int
	}{
		{name: "missing session", origin: "http://example.test", want: http.StatusUnauthorized},
		{name: "non-admin", token: readerToken, origin: "http://example.test", want: http.StatusForbidden},
		{name: "missing Origin", token: adminToken, want: http.StatusForbidden},
		{name: "null Origin", token: adminToken, origin: "null", want: http.StatusForbidden},
		{name: "foreign Origin", token: adminToken, origin: "http://evil.test", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications/ws", nil)
			if tc.token != "" {
				req.AddCookie(&http.Cookie{Name: cookieName, Value: tc.token})
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("WebSocket request status=%d want=%d body=%q", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestNotificationsAPIPaginationAndInvalidCursors(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	for i := 1; i <= 3; i++ {
		if _, err := a.notifications.AppendJobOutcome(plugins.JobRecord{
			RunID: "run-page-0123456789abcdef-" + string(rune('0'+i)), Kind: "rclone", JobID: "rclone.sync",
			Status: plugins.JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	token := a.store.CreateSession("admin")
	get := func(query string) (int, notificationAPIPage) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications"+query, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		req.Header.Set("Origin", "http://example.test")
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		var page notificationAPIPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil && rec.Code == http.StatusOK {
			t.Fatal(err)
		}
		return rec.Code, page
	}
	for _, tc := range []struct {
		query string
		want  []int64
	}{
		{query: "?after=1&limit=2", want: []int64{2, 3}},
		{query: "?before=3&limit=2", want: []int64{2, 1}},
		{query: "?limit=2", want: []int64{3, 2}},
	} {
		status, page := get(tc.query)
		if status != http.StatusOK || len(page.Items) != len(tc.want) {
			t.Fatalf("GET %s status=%d page=%+v", tc.query, status, page)
		}
		for i, seq := range tc.want {
			if page.Items[i].Seq != seq {
				t.Errorf("GET %s item[%d].seq=%d, want %d", tc.query, i, page.Items[i].Seq, seq)
			}
		}
	}
	status, page := get("?after=3")
	if status != http.StatusOK || len(page.Items) != 0 || page.Reset || page.LatestSeq != 3 {
		t.Fatalf("cursor at latest status=%d page=%+v", status, page)
	}
	status, page = get("?after=99")
	if status != http.StatusOK || len(page.Items) != 0 || !page.Reset || page.NextCursor == nil || *page.NextCursor != 3 {
		t.Fatalf("cursor past latest status=%d page=%+v", status, page)
	}
	for _, query := range []string{"?after=nope", "?after=1&before=2", "?after=-1", "?limit=0", "?limit=101", "?limit=nope"} {
		if status, _ := get(query); status != http.StatusBadRequest {
			t.Errorf("GET %s status=%d, want 400", query, status)
		}
	}
}

func TestNotificationsAPIResetBelowRetainedHistory(t *testing.T) {
	root := t.TempDir()
	dataDir := root + "/data"
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := struct {
		Version   int                         `json:"version"`
		LatestSeq int64                       `json:"latest_seq"`
		Items     []plugins.NotificationEvent `json:"items"`
	}{
		Version: 1, LatestSeq: 6,
		Items: []plugins.NotificationEvent{
			{EventID: "event-5", Seq: 5, CreatedAt: time.Now().UTC(), Kind: "plugin", Summary: "Plugin job succeeded.", TargetRoute: "/notifications", RunID: "run-5", Outcome: "succeeded"},
			{EventID: "event-6", Seq: 6, CreatedAt: time.Now().UTC(), Kind: "plugin", Summary: "Plugin job succeeded.", TargetRoute: "/notifications", RunID: "run-6", Outcome: "succeeded"},
		},
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataDir+"/plugin-notifications.json", encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	shareDir := root + "/share"
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dataDir
	cfg.Roots = []Root{{Name: "share", Path: shareDir}}
	a, err := New(cfg, dataDir+"/config.json")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications?after=3", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: a.store.CreateSession("admin")})
	req.Header.Set("Origin", "http://example.test")
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	var page notificationAPIPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !page.Reset || len(page.Items) != 0 || page.NextCursor == nil || *page.NextCursor != 6 {
		t.Fatalf("stale retained-history cursor status=%d page=%+v body=%q", rec.Code, page, rec.Body.String())
	}
}

func TestNotificationsAPIContract(t *testing.T) {
	a, _ := newTestApp(t)
	defer a.Close()
	if _, err := a.notifications.AppendJobOutcome(plugins.JobRecord{
		RunID: "run-0123456789abcdef", Kind: "plugin", PluginID: "demo", JobID: "backup",
		Status: plugins.JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/notifications?limit=10", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: a.store.CreateSession("admin")})
	req.Header.Set("Origin", "http://example.test")
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET notifications status=%d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response headers=%v", rec.Header())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected CORS header: %v", rec.Header())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 7 {
		t.Fatalf("unexpected top-level response fields: %s", rec.Body.String())
	}
	for _, key := range []string{"items", "next_cursor", "before_cursor", "reset", "oldest_seq", "latest_seq", "unread_count"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("required response key %q missing: %s", key, rec.Body.String())
		}
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(payload["items"], &items); err != nil || len(items) != 1 {
		t.Fatalf("items=%s err=%v", payload["items"], err)
	}
	if len(items[0]) != 7 {
		t.Fatalf("unexpected notification item fields: %s", rec.Body.String())
	}
	for _, key := range []string{"event_id", "seq", "created_at", "kind", "summary", "target_route", "is_read"} {
		if _, ok := items[0][key]; !ok {
			t.Errorf("required item key %q missing: %s", key, rec.Body.String())
		}
	}
	for _, key := range []string{"run_id", "plugin_id", "job_id", "outcome"} {
		if _, ok := items[0][key]; ok {
			t.Errorf("internal item field %q leaked: %s", key, rec.Body.String())
		}
	}
	for _, field := range []struct {
		key    string
		target any
	}{
		{key: "event_id", target: new(string)}, {key: "seq", target: new(int64)},
		{key: "created_at", target: new(string)}, {key: "kind", target: new(string)},
		{key: "summary", target: new(string)}, {key: "target_route", target: new(string)},
		{key: "is_read", target: new(bool)},
	} {
		if err := json.Unmarshal(items[0][field.key], field.target); err != nil {
			t.Errorf("item field %q has wrong type: %v", field.key, err)
		}
	}
	var nextCursor, beforeCursor *int64
	var reset bool
	var oldestSeq, latestSeq int64
	var unreadCount int
	for _, field := range []struct {
		key    string
		target any
	}{
		{key: "next_cursor", target: &nextCursor}, {key: "before_cursor", target: &beforeCursor},
		{key: "reset", target: &reset}, {key: "oldest_seq", target: &oldestSeq},
		{key: "latest_seq", target: &latestSeq}, {key: "unread_count", target: &unreadCount},
	} {
		if err := json.Unmarshal(payload[field.key], field.target); err != nil {
			t.Errorf("response field %q has wrong type: %v", field.key, err)
		}
	}
	if !strings.Contains(rec.Body.String(), "Plugin job succeeded.") {
		t.Fatalf("safe summary missing: %s", rec.Body.String())
	}
}
