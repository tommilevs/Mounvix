package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHookStorePersistsPerSubscriberDelivery(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	event := HookEvent{EventID: "upload-1", Kind: "upload.done", Payload: map[string]any{"path": "photo.jpg"}}
	if err := store.Append(event, []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending("alpha")
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	if err := store.MarkDelivered(event.EventID, "alpha"); err != nil {
		t.Fatal(err)
	}
	pending, err = store.Pending("alpha")
	if err != nil || len(pending) != 0 {
		t.Fatalf("delivered event remained pending: %v err=%v", pending, err)
	}
	pending, err = store.Pending("beta")
	if err != nil || len(pending) != 1 {
		t.Fatalf("subscriber state leaked: %v err=%v", pending, err)
	}
	reloaded, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err = reloaded.Pending("beta")
	if err != nil || len(pending) != 1 {
		t.Fatalf("restart lost delivery: %v err=%v", pending, err)
	}
}

func TestHookStoreRetryAfterSaveFailurePersistsEvent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := store.path
	store.path = filepath.Join(dir, "missing", "plugin-hook-events.json")
	event := HookEvent{EventID: "upload-retry", Kind: "upload.done"}
	if err := store.Append(event, []string{"alpha"}); err == nil {
		t.Fatal("append unexpectedly succeeded with missing journal directory")
	}
	store.path = journalPath
	if err := store.Append(event, []string{"alpha"}); err != nil {
		t.Fatalf("retry append: %v", err)
	}
	reloaded, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reloaded.Pending("alpha")
	if err != nil || len(pending) != 1 || pending[0].Event.EventID != event.EventID {
		t.Fatalf("durable retry pending=%+v err=%v", pending, err)
	}
}

func TestHookStoreMarkDeliveredSaveFailureLeavesDeliveryPending(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	event := HookEvent{EventID: "upload-delivery", Kind: "upload.done"}
	if err := store.Append(event, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	journalPath := store.path
	store.path = filepath.Join(dir, "missing", "plugin-hook-events.json")
	if err := store.MarkDelivered(event.EventID, "alpha"); err == nil {
		t.Fatal("mark delivered unexpectedly succeeded with missing journal directory")
	}
	store.path = journalPath
	pending, err := store.Pending("alpha")
	if err != nil || len(pending) != 1 {
		t.Fatalf("failed persistence incorrectly hid pending delivery: %+v err=%v", pending, err)
	}
	if err := store.MarkDelivered(event.EventID, "alpha"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err = reloaded.Pending("alpha")
	if err != nil || len(pending) != 0 {
		t.Fatalf("successful retry remains pending: %+v err=%v", pending, err)
	}
}

func TestHookStoreRejectsUnsupportedHookAndPreservesMalformedJournal(t *testing.T) {
	dir := t.TempDir()
	store, err := NewHookStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(HookEvent{EventID: "bad", Kind: "file.indexed"}, nil); err == nil {
		t.Fatal("file.indexed unexpectedly accepted")
	}
	journal := filepath.Join(dir, "plugin-hook-events.json")
	if err := os.WriteFile(journal, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHookStore(dir); err == nil {
		t.Fatal("malformed journal unexpectedly reset")
	}
	got, err := os.ReadFile(journal)
	if err != nil || string(got) != "{" {
		t.Fatalf("diagnostic journal changed: %q err=%v", got, err)
	}
}
