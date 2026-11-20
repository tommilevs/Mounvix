package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditStoreAtomicAppendRetentionAndMonotonicSequence(t *testing.T) {
	dir := t.TempDir()
	s, err := NewAuditStore(dir)
	if err != nil { t.Fatal(err) }
	for i := 0; i < 1002; i++ {
		if err := s.Append(AuditEvent{At: time.Now().UTC(), Actor: "admin", Phase: "completed", Action: "enable", PluginID: "demo", Outcome: "ok"}); err != nil { t.Fatal(err) }
	}
	events, err := s.Recent(2000)
	if err != nil { t.Fatal(err) }
	if len(events) != 1000 || events[0].Seq != 3 || events[len(events)-1].Seq != 1002 { t.Fatalf("retention/seq: len=%d first=%d last=%d", len(events), events[0].Seq, events[len(events)-1].Seq) }
	s2, err := NewAuditStore(dir)
	if err != nil { t.Fatal(err) }
	if err := s2.Append(AuditEvent{At: time.Now().UTC(), Actor: "admin", Phase: "failed", Action: "run", PluginID: "demo", Outcome: "failed safely"}); err != nil { t.Fatal(err) }
	events, err = s2.Recent(1)
	if err != nil { t.Fatal(err) }
	if len(events) != 1 || events[0].Seq != 1003 { t.Fatalf("restart sequence: %+v", events) }
	b, err := os.ReadFile(filepath.Join(dir, "plugin-audit.json"))
	if err != nil { t.Fatal(err) }
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil { t.Fatal(err) }
	if _, ok := payload["latest_seq"]; !ok { t.Fatal("durable latest_seq missing") }
	text := strings.ToLower(string(b))
	for _, forbidden := range []string{"stdout", "stderr", "token", "secret", "/home/", "file_path"} { if strings.Contains(text, forbidden) { t.Fatalf("audit persisted forbidden field/value %q", forbidden) } }
}
