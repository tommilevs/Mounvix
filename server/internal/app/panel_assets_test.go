package app

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPanelStaticAssetRoutes(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	cases := []struct {
		path        string
		contentType string
	}{
		{path: "/style.css", contentType: "text/css; charset=utf-8"},
		{path: "/theme.js", contentType: "text/javascript; charset=utf-8"},
		{path: "/notifications.js", contentType: "text/javascript; charset=utf-8"},
		{path: "/plugins.js", contentType: "text/javascript; charset=utf-8"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := client.Get(srv.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status=%d location=%q, want 200 without authentication", tc.path, resp.StatusCode, resp.Header.Get("Location"))
			}
			if got := resp.Header.Get("Content-Type"); got != tc.contentType {
				t.Errorf("GET %s Content-Type=%q, want %q", tc.path, got, tc.contentType)
			}
			if strings.TrimSpace(string(body)) == "" {
				t.Errorf("GET %s returned an empty asset", tc.path)
			}
		})
	}
}

func TestPanelThemeControlOnLoginAndRegistration(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	for _, path := range []string{"/login", "/register"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status=%d", path, resp.StatusCode)
			}
			for _, want := range []string{`id="theme-select"`, `href="/style.css"`, `src="/theme.js"`} {
				if !strings.Contains(string(body), want) {
					t.Errorf("GET %s missing %q", path, want)
				}
			}
			if strings.Contains(string(body), `class="sidebar"`) {
				t.Errorf("anonymous page %s exposes authenticated navigation", path)
			}
		})
	}
}

func TestPanelAuthenticatedShellContainsSidebarAndThemeControl(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	resp, err := client.PostForm(srv.URL+"/login", url.Values{"username": {"admin"}, "password": {testAdminPassword}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/" {
		t.Fatalf("login redirected to %q, want dashboard", resp.Request.URL.Path)
	}

	resp, err = client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /: status=%d", resp.StatusCode)
	}
	for _, want := range []string{
		`class="sidebar"`, `id="nav-toggle"`, `aria-controls="sidebar"`, `aria-expanded="false"`,
		`id="primary-nav"`, `id="theme-select"`, `id="nav-close"`, `aria-label="Закрыть меню"`, `id="main-content"`, `href="/stats"`, `src="/theme.js"`, `<div class="table-wrap"><table>`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestLoginAndPanelFooterOfferLicenseAndSource(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	const sourceLink = `href="https://github.com/tommilevs/Mounvix"`
	const licenseText = `AGPL-3.0-only`

	resp, err := http.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	loginBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /login: status=%d", resp.StatusCode)
	}
	for _, want := range []string{sourceLink, licenseText} {
		if !strings.Contains(string(loginBody), want) {
			t.Errorf("login page missing source/license offer %q", want)
		}
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	resp, err = client.PostForm(srv.URL+"/login", url.Values{"username": {"admin"}, "password": {testAdminPassword}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/" {
		t.Fatalf("login redirected to %q, want dashboard", resp.Request.URL.Path)
	}

	resp, err = client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	panelBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /: status=%d", resp.StatusCode)
	}
	for _, want := range []string{sourceLink, licenseText} {
		if !strings.Contains(string(panelBody), want) {
			t.Errorf("authenticated panel footer missing source/license offer %q", want)
		}
	}
}

func TestPanelCSSHasThemeAndResponsiveRules(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /style.css: status=%d", resp.StatusCode)
	}
	for _, want := range []string{
		"--canvas", `:root[data-theme="light"]`, `:root[data-theme="system"]`,
		"prefers-color-scheme", ":focus-visible", "@media (max-width: 880px)",
		"overflow-x: auto", "font-variant-numeric: tabular-nums",
		"@media (prefers-reduced-motion: reduce)", "transition-property: none", "z-index: 50", ".sidebar-close",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("panel CSS missing %q", want)
		}
	}
}

func TestPanelScriptContainsThemeAndNavigationHooks(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/theme.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /theme.js: status=%d", resp.StatusCode)
	}
	for _, want := range []string{
		"mounvix_theme", "localStorage.getItem", "localStorage.setItem", "dataset.theme",
		"new Set([\"dark\", \"light\", \"system\"])", "select.value", "dataset.js",
		"aria-current", "aria-expanded", "dataset.open", "nav-close", "inert", "event.key === \"Tab\"", "const focusAfterResize", "closeButton.contains(activeElement)", "DOMContentLoaded", "matchMedia", "Escape",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("panel theme script missing %q", want)
		}
	}
}

func TestPanelMobileNavigationDoesNotHideLinkBeforeDefaultNavigation(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/theme.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /theme.js: status=%d", resp.StatusCode)
	}

	const synchronousLinkClose = `link.addEventListener("click", () => {
        if (isCompact()) setMenuOpen(false);
      });`
	if strings.Contains(string(body), synchronousLinkClose) {
		t.Fatal("compact navigation hides the activated anchor before its default route navigation")
	}
}

func TestPanelCompactDrawerUsesViewportEdgesInsteadOfViewportHeight(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /style.css: status=%d", resp.StatusCode)
	}

	css := string(body)
	start := strings.Index(css, `:root[data-js="true"] .sidebar {`)
	if start < 0 {
		t.Fatal("compact fixed-drawer rule not found")
	}
	rest := css[start:]
	end := strings.Index(rest, "\n  :root[data-js=\"true\"] .sidebar[data-open=\"true\"]")
	if end < 0 {
		t.Fatal("next compact-drawer rule not found")
	}
	rule := rest[:end]
	if !strings.Contains(rule, "\n    top: 0;") || !strings.Contains(rule, "\n    bottom: 0;") {
		t.Fatal("compact drawer must be anchored to both WebView viewport edges so all navigation links remain reachable")
	}
	if strings.Contains(rule, "height: 100vh;") || strings.Contains(rule, "height: 100dvh;") {
		t.Fatal("compact drawer must not depend on viewport-height units that clipped navigation in Android WebView")
	}
}
