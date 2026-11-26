package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestJobStorePersistsSequenceAndReconcilesRunningJobs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := store.Enqueue(ctx, JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "report", Operation: "run", Payload: []byte(`{"safe":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.Enqueue(ctx, JobRequest{ActorID: "admin", Kind: "rclone", JobID: "rclone.sync", Operation: "sync", Payload: []byte(`{"src":"a","dst":"b"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(ctx, running.RunID); err != nil {
		t.Fatal(err)
	}
	if running.Seq <= queued.Seq {
		t.Fatalf("seq did not increase: queued=%d running=%d", queued.Seq, running.Seq)
	}
	store, err = NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	restoredQueued, ok := store.queueRecord(queued.RunID)
	if !ok {
		t.Fatal("queued job disappeared after restart")
	}
	actor, found := store.ActorForRun(queued.RunID)
	if !found || actor != "admin" {
		t.Fatalf("reconciliation actor = %q, found=%v", actor, found)
	}
	var restoredPayload struct {
		Safe bool `json:"safe"`
	}
	if err := json.Unmarshal(restoredQueued.Payload, &restoredPayload); err != nil {
		t.Fatalf("decode queued payload after restart: %v", err)
	}
	if !restoredPayload.Safe {
		t.Fatalf("queued job payload after restart = %s", restoredQueued.Payload)
	}
	page, err := store.ListAfter(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Status != JobStatusQueued || page.Items[1].Status != JobStatusInterrupted {
		t.Fatalf("restart reconciliation items = %#v", page.Items)
	}
	if page.LatestSeq <= running.Seq {
		t.Fatalf("reconciliation did not advance durable seq: %d <= %d", page.LatestSeq, running.Seq)
	}
}

func TestJobStoreListAfterOrdersLifecycleSequencesWithoutSkipping(t *testing.T) {
	ctx := context.Background()
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Enqueue(ctx, JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "one", JobID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Enqueue(ctx, JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "two", JobID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(ctx, first.RunID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, first.RunID, "success", "done"); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListAfter(ctx, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RunID != second.RunID || page.Items[0].Seq != second.Seq {
		t.Fatalf("first catch-up page = %#v, want second job at seq %d", page.Items, second.Seq)
	}
	if page.OldestSeq != second.Seq || page.NextCursor != second.Seq {
		t.Fatalf("page cursors oldest=%d next=%d, want %d", page.OldestSeq, page.NextCursor, second.Seq)
	}
	page, err = store.ListAfter(ctx, page.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RunID != first.RunID || page.Items[0].Seq <= second.Seq {
		t.Fatalf("second catch-up page = %#v, want first job's later lifecycle sequence", page.Items)
	}
}

func TestJobStoreReturnsPendingPackageOperationPayloadOnlyForInternalRetryHandling(t *testing.T) {
	ctx := context.Background()
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"version":"1.0.0","confirmed":true}`)
	run, err := store.Enqueue(ctx, JobRequest{ActorID: "admin", Kind: "package", PluginID: "demo", JobID: "demo", Operation: "install", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.ListAfter(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(page.Items[0].Payload) != 0 {
		t.Fatalf("public job list leaked operation payload: %+v", page.Items)
	}
	internal, found, err := store.GetPendingPackageOperation(ctx, run.RunID)
	if err != nil || !found {
		t.Fatalf("GetPendingPackageOperation() found=%v err=%v", found, err)
	}
	if string(internal.Payload) != string(payload) {
		t.Fatalf("internal payload=%s, want %s", internal.Payload, payload)
	}
	encoded, err := json.Marshal(internal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"payload"`) || strings.Contains(string(encoded), `1.0.0`) {
		t.Fatalf("internal payload was exposed by safe JSON encoding: %s", encoded)
	}
	if _, found, err := store.GetPendingPackageOperation(ctx, "missing"); err != nil || found {
		t.Fatalf("missing package operation found=%v err=%v", found, err)
	}
}

func TestJobStoreFinishRedactsSummarySecrets(t *testing.T) {
	ctx := context.Background()
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.Enqueue(ctx, JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRunning(ctx, run.RunID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, run.RunID, "success", "Authorization: Bearer top-secret, token: scoped-secret; read /var/lib/mounvix/private.db C:\\ProgramData\\Mounvix\\private.log"); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListAfter(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("listed jobs = %d, want 1", len(page.Items))
	}
	summary := page.Items[0].Summary
	if strings.Contains(summary, "top-secret") || strings.Contains(summary, "scoped-secret") || strings.Contains(summary, "/var/lib/mounvix") || strings.Contains(summary, `C:\\ProgramData`) || !strings.Contains(summary, "[REDACTED]") {
		t.Fatalf("job summary was not safely redacted: %q", summary)
	}
}

func TestJobStoreRejectsCorruptJournalWithoutReplacingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin-jobs.json")
	original := []byte("{not-json")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJobStore(dir); err == nil {
		t.Fatal("corrupt journal accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("corrupt journal was replaced: %q", got)
	}
}

func TestJobStoreIdempotencyAndScheduleSlotUniqueness(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	request := JobRequest{ActorID: "user-1", Kind: "plugin", PluginID: "demo", JobID: "sync", Operation: "run", Payload: []byte(`{"x":1}`), IdempotencyKey: "secret-key"}
	first, err := store.Enqueue(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.Enqueue(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.RunID != first.RunID {
		t.Fatalf("idempotent run=%q, want %q", duplicate.RunID, first.RunID)
	}
	if duplicate.ActorID != "" || len(duplicate.Payload) != 0 || duplicate.IdempotencyKeyHash != "" || duplicate.RequestDigest != "" {
		t.Fatalf("public job record exposed internal request data: %#v", duplicate)
	}
	changed := request
	changed.Payload = []byte(`{"x":2}`)
	if _, err := store.Enqueue(ctx, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different digest error = %v", err)
	}
	scheduled := request
	scheduled.IdempotencyKey = ""
	scheduled.ActorID = "other"
	scheduled.ScheduledSlot = "2026-11-20T10:00:00Z"
	if _, err := store.Enqueue(ctx, scheduled); err != nil {
		t.Fatal(err)
	}
	scheduled.ActorID = "another"
	if _, err := store.Enqueue(ctx, scheduled); !errors.Is(err, ErrDuplicateScheduleSlot) {
		t.Fatalf("duplicate slot error = %v", err)
	}
	contents, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "secret-key") {
		t.Fatalf("raw idempotency key persisted: %s", contents)
	}

	restartedStore, err := NewJobStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	restartedDuplicate, err := restartedStore.Enqueue(ctx, request)
	if err != nil {
		t.Fatalf("enqueue idempotent request after restart: %v", err)
	}
	if restartedDuplicate.RunID != first.RunID {
		t.Fatalf("restart idempotent run=%q, want original run %q", restartedDuplicate.RunID, first.RunID)
	}
	if _, err := restartedStore.Enqueue(ctx, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different digest after restart error = %v", err)
	}
}

func TestJobStoreConcurrentEnqueuePreservesEveryRecordAndSequence(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "package", JobID: "pkg", Operation: "install", Payload: []byte("null")})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListAfter(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != count {
		t.Fatalf("records=%d, want %d", len(page.Items), count)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode=%#o, want 0600", info.Mode().Perm())
	}
	seen := map[uint64]bool{}
	for _, record := range page.Items {
		if seen[record.Seq] {
			t.Fatalf("duplicate seq %d", record.Seq)
		}
		seen[record.Seq] = true
	}
}

func TestJobManagerRejectsQueueOverflowAndCancelsQueuedJobs(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	manager, err := NewJobManager(store, func(ctx context.Context, _ JobRecord) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "cancelled", ctx.Err()
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "active"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("active job did not start")
	}
	for i := 0; i < jobQueueCapacity; i++ {
		if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "work", Payload: []byte(`{}`)}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "overflow"}); !errors.Is(err, ErrJobQueueFull) {
		t.Fatalf("overflow error=%v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListAfter(context.Background(), 0, jobRetentionLimit)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Items {
		if r.Status == JobStatusQueued || r.Status == JobStatusRunning {
			t.Fatalf("job remained nonterminal after close: %#v", r)
		}
	}
}

func TestJobManagerDoesNotQueueDuplicateIdempotentRequests(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	manager, err := NewJobManager(store, func(ctx context.Context, job JobRecord) (string, error) {
		if job.JobID == "block" {
			started <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "done", nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "block"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("blocking job did not start")
	}

	request := JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "dedupe", IdempotencyKey: "retry-key"}
	first, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < jobQueueCapacity-1; i++ {
		filler := JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "filler-" + strconv.Itoa(i)}
		if _, err := manager.Enqueue(context.Background(), filler); err != nil {
			t.Fatalf("enqueue filler %d: %v", i, err)
		}
	}
	duplicate, err := manager.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatalf("retry idempotent request with full queue: %v", err)
	}
	if duplicate.RunID != first.RunID {
		t.Fatalf("duplicate run=%q, want %q", duplicate.RunID, first.RunID)
	}
	if got := len(manager.queue); got != jobQueueCapacity {
		t.Fatalf("queued jobs after idempotent retry=%d, want %d", got, jobQueueCapacity)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerCancelStopsOneActiveJobAndIsIdempotent(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	manager, err := NewJobManager(store, func(ctx context.Context, _ JobRecord) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("plugin job did not start")
	}
	if err := manager.Cancel(context.Background(), job.RunID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if err := manager.Cancel(context.Background(), job.RunID); err != nil {
		t.Fatalf("repeated Cancel() error = %v", err)
	}
	page, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RunID != job.RunID || page.Items[0].Status != JobStatusCancelled {
		t.Fatalf("cancelled job state = %#v", page.Items)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerDisableWaitsForActiveRunToBecomeTerminal(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	canceled := make(chan struct{})
	finish := make(chan struct{})
	manager, err := NewJobManager(store, func(ctx context.Context, _ JobRecord) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		close(canceled)
		<-finish
		return "", ctx.Err()
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "active"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("plugin job did not start")
	}
	disabled := make(chan error, 1)
	go func() { disabled <- manager.DisablePlugin(context.Background(), "demo") }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("active plugin job did not observe cancellation")
	}
	select {
	case err := <-disabled:
		t.Fatalf("DisablePlugin returned before the active run stopped: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(finish)
	select {
	case err := <-disabled:
		if err != nil {
			t.Fatalf("DisablePlugin() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DisablePlugin did not return after active run stopped")
	}
	page, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RunID != job.RunID || page.Items[0].Status != JobStatusCancelled {
		t.Fatalf("job state after disable = %#v", page.Items)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerDisableCancelsActiveAndBlocksNewPluginJobs(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	manager, err := NewJobManager(store, func(ctx context.Context, _ JobRecord) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "work"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("plugin job did not start")
	}
	queuedJob, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.DisablePlugin(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "next"}); !errors.Is(err, ErrPluginJobsDisabled) {
		t.Fatalf("disabled enqueue error=%v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]JobStatus, len(page.Items))
	for _, item := range page.Items {
		statuses[item.RunID] = item.Status
	}
	if len(page.Items) != 2 || statuses[job.RunID] != JobStatusCancelled || statuses[queuedJob.RunID] != JobStatusCancelled {
		t.Fatalf("disabled plugin job states=%#v", page.Items)
	}
}

func TestJobStoreRetentionKeepsLatestSequenceAndResetsStaleCursor(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "package", JobID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.journal.Records[0].Record.CreatedAt = time.Now().Add(-31 * time.Hour * 24)
	if err := store.saveLocked(); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()
	if _, err := store.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "package", JobID: "new"}); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Reset || len(page.Items) != 1 || page.LatestSeq <= first.Seq {
		t.Fatalf("retention cursor page=%#v", page)
	}
}

func TestJobStoreRetentionCapsRecordsAtOneThousand(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	for seq := uint64(1); seq <= jobRetentionLimit+1; seq++ {
		record := JobRecord{Seq: seq, RunID: "run-" + strconv.FormatUint(seq, 10), ActorID: "admin", Kind: "package", JobID: "pkg", Status: JobStatusSucceeded, CreatedAt: time.Now().UTC()}
		store.journal.Records = append(store.journal.Records, persistedJobRecord{Record: record})
	}
	store.journal.LatestSeq = jobRetentionLimit + 1
	if err := store.trimLocked(time.Now().UTC()); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	if len(store.journal.Records) != jobRetentionLimit {
		store.mu.Unlock()
		t.Fatalf("retained %d records", len(store.journal.Records))
	}
	if err := store.saveLocked(); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()
	reloaded, err := NewJobStore(filepath.Dir(store.path))
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.journal.LatestSeq != jobRetentionLimit+1 {
		t.Fatalf("latest seq=%d", reloaded.journal.LatestSeq)
	}
}

func TestJobManagerExecutorsCanAcquireTheSharedProcessGate(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executed := make(chan struct{}, 1)
	executor := func(ctx context.Context, _ JobRecord) (string, error) {
		release, err := SharedProcessGate().Acquire(ctx, true)
		if err != nil {
			return "", err
		}
		defer release()
		executed <- struct{}{}
		return "done", nil
	}
	manager, err := NewJobManager(store, executor, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "run"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-executed:
	case <-time.After(3 * time.Second):
		if err := manager.Close(); err != nil {
			t.Fatalf("close manager after executor gate timeout: %v", err)
		}
		t.Fatal("job executor could not acquire the shared process gate")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerCloseWaitsForRuntimeForcedKillAndTerminalState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process signal lifecycle is covered by the Windows cross-build")
	}
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pluginRuntime := makeRuntimeTestFixture(t, "demo", "bin/plugin", 0o700, nil)
	manager, err := NewJobManager(store, func(ctx context.Context, job JobRecord) (string, error) {
		_, err := pluginRuntime.Call(ctx, job.PluginID, "plugin.invoke", []byte(`{"hang":true,"ignore_sigterm":true}`))
		return "", err
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	job, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "run"})
	if err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(pluginRuntime.dataDir, "runtime-helper.pid")
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	wait := time.NewTimer(3 * time.Second)
	defer wait.Stop()
	var pid int
	for {
		data, err := os.ReadFile(pidPath)
		if err == nil {
			pid, err = strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("parse helper pid: %v", err)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read helper pid: %v", err)
		}
		select {
		case <-poll.C:
		case <-wait.C:
			t.Fatal("runtime helper did not start before deadline")
		}
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close() returned before runtime killed the child: %v", err)
	}
	page, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RunID != job.RunID || page.Items[0].Status != JobStatusCancelled {
		t.Fatalf("shutdown job state = %#v, want terminal cancelled", page.Items)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("plugin child %d survived JobManager.Close", pid)
	} else if !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("probe child process %d: %v", pid, err)
	}
}

func TestJobManagerDoesNotExecuteWhenIntentRecordingFails(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executed := make(chan struct{}, 1)
	manager, err := NewJobManager(store, func(context.Context, JobRecord) (string, error) {
		executed <- struct{}{}
		return "done", nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.SetIntentRecorder(func(JobRecord) error { return errors.New("audit store unavailable") })
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "work"}); err == nil {
		t.Fatal("Enqueue succeeded without a durable audit intent")
	}
	select {
	case <-executed:
		t.Fatal("job executed after audit intent failure")
	default:
	}
	page, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != JobStatusCancelled {
		t.Fatalf("job state after audit failure = %#v, want one cancelled record", page.Items)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerRecordsIntentForRestoredQueuedJob(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queued, err := store.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "restored"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewJobManager(store, func(context.Context, JobRecord) (string, error) { return "done", nil }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	intent := make(chan JobRecord, 1)
	manager.SetIntentRecorder(func(record JobRecord) error { intent <- record; return nil })
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-intent:
		if record.RunID != queued.RunID || record.ActorID != "admin" || record.Status != JobStatusQueued {
			t.Fatalf("restored job intent = %+v", record)
		}
	case <-time.After(time.Second):
		t.Fatal("restored queued job did not record audit intent")
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerPublishesTerminalRecordAfterDurableSave(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	published := make(chan JobRecord, 1)
	events := make(chan string, 2)
	manager, err := NewJobManager(store, func(context.Context, JobRecord) (string, error) {
		return "done", nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.SetIntentRecorder(func(JobRecord) error {
		events <- "intent"
		return nil
	})
	manager.SetOutcomePublisher(func(record JobRecord) {
		data, err := os.ReadFile(store.path)
		if err != nil {
			t.Errorf("read durable job journal in publisher: %v", err)
			return
		}
		var journal jobJournal
		if err := json.Unmarshal(data, &journal); err != nil {
			t.Errorf("decode durable job journal in publisher: %v", err)
			return
		}
		persisted := false
		for _, item := range journal.Records {
			if item.Record.RunID == record.RunID && item.Record.Status == JobStatusSucceeded {
				persisted = true
			}
		}
		if record.Status != JobStatusSucceeded || !persisted {
			t.Errorf("publisher received non-durable terminal run: record=%#v persisted=%v", record, persisted)
			return
		}
		events <- "result"
		published <- record
	})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: "plugin", PluginID: "demo", JobID: "publish"}); err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-published:
		if record.Status != JobStatusSucceeded {
			t.Fatalf("published status=%s, want succeeded", record.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal job outcome was not published immediately")
	}
	if first, second := <-events, <-events; first != "intent" || second != "result" {
		t.Fatalf("audit callback order = %q, %q; want intent then result", first, second)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJobManagerSerializesMixedJobKinds(t *testing.T) {
	store, err := NewJobStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	active, maxActive, completed := 0, 0, 0
	execJob := func(context.Context, JobRecord) (string, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active--
		completed++
		mu.Unlock()
		return "done", nil
	}
	manager, err := NewJobManager(store, execJob, execJob, execJob)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"plugin", "rclone", "package"} {
		jobID := "run"
		if kind == "rclone" {
			jobID = "rclone.sync"
		}
		if _, err := manager.Enqueue(context.Background(), JobRequest{ActorID: "admin", Kind: kind, PluginID: map[bool]string{true: "", false: "demo"}[kind == "rclone"], JobID: jobID}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		done := completed
		mu.Unlock()
		if done == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("jobs did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if maxActive != 1 {
		t.Fatalf("maximum concurrent jobs=%d, want 1", maxActive)
	}
}
