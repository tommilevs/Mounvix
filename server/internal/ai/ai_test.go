package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDescribeSendsImageAndParsesAnswer(t *testing.T) {
	var got chatRequest
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"на фото красный квадрат"}}]}`))
	}))
	defer server.Close()

	c := &Client{BaseURL: server.URL, Model: "vision-test", Key: "secret"}
	answer, err := c.Describe(context.Background(), []byte{1, 2, 3}, "image/png", "Что на фото?")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "на фото красный квадрат" {
		t.Fatalf("answer = %q", answer)
	}
	if auth != "Bearer secret" {
		t.Fatalf("auth = %q", auth)
	}
	if got.Model != "vision-test" || len(got.Messages) != 1 || len(got.Messages[0].Content) != 2 {
		t.Fatalf("request = %+v", got)
	}
	img := got.Messages[0].Content[1]
	if img.Type != "image_url" || img.ImageURL == nil || !strings.HasPrefix(img.ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image content = %+v", img)
	}
	if got.Messages[0].Content[0].Text != "Что на фото?" {
		t.Fatalf("prompt = %q", got.Messages[0].Content[0].Text)
	}
}

func TestDescribeErrors(t *testing.T) {
	if _, err := (&Client{}).Describe(context.Background(), nil, "", ""); err == nil {
		t.Fatal("unconfigured client accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer server.Close()
	c := &Client{BaseURL: server.URL, Model: "m"}
	if _, err := c.Describe(context.Background(), []byte{1}, "image/png", ""); err == nil {
		t.Fatal("500 not reported")
	}
}
