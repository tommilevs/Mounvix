package app

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThumbGalleryAndStream(t *testing.T) {
	a, shareDir := newTestApp(t)
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for x := 0; x < 800; x += 10 {
		for y := 0; y < 600; y += 10 {
			img.Set(x, y, color.RGBA{R: 200, G: 80, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "big.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "t.txt"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a)
	defer srv.Close()
	c := loginClient(t, srv.URL)

	resp, err := http.Get(srv.URL + "/api/v1/thumbs?root=share&path=big.png&w=256")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth thumb: %d, want 401", resp.StatusCode)
	}

	resp, err = c.Get(srv.URL + "/api/v1/thumbs?root=share&path=big.png&w=256")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/jpeg") {
		t.Fatalf("thumb: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("thumb not jpeg: %v", err)
	}
	if cfg.Width > 256 || cfg.Width < 1 {
		t.Fatalf("thumb width = %d", cfg.Width)
	}

	resp, err = c.Get(srv.URL + "/media?root=share")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "big.png") || !strings.Contains(string(body), "/api/v1/thumbs") {
		t.Fatalf("media page: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/file?root=share&path=t.txt", nil)
	req.Header.Set("Range", "bytes=1-3")
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "bcd" {
		t.Fatalf("file range: %d %q", resp.StatusCode, string(body))
	}
}
