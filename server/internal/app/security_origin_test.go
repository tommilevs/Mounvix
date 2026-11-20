package app

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginRequiresExactPublicSchemeAndAuthority(t *testing.T) {
	a := &App{cfg: Config{PublicScheme: "https"}}
	for _, tc := range []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "exact", host: "example.test", origin: "https://example.test", want: true},
		{name: "host case insensitive", host: "example.test", origin: "https://EXAMPLE.TEST", want: true},
		{name: "default port equivalent", host: "example.test", origin: "https://example.test:443", want: true},
		{name: "wrong scheme", host: "example.test", origin: "http://example.test"},
		{name: "wrong host", host: "example.test", origin: "https://evil.test"},
		{name: "suffix host", host: "example.test", origin: "https://example.test.evil"},
		{name: "wrong port", host: "example.test:8443", origin: "https://example.test"},
		{name: "path in Origin", host: "example.test", origin: "https://example.test/path"},
		{name: "opaque origin", host: "example.test", origin: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.test/", nil)
			req.Host = tc.host
			req.Header.Set("Origin", tc.origin)
			if got := a.sameOrigin(req); got != tc.want {
				t.Fatalf("sameOrigin(%q, host %q)=%t, want %t", tc.origin, tc.host, got, tc.want)
			}
		})
	}
}

func TestSameOriginRejectsMultipleOriginHeaders(t *testing.T) {
	a := &App{cfg: Config{PublicScheme: "https"}}
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.Header.Add("Origin", "https://example.test")
	req.Header.Add("Origin", "https://evil.test")
	if a.sameOrigin(req) {
		t.Fatal("multiple Origin headers were accepted")
	}
}

func TestSameOriginBrowserReadRejectsDuplicateFallbackHeaders(t *testing.T) {
	a := &App{cfg: Config{}}
	for _, tc := range []struct {
		name string
		add  func(*http.Request)
	}{
		{name: "Sec-Fetch-Site", add: func(r *http.Request) {
			r.Header.Add("Sec-Fetch-Site", "same-origin")
			r.Header.Add("Sec-Fetch-Site", "same-origin")
		}},
		{name: "Referer", add: func(r *http.Request) {
			r.Header.Add("Referer", "http://example.test/page")
			r.Header.Add("Referer", "http://evil.test/page")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.test/", nil)
			tc.add(req)
			if a.sameOriginOrBrowserRead(req) {
				t.Fatal("duplicate browser fallback headers were accepted")
			}
		})
	}
}

func TestForwardedProtoDoesNotSetOriginSchemeOrSecureCookie(t *testing.T) {
	a := &App{cfg: Config{}}
	req := httptest.NewRequest("GET", "http://example.test/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Origin", "https://example.test")
	if a.sameOrigin(req) {
		t.Fatal("X-Forwarded-Proto changed the expected origin scheme")
	}
	if a.sessionCookieSecure(req) {
		t.Fatal("X-Forwarded-Proto enabled the Secure cookie")
	}
	req.TLS = &tls.ConnectionState{}
	if !a.sessionCookieSecure(req) {
		t.Fatal("direct TLS request did not enable the Secure cookie")
	}
}
