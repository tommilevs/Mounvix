package plugins

import (
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

const (
	hookRetentionLimit = 1000
	hookRetentionAge   = 30 * 24 * time.Hour
)

// HookEvent is a durable, safe event delivered to subscribed process plugins.
type HookEvent struct {
	EventID   string         `json:"event_id"`
	Kind      string         `json:"kind"`
	CreatedAt time.Time      `json:"created_at"`
	Payload   map[string]any `json:"payload,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	PluginID  string         `json:"plugin_id,omitempty"`
}

// HookDelivery carries an event and its target subscriber.
type HookDelivery struct {
	Event        HookEvent `json:"event"`
	SubscriberID string    `json:"subscriber_id"`
}

type hookJournal struct {
	Version           int          `json:"version"`
	HookOverflowCount uint64       `json:"hook_overflow_count"`
	Events            []hookRecord `json:"events"`
}
type hookRecord struct {
	Event       HookEvent       `json:"event"`
	Subscribers []string        `json:"subscribers"`
	Delivered   map[string]bool `json:"delivered"`
}

// HookStore persists hook events and per-subscriber delivery state.
type HookStore struct {
	mu      sync.Mutex
	path    string
	journal hookJournal
}

// NewHookStore loads a diagnostic-preserving hook journal.
func NewHookStore(dataDir string) (*HookStore, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create hook data directory: %w", err)
	}
	s := &HookStore{path: filepath.Join(dataDir, "plugin-hook-events.json"), journal: hookJournal{Version: 1, Events: []hookRecord{}}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hook journal: %w", err)
	}
	if err = json.Unmarshal(b, &s.journal); err != nil {
		return nil, fmt.Errorf("parse hook journal: %w", err)
	}
	if s.journal.Version != 1 || s.journal.Events == nil {
		return nil, errors.New("unsupported or incomplete hook journal")
	}
	return s, nil
}

// Append durably appends an allowed hook for a stable, sorted subscriber set.
func (s *HookStore) Append(event HookEvent, subscriberIDs []string) error {
	if s == nil {
		return errors.New("hook store is unavailable")
	}
	if event.EventID == "" || !allowedHook(event.Kind) {
		return errors.New("invalid hook event")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}
	subs := append([]string(nil), subscriberIDs...)
	sort.Strings(subs)
	uniq := subs[:0]
	for _, id := range subs {
		if id == "" {
			return errors.New("empty hook subscriber")
		}
		if len(uniq) == 0 || uniq[len(uniq)-1] != id {
			uniq = append(uniq, id)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.journal.Events {
		if record.Event.EventID == event.EventID {
			return nil
		}
	}
	previous := cloneHookJournal(s.journal)
	s.journal.Events = append(s.journal.Events, hookRecord{Event: event, Subscribers: append([]string(nil), uniq...), Delivered: map[string]bool{}})
	s.trimLocked()
	if err := s.saveLocked(); err != nil {
		s.journal = previous
		return err
	}
	return nil
}

// Pending returns undelivered events for one subscriber in creation order.
func (s *HookStore) Pending(pluginID string) ([]HookDelivery, error) {
	if s == nil {
		return nil, errors.New("hook store is unavailable")
	}
	if pluginID == "" {
		return nil, errors.New("plugin id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []HookDelivery{}
	for _, record := range s.journal.Events {
		for _, id := range record.Subscribers {
			if id == pluginID && !record.Delivered[id] {
				out = append(out, HookDelivery{Event: record.Event, SubscriberID: id})
				break
			}
		}
	}
	return out, nil
}

// MarkDelivered records a successful plugin.handleEvent response.
func (s *HookStore) MarkDelivered(eventID, pluginID string) error {
	if s == nil {
		return errors.New("hook store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.journal.Events {
		r := &s.journal.Events[i]
		if r.Event.EventID != eventID {
			continue
		}
		found := false
		for _, id := range r.Subscribers {
			if id == pluginID {
				found = true
				break
			}
		}
		if !found {
			return errors.New("hook delivery not found")
		}
		if r.Delivered == nil {
			r.Delivered = map[string]bool{}
		}
		previous := cloneHookJournal(s.journal)
		r.Delivered[pluginID] = true
		if err := s.saveLocked(); err != nil {
			s.journal = previous
			return err
		}
		return nil
	}
	return errors.New("hook event not found")
}

// OverflowCount reports undelivered event loss due to bounded retention.
func (s *HookStore) OverflowCount() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.journal.HookOverflowCount
}
func cloneHookJournal(journal hookJournal) hookJournal {
	clone := journal
	clone.Events = make([]hookRecord, len(journal.Events))
	for i, record := range journal.Events {
		clone.Events[i] = record
		clone.Events[i].Subscribers = append([]string(nil), record.Subscribers...)
		if record.Delivered != nil {
			clone.Events[i].Delivered = make(map[string]bool, len(record.Delivered))
			for id, delivered := range record.Delivered {
				clone.Events[i].Delivered[id] = delivered
			}
		}
	}
	return clone
}

func (s *HookStore) trimLocked() {
	now := time.Now().UTC()
	kept := s.journal.Events[:0]
	for _, r := range s.journal.Events {
		if now.Sub(r.Event.CreatedAt) <= hookRetentionAge {
			kept = append(kept, r)
		} else {
			for _, id := range r.Subscribers {
				if !r.Delivered[id] {
					s.journal.HookOverflowCount++
					break
				}
			}
		}
	}
	s.journal.Events = kept
	if len(s.journal.Events) > hookRetentionLimit {
		for len(s.journal.Events) > hookRetentionLimit {
			idx := -1
			for i, r := range s.journal.Events {
				done := true
				for _, id := range r.Subscribers {
					if !r.Delivered[id] {
						done = false
						break
					}
				}
				if done {
					idx = i
					break
				}
			}
			if idx < 0 {
				idx = 0
				s.journal.HookOverflowCount++
			}
			s.journal.Events = append(s.journal.Events[:idx], s.journal.Events[idx+1:]...)
		}
	}
}
func (s *HookStore) saveLocked() error {
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
