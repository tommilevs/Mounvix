package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupAPICreateAndRun(t *testing.T) {
	a, shareDir := newTestApp(t)
	if err := os.WriteFile(filepath.Join(shareDir, "backup-me.txt"), []byte("backup-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	srv := httptest.NewServer(a)
	defer srv.Close()
	c := loginClient(t, srv.URL)

	body := `{"name":"qa","source":"` + strings.ReplaceAll(shareDir, `\`, `\\`) + `","targets":[{"type":"local","path":"` + strings.ReplaceAll(target, `\`, `\\`) + `"}]}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/backup/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("create job: %d %s", resp.StatusCode, string(raw))
	}
	var created struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.Job.ID == "" {
		t.Fatalf("bad create response: %s", string(raw))
	}

	resp, err = c.PostForm(srv.URL+"/api/v1/backup/run?id="+url.QueryEscape(created.Job.ID), url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("run job: %d %s", resp.StatusCode, string(raw))
	}
	var run struct {
		Report struct {
			Files int `json:"files"`
		} `json:"report"`
	}
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatalf("bad run response: %s", string(raw))
	}
	if run.Report.Files != 1 {
		t.Fatalf("report files=%d raw=%s", run.Report.Files, string(raw))
	}
	got, err := os.ReadFile(filepath.Join(target, "backup-me.txt"))
	if err != nil || string(got) != "backup-content" {
		t.Fatalf("backup target file: %q %v", got, err)
	}
}
