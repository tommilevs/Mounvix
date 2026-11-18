// Package ai — опциональный OpenAI-совместимый vision-бэкенд (описание изображений).
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client — клиент chat/completions с поддержкой image_url (data URL).
type Client struct {
	BaseURL string
	Model   string
	Key     string
	HTTP    *http.Client
}

type content struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type message struct {
	Role    string    `json:"role"`
	Content []content `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Describe отправляет изображение модели и возвращает текстовый ответ.
func (c *Client) Describe(ctx context.Context, data []byte, mime, prompt string) (string, error) {
	if c.BaseURL == "" || c.Model == "" {
		return "", errors.New("AI backend not configured")
	}
	if prompt == "" {
		prompt = "Опиши изображение кратко: что на нём, текст и ключевые объекты."
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	reqBody := chatRequest{
		Model: c.Model,
		Messages: []message{{
			Role: "user",
			Content: []content{
				{Type: "text", Text: prompt},
				{Type: "image_url", ImageURL: &imageURL{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}},
			},
		}},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 120 * time.Second}
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("AI HTTP %d: %.300s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("AI response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", errors.New("AI returned empty answer")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
