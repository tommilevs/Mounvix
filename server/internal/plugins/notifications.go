package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const notificationRetentionLimit = 1000
const notificationRetentionAge = 30 * 24 * time.Hour

// ErrNotificationNotFound is returned when a notification event is unknown.
var ErrNotificationNotFound = errors.New("notification event not found")

// NotificationEvent is a safe terminal plugin or rclone outcome for admins.
type NotificationEvent struct {
	EventID     string    `json:"event_id"`
	Seq         int64     `json:"seq"`
	CreatedAt   time.Time `json:"created_at"`
	Kind        string    `json:"kind"`
	Summary     string    `json:"summary"`
	TargetRoute string    `json:"target_route"`
	RunID       string    `json:"run_id"`
	PluginID    string    `json:"plugin_id,omitempty"`
	JobID       string    `json:"job_id,omitempty"`
	Outcome     string    `json:"outcome"`
	ReadBy      []string  `json:"read_by,omitempty"`
	IsRead      bool      `json:"is_read"`
}

type notificationJournal struct {
	Version   int                 `json:"version"`
	LatestSeq int64               `json:"latest_seq"`
	Items     []NotificationEvent `json:"items"`
}

// NotificationPage is a cursor response over retained notifications.
type NotificationPage struct {
	Items        []NotificationEvent `json:"items"`
	NextCursor   *int64              `json:"next_cursor"`
	BeforeCursor *int64              `json:"before_cursor"`
	Reset        bool                `json:"reset"`
	OldestSeq    int64               `json:"oldest_seq"`
	LatestSeq    int64               `json:"latest_seq"`
	UnreadCount  int                 `json:"unread_count"`
}
type notificationSubscriber struct{ ch chan NotificationEvent }

// NotificationStore persists terminal outcomes, read state, sequence high-watermark, and live subscribers.
type NotificationStore struct {
	mu             sync.Mutex
	path           string
	journal        notificationJournal
	subscribers    map[uint64]notificationSubscriber
	nextSubscriber uint64
	retentionLimit int
}

// NewNotificationStore loads the independent plugin/rclone inbox journal.
func NewNotificationStore(dataDir string) (*NotificationStore, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create notification data directory: %w", err)
	}
	s := &NotificationStore{path: filepath.Join(dataDir, "plugin-notifications.json"), journal: notificationJournal{Version: 1, Items: []NotificationEvent{}}, subscribers: map[uint64]notificationSubscriber{}, retentionLimit: notificationRetentionLimit}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read notification journal: %w", err)
	}
	if err = json.Unmarshal(b, &s.journal); err != nil {
		return nil, fmt.Errorf("parse notification journal: %w", err)
	}
	if s.journal.Version != 1 || s.journal.Items == nil {
		return nil, errors.New("unsupported or incomplete notification journal")
	}
	for _, item := range s.journal.Items {
		if item.Seq > s.journal.LatestSeq {
			s.journal.LatestSeq = item.Seq
		}
	}
	return s, nil
}

// AppendJobOutcome idempotently stores supported terminal plugin or rclone outcomes only.
func (s *NotificationStore) AppendJobOutcome(run JobRecord) (NotificationEvent, error) {
	if s == nil {
		return NotificationEvent{}, errors.New("notification store is unavailable")
	}
	kind, summary, ok := notificationOutcome(run)
	if !ok {
		return NotificationEvent{}, nil
	}
	outcome := string(run.Status)
	if run.Status == JobStatusInterrupted {
		outcome = string(JobStatusFailed)
	}
	sum := sha256.Sum256([]byte(run.RunID + "\x00" + outcome))
	event := NotificationEvent{EventID: "job-" + hex.EncodeToString(sum[:16]), CreatedAt: run.FinishedAt.UTC(), Kind: kind, Summary: summary, TargetRoute: "/notifications", RunID: run.RunID, PluginID: run.PluginID, JobID: run.JobID, Outcome: outcome}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = run.CreatedAt.UTC()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.journal.Items {
		if existing.EventID == event.EventID {
			return existing, nil
		}
	}
	previousSeq := s.journal.LatestSeq
	previousItems := cloneNotificationItems(s.journal.Items)
	s.journal.LatestSeq++
	event.Seq = s.journal.LatestSeq
	s.journal.Items = append(s.journal.Items, event)
	s.trimLocked(time.Now().UTC())
	if err := s.saveLocked(); err != nil {
		s.journal.LatestSeq = previousSeq
		s.journal.Items = previousItems
		return NotificationEvent{}, err
	}
	for id, sub := range s.subscribers {
		select {
		case sub.ch <- event:
		default:
			close(sub.ch)
			delete(s.subscribers, id)
		}
	}
	return event, nil
}
func notificationOutcome(r JobRecord) (string, string, bool) {
	if r.RunID == "" || (r.Status != JobStatusSucceeded && r.Status != JobStatusFailed && r.Status != JobStatusCancelled && r.Status != JobStatusInterrupted) {
		return "", "", false
	}
	state := string(r.Status)
	if r.Status == JobStatusInterrupted {
		state = "failed"
	}
	switch r.Kind {
	case "plugin":
		return "plugin.job." + state, "Plugin job " + state + ".", true
	case "rclone":
		return "rclone.sync." + state, "rclone sync " + state + ".", true
	default:
		return "", "", false
	}
}

