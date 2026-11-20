package plugins

import (
	"context"
	"crypto/rand"
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
	"unicode/utf8"
)

var (
	ErrIdempotencyConflict   = errors.New("idempotency key conflicts with an earlier request")
	ErrDuplicateScheduleSlot = errors.New("scheduled job slot already exists")
	ErrJobStoreClosed        = errors.New("job manager is closed")
	ErrPluginJobsDisabled    = errors.New("plugin jobs are disabled")
)

const (
	jobQueueCapacity  = 100
	jobRetentionLimit = 1000
	jobRetentionAge   = 30 * 24 * time.Hour
)

// JobStatus describes a durable job lifecycle state.
type JobStatus string

const (
	JobStatusQueued      JobStatus = "queued"
	JobStatusRunning     JobStatus = "running"
	JobStatusSucceeded   JobStatus = "succeeded"
	JobStatusFailed      JobStatus = "failed"
	JobStatusCancelled   JobStatus = "cancelled"
	JobStatusInterrupted JobStatus = "interrupted"
)

// JobRecord is a safe public view of a job; payload and idempotency material are never serialized.
type JobRecord struct {
	Seq                uint64          `json:"seq"`
	RunID              string          `json:"run_id"`
	ActorID            string          `json:"-"`
	Kind               string          `json:"kind"`
	PluginID           string          `json:"plugin_id,omitempty"`
	JobID              string          `json:"job_id"`
	Operation          string          `json:"operation,omitempty"`
	ScheduledSlot      string          `json:"scheduled_slot,omitempty"`
	Payload            json.RawMessage `json:"-"`
	Status             JobStatus       `json:"status"`
	StartedAt          time.Time       `json:"started_at,omitempty"`
	FinishedAt         time.Time       `json:"finished_at,omitempty"`
	Outcome            string          `json:"outcome,omitempty"`
	Summary            string          `json:"summary,omitempty"`
	IdempotencyKeyHash string          `json:"-"`
	RequestDigest      string          `json:"-"`
	CreatedAt          time.Time       `json:"created_at"`
}

// JobRequest contains the minimum non-secret data needed to execute a job.
type JobRequest struct {
	ActorID        string
	Kind           string
	PluginID       string
	JobID          string
	Operation      string
	ScheduledSlot  string
	Payload        json.RawMessage
	IdempotencyKey string
}

type persistedJobRecord struct {
	Record             JobRecord       `json:"record"`
	ActorID            string          `json:"actor_id,omitempty"`
	IdempotencyKeyHash string          `json:"idempotency_key_hash,omitempty"`
	RequestDigest      string          `json:"request_digest,omitempty"`
	Payload            json.RawMessage `json:"payload,omitempty"`
}

type jobJournal struct {
	Version   int                  `json:"version"`
	LatestSeq uint64               `json:"latest_seq"`
	Records   []persistedJobRecord `json:"records"`
}

// JobPage is a cursor-based journal result.
type JobPage struct {
	Items      []JobRecord `json:"items"`
	NextCursor uint64      `json:"next_cursor"`
	Reset      bool        `json:"reset"`
	OldestSeq  uint64      `json:"oldest_seq"`
	LatestSeq  uint64      `json:"latest_seq"`
}

// JobStore persists lifecycle transitions in DataDir/plugin-jobs.json.
type JobStore struct {
	mu      sync.Mutex
	path    string
	journal jobJournal
}

