// Package feeds generates and stores scheduled photo/video memory cards.
package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/media"
)

const (
	defaultPeriodDays   = 7
	defaultMaxItems     = 8
	defaultRetentionDay = 30
	maxListItems        = 100
)

// Config controls generation; schedule values are evaluated in the server's local time zone.
type Config struct {
	Enabled         bool     `json:"enabled"`
	RootNames       []string `json:"roots,omitempty"`
	Weekdays        []int    `json:"weekdays,omitempty"`
	Times           []string `json:"times,omitempty"`
	QuietHoursStart string   `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   string   `json:"quiet_hours_end,omitempty"`
	PeriodDays      int      `json:"period_days"`
	MaxItems        int      `json:"max_items"`
	RetentionDays   int      `json:"retention_days"`
}

// DefaultConfig returns the safe, disabled feed defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		PeriodDays:    defaultPeriodDays,
		MaxItems:      defaultMaxItems,
		RetentionDays: defaultRetentionDay,
	}
}

// Root identifies an administrator-configured filesystem root.
type Root struct {
	Name string
	Path string
}

// MediaRef contains only a root name and path relative to that root.
type MediaRef struct {
	Root       string    `json:"root"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	ModifiedAt time.Time `json:"modified_at"`
}

// Card is one generated memories collection.
type Card struct {
	ID          string     `json:"id"`
	GeneratedAt time.Time  `json:"generated_at"`
	Title       string     `json:"title"`
	Media       []MediaRef `json:"media"`
	IsRead      bool       `json:"is_read"`
}

type storedCard struct {
	ID          string          `json:"id"`
	GeneratedAt time.Time       `json:"generated_at"`
	Title       string          `json:"title"`
	Media       []MediaRef      `json:"media"`
	ReadBy      map[string]bool `json:"read_by,omitempty"`
}

type diskState struct {
	Cards          []storedCard `json:"cards"`
	ProcessedSlots []time.Time  `json:"processed_slots,omitempty"`
}

// Manager serializes generation and read-state updates and persists a bounded JSON archive.
type Manager struct {
	generationMu    sync.Mutex
	mu              sync.RWMutex
	dir             string
	path            string
	cfg             Config
	configVersion   uint64
	state           diskState
	scanMaxEntries  int
	scanMaxDuration time.Duration
	scan            scanFunc
}

type candidate struct {
	item MediaRef
}

type scanFunc func(context.Context, []Root, time.Time, Config, int, time.Duration) ([]candidate, error)

// New loads the feed archive from dataDir/feeds.json.
func New(dataDir string, cfg Config) (*Manager, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("feeds: empty data directory")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("feeds: create data directory: %w", err)
	}
	cfg = cloneConfig(withDefaults(cfg))
	m := &Manager{
		dir:             dataDir,
		path:            filepath.Join(dataDir, "feeds.json"),
		cfg:             cfg,
		configVersion:   1,
		state:           diskState{Cards: []storedCard{}, ProcessedSlots: []time.Time{}},
		scanMaxEntries:  20_000,
		scanMaxDuration: 30 * time.Second,
		scan:            scanRoots,
	}
	data, err := os.ReadFile(m.path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("feeds: read archive: %w", err)
	}
	if err := json.Unmarshal(data, &m.state); err != nil {
		return nil, fmt.Errorf("feeds: decode archive: %w", err)
	}
	if m.state.Cards == nil {
		m.state.Cards = []storedCard{}
	}
	if m.state.ProcessedSlots == nil {
		m.state.ProcessedSlots = []time.Time{}
	}
	for i := range m.state.Cards {
		if m.state.Cards[i].ReadBy == nil {
			m.state.Cards[i].ReadBy = map[string]bool{}
		}
	}
	return m, nil
}

// SetConfig atomically replaces the manager's effective settings.
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cloneConfig(withDefaults(cfg))
	m.configVersion++
}

func cloneConfig(cfg Config) Config {
	cfg.RootNames = append([]string(nil), cfg.RootNames...)
	cfg.Weekdays = append([]int(nil), cfg.Weekdays...)
	cfg.Times = append([]string(nil), cfg.Times...)
	return cfg
}