// ListPage implements newest, before, and after cursor modes; after gaps request reset.
func (s *NotificationStore) ListPage(userID string, after, before *int64, limit int) (NotificationPage, error) {
	if s == nil {
		return NotificationPage{}, errors.New("notification store is unavailable")
	}
	if userID == "" {
		return NotificationPage{}, errors.New("user id required")
	}
	if after != nil && before != nil {
		return NotificationPage{}, errors.New("after and before are mutually exclusive")
	}
	if limit < 1 || limit > 100 {
		return NotificationPage{}, errors.New("limit must be between 1 and 100")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := NotificationPage{Items: []NotificationEvent{}, LatestSeq: s.journal.LatestSeq, NextCursor: int64Ptr(s.journal.LatestSeq)}
	if len(s.journal.Items) > 0 {
		p.OldestSeq = s.journal.Items[0].Seq
	}
	for _, it := range s.journal.Items {
		if !containsString(it.ReadBy, userID) {
			p.UnreadCount++
		}
	}
	if after != nil && (*after > s.journal.LatestSeq || (p.OldestSeq > 0 && *after < p.OldestSeq-1) || (p.OldestSeq == 0 && s.journal.LatestSeq > 0 && *after < s.journal.LatestSeq)) {
		p.Reset = true
		p.NextCursor = int64Ptr(s.journal.LatestSeq)
		return p, nil
	}
	items := append([]NotificationEvent(nil), s.journal.Items...)
	if after != nil {
		for _, it := range items {
			if it.Seq > *after {
				it.IsRead = containsString(it.ReadBy, userID)
				p.Items = append(p.Items, it)
				if len(p.Items) == limit {
					break
				}
			}
		}
		if len(p.Items) > 0 {
			p.NextCursor = int64Ptr(p.Items[len(p.Items)-1].Seq)
		}
		return p, nil
	}
	if before != nil {
		for i := len(items) - 1; i >= 0; i-- {
			it := items[i]
			if it.Seq >= *before {
				continue
			}
			it.IsRead = containsString(it.ReadBy, userID)
			p.Items = append(p.Items, it)
			if len(p.Items) == limit {
				break
			}
		}
		if len(p.Items) > 0 {
			p.BeforeCursor = int64Ptr(p.Items[len(p.Items)-1].Seq)
		}
		return p, nil
	}
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		it.IsRead = containsString(it.ReadBy, userID)
		p.Items = append(p.Items, it)
		if len(p.Items) == limit {
			break
		}
	}
	return p, nil
}

func cloneNotificationItems(items []NotificationEvent) []NotificationEvent {
	clone := append([]NotificationEvent(nil), items...)
	for i := range clone {
		clone[i].ReadBy = append([]string(nil), items[i].ReadBy...)
	}
	return clone
}

// MarkRead idempotently marks one event read by one user and persists the change.
func (s *NotificationStore) MarkRead(userID, eventID string) error {
	if s == nil {
		return errors.New("notification store is unavailable")
	}
	if userID == "" || eventID == "" {
		return errors.New("user and event id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.journal.Items {
		e := &s.journal.Items[i]
		if e.EventID == eventID {
			if containsString(e.ReadBy, userID) {
				return nil
			}
			previousReadBy := append([]string(nil), e.ReadBy...)
			e.ReadBy = append(e.ReadBy, userID)
			sort.Strings(e.ReadBy)
			if err := s.saveLocked(); err != nil {
				e.ReadBy = previousReadBy
				return err
			}
			return nil
		}
	}
	return ErrNotificationNotFound
}

// Subscribe returns a bounded live stream and idempotent cancellation function.
func (s *NotificationStore) Subscribe(buffer int) (<-chan NotificationEvent, func()) {
	if buffer < 1 {
		buffer = 1
	}
	if buffer > 100 {
		buffer = 100
	}
	ch := make(chan NotificationEvent, buffer)
	if s == nil {
		close(ch)
		return ch, func() {}
	}
	s.mu.Lock()
	s.nextSubscriber++
	id := s.nextSubscriber
	s.subscribers[id] = notificationSubscriber{ch: ch}
	s.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			if sub, ok := s.subscribers[id]; ok {
				close(sub.ch)
				delete(s.subscribers, id)
			}
			s.mu.Unlock()
		})
	}
	return ch, cancel
}
func (s *NotificationStore) trimLocked(now time.Time) {
	kept := s.journal.Items[:0]
	for _, e := range s.journal.Items {
		if now.Sub(e.CreatedAt) <= notificationRetentionAge {
			kept = append(kept, e)
		}
	}
	s.journal.Items = kept
	if len(s.journal.Items) > s.retentionLimit {
		s.journal.Items = append([]NotificationEvent(nil), s.journal.Items[len(s.journal.Items)-s.retentionLimit:]...)
	}
}
func (s *NotificationStore) saveLocked() error {
	b, err := json.MarshalIndent(s.journal, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return err
	}
	return nil
}
func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func int64Ptr(v int64) *int64 { return &v }