// NewJobStore loads the journal and reconciles running work as interrupted.
func NewJobStore(dataDir string) (*JobStore, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data directory is required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create job data directory: %w", err)
	}
	s := &JobStore{path: filepath.Join(dataDir, "plugin-jobs.json"), journal: jobJournal{Version: 1, Records: []persistedJobRecord{}}}
	b, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read job journal: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(b, &s.journal); err != nil {
			return nil, fmt.Errorf("parse job journal: %w", err)
		}
		if s.journal.Version != 1 || s.journal.Records == nil {
			return nil, errors.New("unsupported or incomplete job journal")
		}
	}
	changed := false
	for i := range s.journal.Records {
		item := &s.journal.Records[i]
		item.Record.ActorID = item.ActorID
		item.Record.IdempotencyKeyHash = item.IdempotencyKeyHash
		item.Record.RequestDigest = item.RequestDigest
		item.Record.Payload = append(json.RawMessage(nil), item.Payload...)
		r := &item.Record
		if r.Status == JobStatusRunning {
			r.Status, r.Outcome, r.Summary, r.FinishedAt = JobStatusInterrupted, "interrupted", "process was active during restart", time.Now().UTC()
			s.nextSeqLocked(r)
			changed = true
		}
		if r.Seq > s.journal.LatestSeq {
			s.journal.LatestSeq = r.Seq
			changed = true
		}
	}
	beforeTrim := len(s.journal.Records)
	if err := s.trimLocked(time.Now().UTC()); err != nil {
		return nil, err
	}
	if len(s.journal.Records) != beforeTrim {
		changed = true
	}
	if changed || !fileExists(s.path) {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func normalizeJobRequest(request JobRequest) (JobRequest, error) {
	if request.ActorID == "" || request.JobID == "" || (request.Kind != "plugin" && request.Kind != "rclone" && request.Kind != "package") {
		return JobRequest{}, errors.New("invalid job request")
	}
	if request.Kind == "rclone" {
		request.PluginID = ""
		if request.JobID != "rclone.sync" {
			return JobRequest{}, errors.New("invalid rclone job id")
		}
	}
	return request, nil
}

func jobRequestDigests(request JobRequest) (keyHash, requestDigest string, err error) {
	digestInput := struct {
		Actor, Kind, PluginID, JobID, Operation, Slot string
		Payload                                       json.RawMessage
	}{request.ActorID, request.Kind, request.PluginID, request.JobID, request.Operation, request.ScheduledSlot, request.Payload}
	digestBytes, err := json.Marshal(digestInput)
	if err != nil {
		return "", "", fmt.Errorf("digest job request: %w", err)
	}
	digest := sha256.Sum256(digestBytes)
	if request.IdempotencyKey != "" {
		key := sha256.Sum256([]byte(request.IdempotencyKey))
		keyHash = hex.EncodeToString(key[:])
	}
	return keyHash, hex.EncodeToString(digest[:]), nil
}

func (s *JobStore) findIdempotency(ctx context.Context, request JobRequest) (JobRecord, bool, error) {
	if ctx == nil {
		return JobRecord{}, false, errors.New("job context is required")
	}
	if err := ctx.Err(); err != nil {
		return JobRecord{}, false, err
	}
	request, err := normalizeJobRequest(request)
	if err != nil {
		return JobRecord{}, false, err
	}
	if request.IdempotencyKey == "" {
		return JobRecord{}, false, nil
	}
	keyHash, digest, err := jobRequestDigests(request)
	if err != nil {
		return JobRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return JobRecord{}, false, err
	}
	for _, item := range s.journal.Records {
		r := item.Record
		if r.ActorID != request.ActorID || r.IdempotencyKeyHash != keyHash {
			continue
		}
		if r.RequestDigest != digest {
			return JobRecord{}, false, ErrIdempotencyConflict
		}
		return safeJobRecord(r), true, nil
	}
	return JobRecord{}, false, nil
}

// Enqueue records a new job, deduplicating idempotency keys and scheduled slots.
func (s *JobStore) Enqueue(ctx context.Context, request JobRequest) (JobRecord, error) {
	if ctx == nil {
		return JobRecord{}, errors.New("job context is required")
	}
	if err := ctx.Err(); err != nil {
		return JobRecord{}, err
	}
	request, err := normalizeJobRequest(request)
	if err != nil {
		return JobRecord{}, err
	}
	keyHash, digest, err := jobRequestDigests(request)
	if err != nil {
		return JobRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return JobRecord{}, err
	}
	for _, item := range s.journal.Records {
		r := item.Record
		if keyHash != "" && r.ActorID == request.ActorID && r.IdempotencyKeyHash == keyHash {
			if r.RequestDigest != digest {
				return JobRecord{}, ErrIdempotencyConflict
			}
			return safeJobRecord(r), nil
		}
		if request.ScheduledSlot != "" && r.PluginID == request.PluginID && r.JobID == request.JobID && r.ScheduledSlot == request.ScheduledSlot {
			return JobRecord{}, ErrDuplicateScheduleSlot
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return JobRecord{}, fmt.Errorf("generate run id: %w", err)
	}
	r := JobRecord{RunID: "run-" + hex.EncodeToString(random[:]), ActorID: request.ActorID, Kind: request.Kind, PluginID: request.PluginID, JobID: request.JobID, Operation: request.Operation, ScheduledSlot: request.ScheduledSlot, Payload: append(json.RawMessage(nil), request.Payload...), Status: JobStatusQueued, IdempotencyKeyHash: keyHash, RequestDigest: digest, CreatedAt: time.Now().UTC()}
	s.nextSeqLocked(&r)
	s.journal.Records = append(s.journal.Records, persistedJobRecord{Record: r, Payload: r.Payload})
	if err := s.trimLocked(time.Now().UTC()); err != nil {
		return JobRecord{}, err
	}
	if err := s.saveLocked(); err != nil {
		return JobRecord{}, err
	}
	return safeJobRecord(r), nil
}

// MarkRunning durably records a job start before execution.
func (s *JobStore) MarkRunning(ctx context.Context, runID string) error {
	return s.transition(ctx, runID, func(r *JobRecord) error {
		if r.Status != JobStatusQueued {
			return fmt.Errorf("job %s is not queued", runID)
		}
		r.Status = JobStatusRunning
		r.StartedAt = time.Now().UTC()
		return nil
	})
}

// Finish records a safe terminal outcome.
func (s *JobStore) Finish(ctx context.Context, runID, outcome, summary string) error {
	return s.transition(ctx, runID, func(r *JobRecord) error {
		if r.Status != JobStatusRunning && r.Status != JobStatusQueued {
			return fmt.Errorf("job %s is already terminal", runID)
		}
		r.Status = JobStatusSucceeded
		if outcome == "error" {
			r.Status = JobStatusFailed
		}
		r.Outcome = safeSummary(outcome, 128)
		r.Summary = safeSummary(summary, 512)
		r.FinishedAt = time.Now().UTC()
		return nil
	})
}

// Cancel marks a queued or running job cancelled.
func (s *JobStore) Cancel(ctx context.Context, runID string) error {
	return s.transition(ctx, runID, func(r *JobRecord) error {
		if terminalJobStatus(r.Status) {
			return nil
		}
		r.Status = JobStatusCancelled
		r.Outcome = "cancelled"
		r.Summary = "job cancelled"
		r.FinishedAt = time.Now().UTC()
		return nil
	})
}

func (s *JobStore) transition(ctx context.Context, runID string, apply func(*JobRecord) error) error {
	if ctx == nil {
		return errors.New("job context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.journal.Records {
		item := &s.journal.Records[i]
		if item.Record.RunID != runID {
			continue
		}
		if err := apply(&item.Record); err != nil {
			return err
		}
		s.nextSeqLocked(&item.Record)
		if err := s.trimLocked(time.Now().UTC()); err != nil {
			return err
		}
		return s.saveLocked()
	}
	return os.ErrNotExist
}

// ListAfter returns safe job metadata after a sequence cursor.
func (s *JobStore) ListAfter(ctx context.Context, afterSeq uint64, limit int) (JobPage, error) {
	if ctx == nil {
		return JobPage{}, errors.New("job context is required")
	}
	if err := ctx.Err(); err != nil {
		return JobPage{}, err
	}
	if limit < 1 {
		return JobPage{}, errors.New("page limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	page := JobPage{Items: []JobRecord{}, LatestSeq: s.journal.LatestSeq}
	if len(s.journal.Records) > 0 {
		page.OldestSeq = s.journal.Records[0].Record.Seq
	}
	if page.OldestSeq > 0 && afterSeq+1 < page.OldestSeq {
		page.Reset = true
		afterSeq = page.OldestSeq - 1
	}
	for _, item := range s.journal.Records {
		if item.Record.Seq <= afterSeq {
			continue
		}
		if len(page.Items) == limit {
			break
		}
		page.Items = append(page.Items, safeJobRecord(item.Record))
		page.NextCursor = item.Record.Seq
	}
	if len(page.Items) == 0 {
		page.NextCursor = page.LatestSeq
	}
	return page, nil
}

func (s *JobStore) record(runID string) (JobRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.journal.Records {
		if item.Record.RunID == runID {
			return item.Record, true
		}
	}
	return JobRecord{}, false
}

// ActorForRun returns only the actor identifier needed by trusted reconciliation.
func (s *JobStore) ActorForRun(runID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.journal.Records {
		if item.Record.RunID == runID {
			return item.Record.ActorID, true
		}
	}
	return "", false
}

func (s *JobStore) queueRecord(runID string) (JobRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.journal.Records {
		if item.Record.RunID == runID {
			r := item.Record
			r.Payload = append(json.RawMessage(nil), item.Payload...)
			return r, true
		}
	}
	return JobRecord{}, false
}

func (s *JobStore) queuedRecords() []JobRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []JobRecord{}
	for _, item := range s.journal.Records {
		if item.Record.Status == JobStatusQueued {
			r := item.Record
			r.Payload = append(json.RawMessage(nil), item.Payload...)
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

func (s *JobStore) nextSeqLocked(r *JobRecord) { s.journal.LatestSeq++; r.Seq = s.journal.LatestSeq }
func (s *JobStore) trimLocked(now time.Time) error {
	sort.Slice(s.journal.Records, func(i, j int) bool {
		return s.journal.Records[i].Record.Seq < s.journal.Records[j].Record.Seq
	})
	kept := s.journal.Records[:0]
	for _, item := range s.journal.Records {
		if now.Sub(item.Record.CreatedAt) <= jobRetentionAge {
			kept = append(kept, item)
		}
	}
	s.journal.Records = kept
	if len(s.journal.Records) > jobRetentionLimit {
		s.journal.Records = append([]persistedJobRecord(nil), s.journal.Records[len(s.journal.Records)-jobRetentionLimit:]...)
	}
	return nil
}
func (s *JobStore) saveLocked() error {
	for i := range s.journal.Records {
		item := &s.journal.Records[i]
		item.ActorID = item.Record.ActorID
		item.IdempotencyKeyHash = item.Record.IdempotencyKeyHash
		item.RequestDigest = item.Record.RequestDigest
		item.Payload = append(json.RawMessage(nil), item.Record.Payload...)
	}
	b, err := json.MarshalIndent(s.journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode job journal: %w", err)
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create job journal temp: %w", err)
	}
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write job journal: %w", err)
	}
	if err = os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace job journal: %w", err)
	}
	return nil
}
func safeJobRecord(r JobRecord) JobRecord {
	r.ActorID = ""
	r.Payload = nil
	r.IdempotencyKeyHash = ""
	r.RequestDigest = ""
	return r
}
func terminalJobStatus(status JobStatus) bool {
	return status == JobStatusSucceeded || status == JobStatusFailed || status == JobStatusCancelled || status == JobStatusInterrupted
}
func safeSummary(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(string(redactKnownSecrets([]byte(s))), "\uFFFD")
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[:max]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// JobExecutor executes one durable job and returns a safe summary.
type JobExecutor func(context.Context, JobRecord) (summary string, err error)

// JobManager serializes plugin, rclone and package jobs through the shared process gate.
type JobManager struct {
	store     *JobStore
	executors map[string]JobExecutor
	queue     chan JobRecord
	mu        sync.Mutex
	started   bool
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	worker    sync.WaitGroup
	active    map[string]context.CancelFunc
	pending   map[string]struct{}
	disabled  map[string]bool
	onIntent  func(JobRecord) error
	onOutcome func(JobRecord)
}

// NewJobManager creates a bounded one-worker manager with kind-specific executors.
func NewJobManager(store *JobStore, plugin, rclone, packageJob JobExecutor) (*JobManager, error) {
	if store == nil {
		return nil, errors.New("job store is required")
	}
	return &JobManager{store: store, executors: map[string]JobExecutor{"plugin": plugin, "rclone": rclone, "package": packageJob}, queue: make(chan JobRecord, jobQueueCapacity), active: map[string]context.CancelFunc{}, pending: map[string]struct{}{}, disabled: map[string]bool{}}, nil
}

// SetIntentRecorder registers a callback invoked before a queued job becomes executable.
func (m *JobManager) SetIntentRecorder(recorder func(JobRecord) error) {
	m.mu.Lock()
	m.onIntent = recorder
	m.mu.Unlock()
}

// SetOutcomePublisher registers a callback invoked after a terminal job is durable.
func (m *JobManager) SetOutcomePublisher(publisher func(JobRecord)) {
	m.mu.Lock()
	m.onOutcome = publisher
	m.mu.Unlock()
}

func (m *JobManager) publishOutcome(runID string) {
	record, ok := m.store.record(runID)
	if !ok || !terminalJobStatus(record.Status) {
		return
	}
	m.mu.Lock()
	publisher := m.onOutcome
	m.mu.Unlock()
	if publisher != nil {
		publisher(record)
	}
}

// Start restores queued journal entries and starts the sole worker.
func (m *JobManager) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("job manager context is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("job manager already started")
	}
	if m.closed {
		return ErrJobStoreClosed
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	for _, r := range m.store.queuedRecords() {
		if m.onIntent != nil {
			if err := m.onIntent(r); err != nil {
				if cancelErr := m.store.Cancel(context.Background(), r.RunID); cancelErr != nil {
					return errors.Join(fmt.Errorf("record restored job intent: %w", err), cancelErr)
				}
				continue
			}
		}
		select {
		case m.queue <- r:
			m.pending[r.RunID] = struct{}{}
		default:
			return ErrJobQueueFull
		}
	}
	m.worker.Add(1)
	go m.run()
	return nil
}

// FindIdempotency returns a prior durable job for the same actor and request key without enqueueing work.
func (m *JobManager) FindIdempotency(ctx context.Context, request JobRequest) (JobRecord, bool, error) {
	if ctx == nil {
		return JobRecord{}, false, errors.New("job context is required")
	}
	request, err := normalizeJobRequest(request)
	if err != nil {
		return JobRecord{}, false, err
	}
	if _, _, err := jobRequestDigests(request); err != nil {
		return JobRecord{}, false, err
	}
	return m.store.findIdempotency(ctx, request)
}

// Enqueue persists and schedules a job, rejecting overflow deterministically.
func (m *JobManager) Enqueue(ctx context.Context, request JobRequest) (JobRecord, error) {
	if ctx == nil {
		return JobRecord{}, errors.New("job context is required")
	}
	if err := ctx.Err(); err != nil {
		return JobRecord{}, err
	}
	request, err := normalizeJobRequest(request)
	if err != nil {
		return JobRecord{}, err
	}
	if _, _, err := jobRequestDigests(request); err != nil {
		return JobRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || (m.ctx != nil && m.ctx.Err() != nil) {
		return JobRecord{}, ErrJobStoreClosed
	}
	if !m.started {
		return JobRecord{}, errors.New("job manager is not started")
	}
	if request.Kind == "plugin" && m.disabled[request.PluginID] {
		return JobRecord{}, ErrPluginJobsDisabled
	}
	if request.IdempotencyKey != "" {
		existing, found, err := m.store.findIdempotency(ctx, request)
		if err != nil {
			return JobRecord{}, err
		}
		if found {
			if existing.Status != JobStatusQueued {
				return existing, nil
			}
			if _, ok := m.pending[existing.RunID]; ok || m.active[existing.RunID] != nil {
				return existing, nil
			}
			if len(m.queue) >= cap(m.queue) {
				return JobRecord{}, ErrJobQueueFull
			}
			queued, ok := m.store.queueRecord(existing.RunID)
			if !ok {
				return JobRecord{}, os.ErrNotExist
			}
			m.queue <- queued
			m.pending[existing.RunID] = struct{}{}
			return existing, nil
		}
	}
	if len(m.queue) >= cap(m.queue) {
		return JobRecord{}, ErrJobQueueFull
	}
	r, err := m.store.Enqueue(ctx, request)
	if err != nil {
		return JobRecord{}, err
	}
	if r.Status != JobStatusQueued {
		return r, nil
	}
	if _, ok := m.pending[r.RunID]; ok || m.active[r.RunID] != nil {
		return r, nil
	}
	queued, ok := m.store.queueRecord(r.RunID)
	if !ok {
		return JobRecord{}, os.ErrNotExist
	}
	if m.onIntent != nil {
		if err := m.onIntent(queued); err != nil {
			if cancelErr := m.store.Cancel(context.Background(), r.RunID); cancelErr != nil {
				return JobRecord{}, errors.Join(fmt.Errorf("record job intent: %w", err), cancelErr)
			}
			return JobRecord{}, fmt.Errorf("record job intent: %w", err)
		}
	}
	m.queue <- queued
	m.pending[r.RunID] = struct{}{}
	return r, nil
}

func (m *JobManager) run() {
	defer m.worker.Done()
	for {
		select {
		case <-m.ctx.Done():
			m.cancelQueued()
			return
		case r := <-m.queue:
			if m.ctx.Err() != nil {
				m.mu.Lock()
				delete(m.pending, r.RunID)
				m.mu.Unlock()
				if err := m.store.Cancel(context.Background(), r.RunID); err == nil {
					m.publishOutcome(r.RunID)
				}
				continue
			}
			m.execute(r)
		}
	}
}
func (m *JobManager) execute(r JobRecord) {
	ctx, cancel := context.WithCancel(m.ctx)
	m.mu.Lock()
	if m.closed || (r.Kind == "plugin" && m.disabled[r.PluginID]) {
		delete(m.pending, r.RunID)
		m.mu.Unlock()
		cancel()
		if err := m.store.Cancel(context.Background(), r.RunID); err == nil {
			m.publishOutcome(r.RunID)
		}
		return
	}
	delete(m.pending, r.RunID)
	m.active[r.RunID] = cancel
	m.mu.Unlock()
	defer func() { cancel(); m.mu.Lock(); delete(m.active, r.RunID); m.mu.Unlock() }()
	if err := m.store.MarkRunning(context.Background(), r.RunID); err != nil {
		return
	}
	runCtx, runCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer runCancel()
	executor := m.executors[r.Kind]
	if executor == nil {
		if err := m.store.Finish(context.Background(), r.RunID, "error", "no executor registered for job kind"); err == nil {
			m.publishOutcome(r.RunID)
		}
		return
	}
	summary, runErr := executor(runCtx, r)
	if ctx.Err() != nil {
		if err := m.store.Cancel(context.Background(), r.RunID); err == nil {
			m.publishOutcome(r.RunID)
		}
		return
	}
	outcome := "success"
	if runErr != nil {
		outcome = "error"
		summary = "job execution failed"
	}
	if err := m.store.Finish(context.Background(), r.RunID, outcome, summary); err == nil {
		m.publishOutcome(r.RunID)
	}
}
func (m *JobManager) cancelQueued() {
	for {
		select {
		case r := <-m.queue:
			m.mu.Lock()
			delete(m.pending, r.RunID)
			m.mu.Unlock()
			if err := m.store.Cancel(context.Background(), r.RunID); err == nil {
				m.publishOutcome(r.RunID)
			}
		default:
			return
		}
	}
}

// Cancel stops one queued or active durable job and waits for active work to reach a terminal state.
func (m *JobManager) Cancel(ctx context.Context, runID string) error {
	if ctx == nil {
		return errors.New("job manager context is required")
	}
	if runID == "" {
		return errors.New("job run id is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	record, exists := m.store.record(runID)
	if !exists {
		m.mu.Unlock()
		return os.ErrNotExist
	}
	if terminalJobStatus(record.Status) {
		m.mu.Unlock()
		return nil
	}
	if cancel := m.active[runID]; cancel != nil {
		cancel()
		m.mu.Unlock()
		return m.waitForCancellation(ctx, runID)
	}
	if _, pending := m.pending[runID]; pending {
		if err := m.store.Cancel(ctx, runID); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("cancel queued job: %w", err)
		}
		delete(m.pending, runID)
		m.mu.Unlock()
		m.publishOutcome(runID)
		return nil
	}
	if err := m.store.Cancel(ctx, runID); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("cancel job: %w", err)
	}
	m.mu.Unlock()
	m.publishOutcome(runID)
	return nil
}

func (m *JobManager) waitForCancellation(ctx context.Context, runID string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(processGracePeriod + time.Second)
	defer timer.Stop()
	for {
		record, exists := m.store.record(runID)
		if !exists {
			return os.ErrNotExist
		}
		if terminalJobStatus(record.Status) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("job did not stop within the cancellation window")
		case <-ticker.C:
		}
	}
}

// DisablePlugin prevents queued work from running and cancels the active plugin child.
func (m *JobManager) DisablePlugin(ctx context.Context, pluginID string) error {
	if ctx == nil {
		return errors.New("job manager context is required")
	}
	if pluginID == "" {
		return errors.New("plugin id is required")
	}
	m.mu.Lock()
	m.disabled[pluginID] = true
	activeRuns := make([]string, 0, len(m.active))
	for runID, cancel := range m.active {
		if record, ok := m.store.record(runID); ok && record.Kind == "plugin" && record.PluginID == pluginID {
			cancel()
			activeRuns = append(activeRuns, runID)
		}
	}
	queue := make([]JobRecord, 0, len(m.queue))
	cancelled := make([]string, 0)
	var result error
	for {
		select {
		case r := <-m.queue:
			delete(m.pending, r.RunID)
			if r.Kind == "plugin" && r.PluginID == pluginID {
				if err := m.store.Cancel(ctx, r.RunID); err != nil {
					queue = append(queue, r)
					result = errors.Join(result, fmt.Errorf("cancel queued plugin job %s: %w", r.RunID, err))
				} else {
					cancelled = append(cancelled, r.RunID)
				}
			} else {
				queue = append(queue, r)
			}
		default:
			for _, queued := range queue {
				m.queue <- queued
				m.pending[queued.RunID] = struct{}{}
			}
			m.mu.Unlock()
			for _, runID := range cancelled {
				m.publishOutcome(runID)
			}
			for _, runID := range activeRuns {
				if err := m.waitForCancellation(ctx, runID); err != nil {
					result = errors.Join(result, fmt.Errorf("wait for plugin job %s to stop: %w", runID, err))
				}
			}
			return result
		}
	}
}

// EnablePlugin allows new work for a previously disabled plugin.
func (m *JobManager) EnablePlugin(pluginID string) {
	m.mu.Lock()
	delete(m.disabled, pluginID)
	m.mu.Unlock()
}

// Close cancels queued and active jobs, waits through the five-second process grace/kill cycle, and allows one second for durable terminal-state cleanup.
func (m *JobManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	for _, cancel := range m.active {
		cancel()
	}
	started := m.started
	m.mu.Unlock()
	if !started {
		return nil
	}
	done := make(chan struct{})
	go func() { m.worker.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(processGracePeriod + time.Second):
		return errors.New("job worker did not stop within the process shutdown window")
	}
}
