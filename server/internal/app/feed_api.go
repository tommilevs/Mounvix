package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/feeds"
	"github.com/tommilevs/Mounvix/server/internal/media"
)

const (
	defaultFeedAPILimit = 50
	maxFeedAPILimit     = 100
	maxSessionBodyBytes = 16 << 10
)

type apiSessionRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type feedAPIResponse struct {
	Items     []feedAPICard `json:"items"`
	NextSince time.Time     `json:"next_since"`
}

type feedAPICard struct {
	ID          string         `json:"id"`
	GeneratedAt time.Time      `json:"generated_at"`
	Title       string         `json:"title"`
	Media       []feedAPIMedia `json:"media"`
	IsRead      bool           `json:"is_read"`
}

type feedAPIMedia struct {
	Root         string    `json:"root"`
	Path         string    `json:"path"`
	Kind         string    `json:"kind"`
	ModifiedAt   time.Time `json:"modified_at"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"`
	FileURL      string    `json:"file_url"`
}

func (a *App) handleAPISession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiMethodNotAllowed(w, http.MethodPost)
		return
	}
	var req apiSessionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSessionBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid credentials request")
		return
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid credentials request")
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	u, ok := a.store.Verify(req.Username, req.Password)
	if !ok || u.Disabled {
		a.store.Audit(req.Username, "login_failed", r.RemoteAddr)
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token := a.store.CreateSession(u.Name)
	if token == "" {
		writeAPIError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     a.sessionCookiePath(),
		HttpOnly: true,
		Secure:   a.sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 60 * 60,
	})
	a.store.Audit(u.Name, "login", r.RemoteAddr)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleFeedsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiMethodNotAllowed(w, http.MethodGet)
		return
	}
	u, ok := a.cookieUser(r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	now := time.Now()
	cfg := a.configSnapshot()
	since := now.Add(-time.Duration(cfg.Feeds.RetentionDays) * 24 * time.Hour)
	if cfg.Feeds.RetentionDays <= 0 {
		since = now.Add(-30 * 24 * time.Hour)
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "since must be RFC3339")
			return
		}
		since = parsed
	}
	limit := defaultFeedAPILimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeAPIError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
		if limit > maxFeedAPILimit {
			limit = maxFeedAPILimit
		}
	}

	cards, cursor := a.feeds.List(u.Name, since, limit)
	result := feedAPIResponse{Items: make([]feedAPICard, 0, len(cards)), NextSince: cursor}
	for _, card := range cards {
		visible := a.visibleFeedCard(u, card)
		if len(visible.Media) == 0 {
			continue
		}
		result.Items = append(result.Items, visible)
	}
	writeJSON(w, result)
}

func (a *App) handleFeedReadAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiMethodNotAllowed(w, http.MethodPost)
		return
	}
	u, ok := a.cookieUser(r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	const prefix = "/api/v1/feeds/"
	suffix := strings.TrimPrefix(r.URL.Path, prefix)
	if suffix == r.URL.Path || !strings.HasSuffix(suffix, "/read") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(suffix, "/read")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	card, found := a.feeds.Get(u.Name, id)
	if !found || len(a.visibleFeedCard(u, card).Media) == 0 {
		http.NotFound(w, r)
		return
	}
	marked, err := a.feeds.MarkRead(u.Name, id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "could not mark feed as read")
		return
	}
	if !marked {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) visibleFeedCard(u *User, card feeds.Card) feedAPICard {
	result := feedAPICard{
		ID:          card.ID,
		GeneratedAt: card.GeneratedAt,
		Title:       card.Title,
		IsRead:      card.IsRead,
		Media:       make([]feedAPIMedia, 0, len(card.Media)),
	}
	for _, item := range card.Media {
		if a.userPerm(u, item.Root) == "" {
			continue
		}
		root, ok := a.findRoot(item.Root)
		if !ok {
			continue
		}
		full, err := safeJoin(root.Path, item.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		query := url.Values{}
		query.Set("root", item.Root)
		query.Set("path", filepath.ToSlash(item.Path))
		apiItem := feedAPIMedia{
			Root:       item.Root,
			Path:       filepath.ToSlash(item.Path),
			Kind:       item.Kind,
			ModifiedAt: item.ModifiedAt,
			FileURL:    a.routePath("/api/v1/file?" + query.Encode()),
		}
		if media.Supported(full) {
			apiItem.ThumbnailURL = a.routePath("/api/v1/thumbs?" + query.Encode())
		}
		result.Media = append(result.Media, apiItem)
	}
	return result
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func apiMethodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
}