func withDefaults(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.PeriodDays <= 0 {
		cfg.PeriodDays = defaults.PeriodDays
	}
	if cfg.MaxItems <= 0 {
		cfg.MaxItems = defaults.MaxItems
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = defaults.RetentionDays
	}
	return cfg
}

// Generate scans selected roots for media changed within the configured period.
// A scheduled slot is marked processed even when it contains no supported media.
func (m *Manager) Generate(ctx context.Context, roots []Root, slot time.Time) (Card, bool, error) {
	if ctx == nil {
		return Card{}, false, errors.New("feeds: nil context")
	}
	slot = slot.Truncate(time.Minute)
	m.generationMu.Lock()
	defer m.generationMu.Unlock()

	m.mu.RLock()
	cfg := cloneConfig(m.cfg)
	version := m.configVersion
	scanMaxEntries := m.scanMaxEntries
	scanMaxDuration := m.scanMaxDuration
	scanner := m.scan
	if !cfg.Enabled || m.processedLocked(slot) {
		m.mu.RUnlock()
		return Card{}, false, nil
	}
	m.mu.RUnlock()
	if scanner == nil {
		scanner = scanRoots
	}
	candidates, err := scanner(ctx, roots, slot, cfg, scanMaxEntries, scanMaxDuration)
	if ctx.Err() != nil {
		return Card{}, false, fmt.Errorf("feeds: scan cancelled: %w", ctx.Err())
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if version != m.configVersion || !m.cfg.Enabled || m.processedLocked(slot) {
		return Card{}, false, nil
	}
	old := cloneState(m.state)
	card := Card{}
	created := false
	if len(candidates) > 0 {
		selected := sample(candidates, cfg.MaxItems)
		mediaRefs := make([]MediaRef, 0, len(selected))
		for _, c := range selected {
			mediaRefs = append(mediaRefs, c.item)
		}
		card = Card{
			ID:          slot.UTC().Format("20060102T1504Z"),
			GeneratedAt: slot.UTC(),
			Title:       "Воспоминания · " + slot.In(time.Local).Format("02.01.2006"),
			Media:       mediaRefs,
		}
		m.state.Cards = append(m.state.Cards, storedCard{
			ID:          card.ID,
			GeneratedAt: card.GeneratedAt,
			Title:       card.Title,
			Media:       mediaRefs,
			ReadBy:      map[string]bool{},
		})
		created = true
	}
	m.state.ProcessedSlots = append(m.state.ProcessedSlots, slot.UTC())
	m.pruneLocked(slot)
	if saveErr := m.saveLocked(); saveErr != nil {
		m.state = old
		return Card{}, false, saveErr
	}
	if err != nil {
		log.Printf("mounvix feeds: partial scan for slot %s: %v", slot.Format(time.RFC3339), err)
	}
	if created {
		return cloneCard(card), true, nil
	}
	return Card{}, false, nil
}

// RunDue generates when now matches a configured weekday/time and is outside server quiet hours.
func (m *Manager) RunDue(ctx context.Context, now time.Time, roots []Root) error {
	m.mu.RLock()
	cfg := cloneConfig(m.cfg)
	m.mu.RUnlock()
	if !cfg.Enabled || !containsInt(cfg.Weekdays, isoWeekday(now)) || !containsString(cfg.Times, now.In(time.Local).Format("15:04")) {
		return nil
	}
	if quiet, err := inQuietHours(now.In(time.Local), cfg.QuietHoursStart, cfg.QuietHoursEnd); err != nil {
		return fmt.Errorf("feeds: invalid quiet hours: %w", err)
	} else if quiet {
		return nil
	}
	_, _, err := m.Generate(ctx, roots, now)
	return err
}

// List returns cards newer than since, including read state for user.
func (m *Manager) List(user string, since time.Time, limit int) ([]Card, time.Time) {
	if limit <= 0 {
		limit = 50
	}
	if limit > maxListItems {
		limit = maxListItems
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	cards := make([]Card, 0, limit)
	for _, stored := range m.state.Cards {
		if !since.IsZero() && !stored.GeneratedAt.After(since) {
			continue
		}
		cards = append(cards, cardForUser(stored, user))
	}
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].GeneratedAt.Equal(cards[j].GeneratedAt) {
			return cards[i].ID < cards[j].ID
		}
		return cards[i].GeneratedAt.Before(cards[j].GeneratedAt)
	})
	if len(cards) > limit {
		cards = cards[:limit]
	}
	cursor := since
	if len(cards) > 0 {
		cursor = cards[len(cards)-1].GeneratedAt
	}
	return cards, cursor
}

