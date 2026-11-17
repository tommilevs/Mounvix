package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loginClient(t *testing.T, base string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.PostForm(base+"/login", url.Values{"username": {"admin"}, "password": {"1234567890"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	return c
}

func TestSharePublicAndRange(t *testing.T) {
	a, shareDir := newTestApp(t)
	if err := os.WriteFile(filepath.Join(shareDir, "a.txt"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()
	c := loginClient(t, srv.URL)

	resp, err := c.PostForm(srv.URL+"/api/v1/shares", url.Values{
		"root": {"share"}, "path": {"a.txt"}, "hours": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create share: %d %s", resp.StatusCode, string(body))
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.Token == "" {
		t.Fatalf("bad create response: %s", string(body))
	}

	resp, err = http.Get(srv.URL + "/s/" + created.Token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "abcdef" {
		t.Fatalf("public file: %d %q", resp.StatusCode, string(body))
	}

	req, _ := http.NewRequest("GET", srv.URL+"/s/"+created.Token, nil)
	req.Header.Set("Range", "bytes=1-3")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "bcd" {
		t.Fatalf("range: %d %q", resp.StatusCode, string(body))
	}

	resp, err = http.Get(srv.URL + "/s/" + created.Token + "?path=" + url.QueryEscape("../../../../etc/passwd"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("traversal not blocked: %d", resp.StatusCode)
	}
}

func TestSharePasswordFlow(t *testing.T) {
	a, shareDir := newTestApp(t)
	if err := os.WriteFile(filepath.Join(shareDir, "p.txt"), []byte("secret-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()
	c := loginClient(t, srv.URL)
	resp, err := c.PostForm(srv.URL+"/api/v1/shares", url.Values{
		"root": {"share"}, "path": {"p.txt"}, "hours": {"1"}, "password": {"s3cret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var created struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(body, &created)
	if created.Token == "" {
		t.Fatalf("no token: %s", string(body))
	}

	noRedirect := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = noRedirect.Get(srv.URL + "/s/" + created.Token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Пароль") {
		t.Fatalf("password form expected: %d %q", resp.StatusCode, string(body))
	}

	resp, err = noRedirect.PostForm(srv.URL+"/s/"+created.Token, url.Values{"password": {"wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	full := &http.Client{Jar: jar}
	resp, err = full.PostForm(srv.URL+"/s/"+created.Token, url.Values{"password": {"s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "secret-bytes" {
		t.Fatalf("password unlock: %d %q", resp.StatusCode, string(body))
	}
}
