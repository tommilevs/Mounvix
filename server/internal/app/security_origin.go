package app

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (a *App) routePath(path string) string {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base := a.configSnapshot().PublicBasePath
	if base == "" || path == base || strings.HasPrefix(path, base+"/") {
		return path
	}
	return base + path
}

func (a *App) originRequestAllowed(r *http.Request, path string) bool {
	if path == "/api/v1/notifications" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		return a.sameOriginOrBrowserRead(r)
	}
	if path == "/api/v1/notifications/ws" && r.Method == http.MethodGet {
		return a.sameOrigin(r)
	}
	if (path == "/plugins" || path == "/api/v1/plugins" || strings.HasPrefix(path, "/api/v1/plugins/") ||
		path == "/api/v1/rclone-jobs" || strings.HasPrefix(path, "/api/v1/rclone-jobs/") ||
		path == "/api/v1/plugin-installations" || strings.HasPrefix(path, "/api/v1/plugin-installations/") ||
		strings.HasPrefix(path, "/api/v1/jobs/")) &&
		r.Method != http.MethodGet && r.Method != http.MethodHead {
		return a.sameOrigin(r)
	}
	if strings.HasPrefix(path, "/api/v1/notifications/") && r.Method == http.MethodPost && strings.HasSuffix(path, "/read") {
		return a.sameOrigin(r)
	}
	return true
}

func (a *App) sameOriginOrBrowserRead(r *http.Request) bool {
	if len(r.Header.Values("Origin")) != 0 {
		return a.sameOrigin(r)
	}
	fetchSite := r.Header.Values("Sec-Fetch-Site")
	if len(fetchSite) == 1 && fetchSite[0] == "same-origin" {
		return true
	}
	return a.sameOriginReferer(r)
}

func (a *App) sameOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 {
		return false
	}
	raw := origins[0]
	if raw == "" || strings.EqualFold(raw, "null") {
		return false
	}
	origin, err := url.Parse(raw)
	if err != nil || origin == nil || origin.Opaque != "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	got, ok := canonicalOrigin(origin.Scheme, origin.Host)
	if !ok {
		return false
	}
	want, ok := canonicalOrigin(a.requestScheme(r), r.Host)
	return ok && got == want
}

func (a *App) sameOriginReferer(r *http.Request) bool {
	referers := r.Header.Values("Referer")
	if len(referers) != 1 {
		return false
	}
	referer, err := url.Parse(referers[0])
	if err != nil || referer == nil || referer.Opaque != "" || referer.User != nil || referer.Host == "" {
		return false
	}
	got, ok := canonicalOrigin(referer.Scheme, referer.Host)
	if !ok {
		return false
	}
	want, ok := canonicalOrigin(a.requestScheme(r), r.Host)
	return ok && got == want
}

func canonicalOrigin(scheme, authority string) (string, bool) {
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	u, err := url.Parse(scheme + "://" + authority)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	port := u.Port()
	if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host = net.JoinHostPort(u.Hostname(), port)
	}
	return scheme + "://" + host, true
}

func (a *App) requestScheme(r *http.Request) string {
	if scheme := a.configSnapshot().PublicScheme; scheme != "" {
		return scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (a *App) safeReturnRoute(value string) string {
	fallback := a.routePath("/")
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") ||
		strings.ContainsAny(value, "\\?#%\x00\r\n") || strings.Contains(value, "//") {
		return fallback
	}
	base := a.configSnapshot().PublicBasePath
	if base != "" && value != base && !strings.HasPrefix(value, base+"/") {
		return fallback
	}
	path := strings.TrimPrefix(value, base)
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return fallback
		}
	}
	return value
}

func (a *App) sessionCookiePath() string {
	if path := a.configSnapshot().PublicBasePath; path != "" {
		return path
	}
	return "/"
}

func (a *App) sessionCookieSecure(r *http.Request) bool {
	return a.configSnapshot().PublicScheme == "https" || r.TLS != nil
}
