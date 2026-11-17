package share

import (
	"testing"
	"time"
)

func TestCreateGetRevoke(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.Create("share", "a.txt", "admin", time.Hour, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Get(s.Token) == nil {
		t.Fatal("share not found after create")
	}
	if len(m.List()) != 1 {
		t.Fatalf("list len = %d", len(m.List()))
	}
	if !m.Revoke(s.Token) {
		t.Fatal("revoke returned false")
	}
	if m.Get(s.Token) != nil {
		t.Fatal("share still present after revoke")
	}
}

func TestExpiry(t *testing.T) {
	m, _ := New(t.TempDir())
	s, err := m.Create("share", "", "admin", -time.Second, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Get(s.Token) != nil {
		t.Fatal("expired share returned")
	}
}

func TestPasswordCookie(t *testing.T) {
	m, _ := New(t.TempDir())
	s, err := m.Create("share", "", "admin", time.Hour, "secret", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Unlock(s.Token, "bad"); ok {
		t.Fatal("bad password accepted")
	}
	cookie, ok := m.Unlock(s.Token, "secret")
	if !ok || cookie == "" {
		t.Fatal("good password rejected")
	}
	if !m.CheckCookie(s.Token, cookie) {
		t.Fatal("cookie not verified")
	}
	if m.CheckCookie(s.Token, "deadbeef") {
		t.Fatal("tampered cookie accepted")
	}
}