// Get returns one card with read state for user.
func (m *Manager) Get(user, id string) (Card, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, stored := range m.state.Cards {
		if stored.ID == id {
			return cardForUser(stored, user), true
		}
	}
	return Card{}, false
}

// MarkRead stores an idempotent per-user read marker; false means the card is unknown.
func (m *Manager) MarkRead(user, id string) (bool, error) {
	if strings.TrimSpace(user) == "" {
		return false, errors.New("feeds: empty username")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.state.Cards {
		if m.state.Cards[i].ID != id {
			continue
		}
		if m.state.Cards[i].ReadBy == nil {
			m.state.Cards[i].ReadBy = map[string]bool{}
		}
		if m.state.Cards[i].ReadBy[user] {
			return true, nil
		}
		m.state.Cards[i].ReadBy[user] = true
		if err := m.saveLocked(); err != nil {
			delete(m.state.Cards[i].ReadBy, user)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func scanRoots(ctx context.Context, roots []Root, slot time.Time, cfg Config, maxEntries int, maxDuration time.Duration) ([]candidate, error) {
	allowed := make(map[string]bool, len(cfg.RootNames))
	for _, name := range cfg.RootNames {
		allowed[name] = true
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	scanCtx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()
	start := time.Now()
	cutoff := slot.Add(-time.Duration(cfg.PeriodDays) * 24 * time.Hour)
	items := make([]candidate, 0)
	entryCount := 0
	var scanErrors []error
	capped := false

	for _, root := range roots {
		if !allowed[root.Name] || root.Name == "" || root.Path == "" {
			continue
		}
		rootPath, err := filepath.Abs(filepath.Clean(root.Path))
		if err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("root %q: %w", root.Name, err))
			continue
		}
		rootInfo, err := os.Lstat(rootPath)
		if err != nil {
			scanErrors = append(scanErrors, fmt.Errorf("root %q: %w", root.Name, err))
			continue
		}
		if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
			scanErrors = append(scanErrors, fmt.Errorf("root %q is not a directory", root.Name))
			continue
		}
		walkErr := filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if scanCtx.Err() != nil || time.Since(start) >= maxDuration {
				capped = true
				return fs.SkipAll
			}
			if walkErr != nil {
				scanErrors = append(scanErrors, fmt.Errorf("root %q path %q: %w", root.Name, path, walkErr))
				if path == rootPath {
					return fs.SkipAll
				}
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if entryCount >= maxEntries {
				capped = true
				return fs.SkipAll
			}
			entryCount++
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				scanErrors = append(scanErrors, fmt.Errorf("root %q path %q: %w", root.Name, path, err))
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			isVideo := media.IsVideo(path)
			if !isVideo && !media.Supported(path) {
				return nil
			}
			modified := info.ModTime().UTC()
			if modified.Before(cutoff) || modified.After(slot) {
				return nil
			}
			rel, err := filepath.Rel(rootPath, path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				if err != nil {
					scanErrors = append(scanErrors, fmt.Errorf("root %q relative path %q: %w", root.Name, path, err))
				}
				return nil
			}
			kind := "image"
			if isVideo {
				kind = "video"
			}
			items = append(items, candidate{item: MediaRef{
				Root:       root.Name,
				Path:       filepath.ToSlash(rel),
				Kind:       kind,
				ModifiedAt: modified,
			}})
			return nil
		})
		if errors.Is(walkErr, context.Canceled) || ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
			scanErrors = append(scanErrors, fmt.Errorf("root %q: %w", root.Name, walkErr))
		}
		if capped {
			break
		}
	}
	if capped {
		log.Printf("mounvix feeds: scan cap reached after %d entries or %s", entryCount, time.Since(start).Round(time.Millisecond))
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].item, items[j].item
		if !left.ModifiedAt.Equal(right.ModifiedAt) {
			return left.ModifiedAt.Before(right.ModifiedAt)
		}
		if left.Root != right.Root {
			return left.Root < right.Root
		}
		return left.Path < right.Path
	})
	return items, errors.Join(scanErrors...)
}

