package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/plugins"
	"golang.org/x/net/websocket"
)

type notificationAPIItem struct {
	EventID     string    `json:"event_id"`
	Seq         int64     `json:"seq"`
	CreatedAt   time.Time `json:"created_at"`
	Kind        string    `json:"kind"`
	Summary     string    `json:"summary"`
	TargetRoute string    `json:"target_route"`
	IsRead      bool      `json:"is_read"`
}

type notificationAPIPage struct {
	Items        []notificationAPIItem `json:"items"`
	NextCursor   *int64                `json:"next_cursor"`
	BeforeCursor *int64                `json:"before_cursor"`
	Reset        bool                  `json:"reset"`
	OldestSeq    int64                 `json:"oldest_seq"`
	LatestSeq    int64                 `json:"latest_seq"`
	UnreadCount  int                   `json:"unread_count"`
}

func (a *App) handleNotificationsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie, Origin, Sec-Fetch-Site, Referer")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if a.notifications == nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	after, before, limit, err := parseNotificationQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := a.notifications.ListPage(u.Name, after, before, limit)
	if err != nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	response := notificationAPIPage{
		Items: make([]notificationAPIItem, 0, len(page.Items)), NextCursor: page.NextCursor,
		BeforeCursor: page.BeforeCursor, Reset: page.Reset, OldestSeq: page.OldestSeq,
		LatestSeq: page.LatestSeq, UnreadCount: page.UnreadCount,
	}
	for _, item := range page.Items {
		response.Items = append(response.Items, notificationAPIItem{
			EventID: item.EventID, Seq: item.Seq, CreatedAt: item.CreatedAt,
			Kind: item.Kind, Summary: item.Summary, TargetRoute: a.routePath(item.TargetRoute), IsRead: item.IsRead,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(response)
}

func (a *App) handleNotificationsWebSocket(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if a.notifications == nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	if !a.sameOrigin(r) {
		http.Error(w, "same-origin WebSocket required", http.StatusForbidden)
		return
	}
	server := websocket.Server{
		Handshake: func(_ *websocket.Config, request *http.Request) error {
			if !a.sameOrigin(request) {
				return errors.New("same-origin WebSocket required")
			}
			return nil
		},
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			conn.PayloadType = websocket.TextFrame
			defer conn.Close()
			updates, unsubscribe := a.notifications.Subscribe(8)
			defer unsubscribe()
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				buffer := make([]byte, 1024)
				for {
					if _, err := conn.Read(buffer); err != nil {
						return
					}
				}
			}()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-a.ctx.Done():
					return
				case <-readDone:
					return
				case event, open := <-updates:
					if !open {
						return
					}
					if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
						return
					}
					message := struct {
						Type    string `json:"type"`
						EventID string `json:"event_id"`
						Seq     int64  `json:"seq"`
					}{Type: "notification", EventID: event.EventID, Seq: event.Seq}
					if err := websocket.JSON.Send(conn, message); err != nil {
						return
					}
				}
			}
		}),
	}
	server.ServeHTTP(w, r)
}

func parseNotificationQuery(r *http.Request) (after, before *int64, limit int, err error) {
	query := r.URL.Query()
	if query.Has("after") && query.Has("before") {
		return nil, nil, 0, errors.New("after and before are mutually exclusive")
	}
	parseCursor := func(key string) (*int64, error) {
		if !query.Has(key) {
			return nil, nil
		}
		value, parseErr := strconv.ParseInt(query.Get(key), 10, 64)
		if parseErr != nil || value < 0 {
			return nil, errors.New("invalid " + key + " cursor")
		}
		return &value, nil
	}
	if after, err = parseCursor("after"); err != nil {
		return nil, nil, 0, err
	}
	if before, err = parseCursor("before"); err != nil {
		return nil, nil, 0, err
	}
	limit = 50
	if query.Has("limit") {
		limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			return nil, nil, 0, errors.New("limit must be between 1 and 100")
		}
	}
	return after, before, limit, nil
}

func (a *App) handleNotificationReadAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := a.cookieUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !u.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if a.notifications == nil {
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/notifications/")
	if !strings.HasSuffix(suffix, "/read") {
		http.NotFound(w, r)
		return
	}
	eventID := strings.TrimSuffix(suffix, "/read")
	if eventID == "" || strings.Contains(eventID, "/") || eventID == "ws" {
		http.NotFound(w, r)
		return
	}
	if err := a.notifications.MarkRead(u.Name, eventID); err != nil {
		if errors.Is(err, plugins.ErrNotificationNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "notification inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
