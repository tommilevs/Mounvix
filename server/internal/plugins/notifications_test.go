package plugins

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotificationStoreOnlyPublishesTerminalPluginAndRcloneOutcomes(t *testing.T) {
	store, err := NewNotificationStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []JobRecord{
		{RunID: "pkg", Kind: "package", Status: JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now()},
		{RunID: "queued", Kind: "plugin", Status: JobStatusQueued},
	} {
		if _, err := store.AppendJobOutcome(record); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListPage("admin", nil, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("non-notifiable outcomes published: %+v", page.Items)
	}
	plugin := JobRecord{RunID: "run-1", Kind: "plugin", PluginID: "demo", JobID: "backup", Status: JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now()}
	first, err := store.AppendJobOutcome(plugin)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.AppendJobOutcome(plugin)
	if err != nil || first.EventID != again.EventID || first.Seq != again.Seq {
		t.Fatalf("duplicate changed identity: first=%+v again=%+v err=%v", first, again, err)
	}
	if err := store.MarkRead("admin", first.EventID); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListPage("admin", nil, nil, 50)
	if err != nil || len(page.Items) != 1 || !page.Items[0].IsRead {
		t.Fatalf("read state not durable: %+v err=%v", page, err)
	}
}

func TestNotificationStoreRetryAfterSaveFailurePersistsEvent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := store.path
	store.path = filepath.Join(dir, "missing", "plugin-notifications.json")
	run := JobRecord{RunID: "run-0123456789abcdef", Kind: "plugin", PluginID: "demo", JobID: "work", Status: JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now()}
	if _, err := store.AppendJobOutcome(run); err == nil {
		t.Fatal("append unexpectedly succeeded with missing journal directory")
	}
	store.path = journalPath
	if _, err := store.AppendJobOutcome(run); err != nil {
		t.Fatalf("retry append: %v", err)
	}
	reloaded, err := NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reloaded.ListPage("admin", nil, nil, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].RunID != run.RunID || page.Items[0].Seq != 1 {
		t.Fatalf("durable retry page=%+v err=%v", page, err)
	}
}

func TestNotificationMarkReadSaveFailureCanBeRetried(t *testing.T) {
	dir := t.TempDir()
	store, err := NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := JobRecord{RunID: "run-0123456789abcdef", Kind: "plugin", PluginID: "demo", JobID: "work", Status: JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now()}
	event, err := store.AppendJobOutcome(run)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := store.path
	store.path = filepath.Join(dir, "missing", "plugin-notifications.json")
	if err := store.MarkRead("admin", event.EventID); err == nil {
		t.Fatal("mark read unexpectedly succeeded with missing journal directory")
	}
	store.path = journalPath
	page, err := store.ListPage("admin", nil, nil, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].IsRead {
		t.Fatalf("failed persistence incorrectly marked read: %+v err=%v", page, err)
	}
	if err := store.MarkRead("admin", event.EventID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err = reloaded.ListPage("admin", nil, nil, 10)
	if err != nil || len(page.Items) != 1 || !page.Items[0].IsRead {
		t.Fatalf("read retry not durable: %+v err=%v", page, err)
	}
}

func TestNotificationSubscribePublishesAfterDurableAppendAndDropsSlowSubscriber(t *testing.T) {
	store, err := NewNotificationStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ch, cancel := store.Subscribe(1)
	defer cancel()
	mk := func(id string) JobRecord {
		return JobRecord{RunID: id, Kind: "plugin", PluginID: "demo", JobID: "work", Status: JobStatusSucceeded, Outcome: "success", FinishedAt: time.Now()}
	}
	first, err := store.AppendJobOutcome(mk("one"))
	if err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.EventID != first.EventID {
		t.Fatalf("live event=%+v, persisted=%+v", got, first)
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("event was published before durable append: %v", err)
	}
	if _, err := store.AppendJobOutcome(mk("two")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendJobOutcome(mk("three")); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; !ok {
		t.Fatal("bounded subscriber closed before its queue filled")
	}
	if _, ok := <-ch; ok {
		t.Fatal("slow subscriber was not disconnected")
	}
}

func TestNotificationSequenceSurvivesRetentionAndRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.retentionLimit = 1
	mk := func(id string) JobRecord {
		return JobRecord{RunID: id, Kind: "rclone", JobID: "rclone.sync", Status: JobStatusSucceeded, Outcome: "succeeded", FinishedAt: time.Now()}
	}
	one, err := store.AppendJobOutcome(mk("one"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppendJobOutcome(mk("two"))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewNotificationStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	three, err := reloaded.AppendJobOutcome(mk("three"))
	if err != nil {
		t.Fatal(err)
	}
	if three.Seq <= one.Seq {
		t.Fatalf("sequence reused after retention: first=%d next=%d", one.Seq, three.Seq)
	}
	page, err := reloaded.ListPage("admin", int64Ptr(0), nil, 50)
	if err != nil || !page.Reset {
		t.Fatalf("expected cursor gap reset: %+v err=%v", page, err)
	}
}