func sample(items []candidate, maxItems int) []candidate {
	if maxItems <= 0 || len(items) <= maxItems {
		return items
	}
	if maxItems == 1 {
		return []candidate{items[len(items)-1]}
	}
	selected := make([]candidate, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		index := i * (len(items) - 1) / (maxItems - 1)
		selected = append(selected, items[index])
	}
	return selected
}

func (m *Manager) processedLocked(slot time.Time) bool {
	for _, processed := range m.state.ProcessedSlots {
		if processed.Equal(slot) {
			return true
		}
	}
	return false
}

func (m *Manager) pruneLocked(now time.Time) {
	cutoff := now.Add(-time.Duration(m.cfg.RetentionDays) * 24 * time.Hour)
	kept := m.state.Cards[:0]
	for _, card := range m.state.Cards {
		if !card.GeneratedAt.Before(cutoff) {
			kept = append(kept, card)
		}
	}
	m.state.Cards = kept
	processed := m.state.ProcessedSlots[:0]
	for _, slot := range m.state.ProcessedSlots {
		if !slot.Before(cutoff) {
			processed = append(processed, slot)
		}
	}
	m.state.ProcessedSlots = processed
}

func (m *Manager) saveLocked() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("feeds: encode archive: %w", err)
	}
	f, err := os.CreateTemp(m.dir, "feeds-*.tmp")
	if err != nil {
		return fmt.Errorf("feeds: create temporary archive: %w", err)
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("feeds: protect temporary archive: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("feeds: write temporary archive: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("feeds: sync temporary archive: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("feeds: close temporary archive: %w", err)
	}
	if err := os.Rename(tempPath, m.path); err != nil {
		return fmt.Errorf("feeds: publish archive: %w", err)
	}
	return nil
}

func cloneState(in diskState) diskState {
	out := diskState{
		Cards:          make([]storedCard, len(in.Cards)),
		ProcessedSlots: append([]time.Time(nil), in.ProcessedSlots...),
	}
	for i, card := range in.Cards {
		out.Cards[i] = storedCard{
			ID:          card.ID,
			GeneratedAt: card.GeneratedAt,
			Title:       card.Title,
			Media:       append([]MediaRef(nil), card.Media...),
			ReadBy:      make(map[string]bool, len(card.ReadBy)),
		}
		for user, read := range card.ReadBy {
			out.Cards[i].ReadBy[user] = read
		}
	}
	return out
}

func cardForUser(stored storedCard, user string) Card {
	return Card{
		ID:          stored.ID,
		GeneratedAt: stored.GeneratedAt,
		Title:       stored.Title,
		Media:       append([]MediaRef(nil), stored.Media...),
		IsRead:      stored.ReadBy[user],
	}
}

func cloneCard(card Card) Card {
	card.Media = append([]MediaRef(nil), card.Media...)
	return card
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func isoWeekday(value time.Time) int {
	day := int(value.Weekday())
	if day == 0 {
		return 7
	}
	return day
}

func inQuietHours(now time.Time, startText, endText string) (bool, error) {
	if startText == "" || endText == "" {
		return false, nil
	}
	start, err := time.Parse("15:04", startText)
	if err != nil {
		return false, fmt.Errorf("quiet hours start: %w", err)
	}
	end, err := time.Parse("15:04", endText)
	if err != nil {
		return false, fmt.Errorf("quiet hours end: %w", err)
	}
	current := now.Hour()*60 + now.Minute()
	startMinute := start.Hour()*60 + start.Minute()
	endMinute := end.Hour()*60 + end.Minute()
	if startMinute == endMinute {
		return false, nil
	}
	if startMinute < endMinute {
		return current >= startMinute && current < endMinute, nil
	}
	return current >= startMinute || current < endMinute, nil
}
