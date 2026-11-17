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

func TestRegisterApproveFlow(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/register", url.Values{
		"username": {"newbie"}, "password": {"longpass1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Заявка отправлена") {
		t.Fatalf("register: %d %s", resp.StatusCode, string(body))
	}
	if a.store.Find("newbie") != nil {
		t.Fatal("user created before approval")
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err = c.PostForm(srv.URL+"/login", url.Values{"username": {"newbie"}, "password": {"longpass1"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = c.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "Обзор") {
		t.Fatal("unapproved user reached dashboard")
	}

	pending := a.regs.List()
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
	admin := loginClient(t, srv.URL)
	resp, err = admin.PostForm(srv.URL+"/registrations", url.Values{
		"action": {"approve"}, "id": {pending[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "принят") || a.store.Find("newbie") == nil {
		t.Fatalf("approve failed: %s", string(body))
	}

	c2 := loginClient(t, srv.URL)
	_ = c2
	jar2, _ := cookiejar.New(nil)
	c3 := &http.Client{Jar: jar2}
	resp, err = c3.PostForm(srv.URL+"/login", url.Values{"username": {"newbie"}, "password": {"longpass1"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = c3.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Обзор") {
		t.Fatalf("approved user cannot reach dashboard: %s", string(body))
	}
}

func TestRegisterReject(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	resp, err := http.PostForm(srv.URL+"/register", url.Values{
		"username": {"rejectme"}, "password": {"longpass2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	pending := a.regs.List()
	if len(pending) != 1 {
		t.Fatalf("pending=%d", len(pending))
	}
	admin := loginClient(t, srv.URL)
	resp, err = admin.PostForm(srv.URL+"/registrations", url.Values{
		"action": {"reject"}, "id": {pending[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "отклонена") || a.store.Find("rejectme") != nil {
		t.Fatalf("reject failed: %s", string(body))
	}
}

func TestRegisterShortPasswordRejected(t *testing.T) {
	a, _ := newTestApp(t)
	srv := httptest.NewServer(a)
	defer srv.Close()
	resp, err := http.PostForm(srv.URL+"/register", url.Values{
		"username": {"shorty"}, "password": {"123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "не короче 8") {
		t.Fatalf("short password accepted: %s", string(body))
	}
	if len(a.regs.List()) != 0 {
		t.Fatal("pending created for short password")
	}
}
