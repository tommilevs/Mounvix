package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const auditRetention = 1000
const auditRetentionAge = 30 * 24 * time.Hour

type AuditEvent struct {
	Seq      int64        `json:"seq"`
	At       time.Time    `json:"at"`
	Actor    string       `json:"actor"`
	Phase    string       `json:"phase"`
	Action   string       `json:"action"`
	PluginID string       `json:"plugin_id"`
	Source   PluginSource `json:"source,omitempty"`
	RunID    string       `json:"run_id,omitempty"`
	Outcome  string       `json:"outcome,omitempty"`
}
type auditState struct {
	LatestSeq int64        `json:"latest_seq"`
	Items     []AuditEvent `json:"items"`
}
type AuditStore struct {
	mu    sync.Mutex
	path  string
	state auditState
}

func NewAuditStore(dataDir string) (*AuditStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &AuditStore{path: filepath.Join(dataDir, "plugin-audit.json")}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, err
		}
		if s.state.Items == nil {
			s.state.Items = []AuditEvent{}
		}
		return s, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.state.Items = []AuditEvent{}
	return s, nil
}

// RecordAudit appends a bounded, path-free plugin operation audit event.
func (r *Registry) RecordAudit(event AuditEvent) error {
	if r == nil || r.audit == nil {
		return errors.New("plugin audit store is unavailable")
	}
	event.Actor = strings.TrimSpace(event.Actor)
	if event.Actor == "" || len(event.Actor) > 128 || !pluginIDPattern.MatchString(event.PluginID) {
		return errors.New("invalid plugin audit identity")
	}
	if event.Phase != "intent" && event.Phase != "result" {
		return errors.New("invalid plugin audit phase")
	}
	switch event.Action {
	case "install", "update", "enable", "disable", "remove", "run":
	default:
		return errors.New("invalid plugin audit action")
	}
	if event.RunID != "" && !runIDPattern.MatchString(event.RunID) {
		return errors.New("invalid plugin audit run id")
	}
	if event.RunID != "" {
		return r.audit.AppendOnce(event)
	}
	return r.audit.Append(event)
}

func (s *AuditStore) Append(event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLocked(event)
}

func (s *AuditStore) AppendOnce(event AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.RunID != "" {
		for _, existing := range s.state.Items {
			if existing.Actor == event.Actor && existing.Phase == event.Phase && existing.Action == event.Action && existing.PluginID == event.PluginID && existing.Source == event.Source && existing.RunID == event.RunID && existing.Outcome == event.Outcome {
				return nil
			}
		}
	}
	return s.appendLocked(event)
}

func (s *AuditStore) appendLocked(event AuditEvent) error {
	previous := s.state
	previous.Items = append([]AuditEvent(nil), s.state.Items...)
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	} else {
		event.At = event.At.UTC()
	}
	s.state.LatestSeq++
	event.Seq = s.state.LatestSeq
	s.state.Items = append(s.state.Items, event)
	s.trimLocked(time.Now().UTC())
	if err := s.saveLocked(); err != nil {
		s.state = previous
		return err
	}
	return nil
}
func (s *AuditStore) Recent(limit int) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		return []AuditEvent{}, nil
	}
	start := 0
	if len(s.state.Items) > limit {
		start = len(s.state.Items) - limit
	}
	out := append([]AuditEvent(nil), s.state.Items[start:]...)
	return out, nil
}
func (s *AuditStore) trimLocked(now time.Time) {
	kept := s.state.Items[:0]
	for _, e := range s.state.Items {
		if now.Sub(e.At) <= auditRetentionAge {
			kept = append(kept, e)
		}
	}
	if len(kept) > auditRetention {
		kept = kept[len(kept)-auditRetention:]
	}
	s.state.Items = kept
	// Keep records in ascending sequence order even after loading a diagnostic journal.
	sort.Slice(s.state.Items, func(i, j int) bool { return s.state.Items[i].Seq < s.state.Items[j].Seq })
}
func (s *AuditStore) saveLocked() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
