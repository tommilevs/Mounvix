package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tommilevs/Mounvix/server/internal/app"
)

func TestHealthApp(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.DataDir = t.TempDir()
	a, err := app.New(cfg, cfg.DataDir+"/config.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
}
