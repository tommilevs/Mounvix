package feeds

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func testManager(t *testing.T, cfg Config) (*Manager, string, Root) {
	t.Helper()
	rootPath := t.TempDir()
	dataDir := t.TempDir()
	cfg.Enabled = true
	cfg.RootNames = []string{"photos"}
	manager, err := New(dataDir, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return manager, dataDir, Root{Name: "photos", Path: rootPath}
}

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.MaxItems = 8
	return cfg
}

func writeMedia(t *testing.T, root, name string, modified time.Time) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("test media"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("chtime %s: %v", name, err)
	}
}

func TestGenerateSamplesSupportedMedia(t *testing.T) {
	cfg := testConfig()
	cfg.PeriodDays = 7
	cfg.MaxItems = 3
	manager, _, root := testManager(t, cfg)
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		writeMedia(t, root.Path, fmt.Sprintf("%02d.jpg", i), slot.Add(time.Duration(i-5)*24*time.Hour))
	}
	writeMedia(t, root.Path, "05.mp4", slot.Add(-24*time.Hour))
	writeMedia(t, root.Path, "ignored.txt", slot.Add(-time.Hour))

	card, created, err := manager.Generate(context.Background(), []Root{root}, slot)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !created {
		t.Fatal("Generate created=false, want a card")
	}
	got := make([]string, 0, len(card.Media))
	for _, item := range card.Media {
		got = append(got, item.Path)
		if item.Kind != "image" && item.Kind != "video" {
			t.Errorf("kind for %q = %q", item.Path, item.Kind)
		}
	}
	want := []string{"00.jpg", "02.jpg", "05.mp4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sampled paths = %v, want %v", got, want)
	}
}

func TestGenerateRespectsRootAndScanLimits(t *testing.T) {
	cfg := testConfig()
	cfg.MaxItems = 10
	manager, _, selected := testManager(t, cfg)
	manager.scanMaxEntries = 3
	outside := Root{Name: "outside", Path: t.TempDir()}
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, selected.Path, "a.jpg", slot.Add(-3*time.Hour))
	writeMedia(t, selected.Path, "b.jpg", slot.Add(-2*time.Hour))
	writeMedia(t, selected.Path, "c.jpg", slot.Add(-time.Hour))
	writeMedia(t, outside.Path, "secret.jpg", slot.Add(-time.Hour))
	if err := os.Symlink(filepath.Join(outside.Path, "secret.jpg"), filepath.Join(selected.Path, "z-link.jpg")); err != nil {
		t.Fatalf("create symlink fixture: %v", err)
	}

	card, created, err := manager.Generate(context.Background(), []Root{selected, outside}, slot)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !created {
		t.Fatal("Generate created=false, want a card")
	}
	if len(card.Media) != 3 {
		t.Fatalf("media count = %d, want scan cap 3", len(card.Media))
	}
	for _, item := range card.Media {
		if item.Root != selected.Name || item.Path == "secret.jpg" || item.Path == "z-link.jpg" {
			t.Errorf("media escaped selected root or followed symlink: %+v", item)
		}
	}
}

func TestGenerateSameSlotIsIdempotent(t *testing.T) {
	manager, _, root := testManager(t, testConfig())
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, root.Path, "a.jpg", slot.Add(-time.Hour))

	_, first, err := manager.Generate(context.Background(), []Root{root}, slot)
	if err != nil || !first {
		t.Fatalf("first Generate = created %v, err %v; want created", first, err)
	}
	_, second, err := manager.Generate(context.Background(), []Root{root}, slot)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if second {
		t.Fatal("second Generate created a duplicate card")
	}
}

func TestGenerateConcurrentSameSlot(t *testing.T) {
	manager, _, root := testManager(t, testConfig())
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, root.Path, "a.jpg", slot.Add(-time.Hour))

	var wg sync.WaitGroup
	var mu sync.Mutex
	createdCount := 0
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, created, err := manager.Generate(context.Background(), []Root{root}, slot)
			if err != nil {
				errCh <- err
				return
			}
			if created {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Generate: %v", err)
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
}

func TestLoadPersistsCards(t *testing.T) {
	cfg := testConfig()
	manager, dataDir, root := testManager(t, cfg)
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, root.Path, "a.jpg", slot.Add(-time.Hour))
	card, created, err := manager.Generate(context.Background(), []Root{root}, slot)
	if err != nil || !created {
		t.Fatalf("Generate = created %v, err %v; want created", created, err)
	}

	reloaded, err := New(dataDir, cfg)
	if err != nil {
		t.Fatalf("New after save: %v", err)
	}
	got, ok := reloaded.Get("alice", card.ID)
	if !ok || got.ID != card.ID || len(got.Media) != 1 {
		t.Fatalf("reloaded card = %+v, exists=%v", got, ok)
	}
}

func TestMarkReadIsUserScoped(t *testing.T) {
	manager, _, root := testManager(t, testConfig())
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, root.Path, "a.jpg", slot.Add(-time.Hour))
	card, _, err := manager.Generate(context.Background(), []Root{root}, slot)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if ok, err := manager.MarkRead("alice", card.ID); err != nil || !ok {
		t.Fatalf("MarkRead = %v, err %v; want success", ok, err)
	}
	if ok, err := manager.MarkRead("alice", card.ID); err != nil || !ok {
		t.Fatalf("idempotent MarkRead = %v, err %v; want success", ok, err)
	}
	alice, aliceExists := manager.Get("alice", card.ID)
	bob, bobExists := manager.Get("bob", card.ID)
	if !aliceExists || !alice.IsRead {
		t.Fatalf("alice card read state = %+v, exists=%v; want read", alice, aliceExists)
	}
	if !bobExists || bob.IsRead {
		t.Fatalf("bob card read state = %+v, exists=%v; want unread", bob, bobExists)
	}
}

func TestListIncludesUserReadState(t *testing.T) {
	manager, _, root := testManager(t, testConfig())
	firstSlot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, root.Path, "a.jpg", firstSlot.Add(-time.Hour))
	first, _, err := manager.Generate(context.Background(), []Root{root}, firstSlot)
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	secondSlot := firstSlot.Add(24 * time.Hour)
	writeMedia(t, root.Path, "a.jpg", secondSlot.Add(-time.Hour))
	_, _, err = manager.Generate(context.Background(), []Root{root}, secondSlot)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if ok, err := manager.MarkRead("alice", first.ID); err != nil || !ok {
		t.Fatalf("MarkRead = %v, err %v; want success", ok, err)
	}
	cards, cursor := manager.List("alice", time.Time{}, 10)
	if len(cards) != 2 || !cards[0].IsRead || cards[1].IsRead {
		t.Fatalf("cards = %+v; want first read and second unread", cards)
	}
	if !cursor.Equal(secondSlot) {
		t.Fatalf("cursor = %s, want %s", cursor, secondSlot)
	}
}

func TestListRemainsAvailableDuringScan(t *testing.T) {
	manager, _, root := testManager(t, testConfig())
	slot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	started := make(chan struct{})
	release := make(chan struct{})
	manager.scan = func(context.Context, []Root, time.Time, Config, int, time.Duration) ([]candidate, error) {
		close(started)
		<-release
		return []candidate{{item: MediaRef{Root: root.Name, Path: "a.jpg", Kind: "image", ModifiedAt: slot.Add(-time.Hour)}}}, nil
	}

	generateDone := make(chan error, 1)
	go func() {
		_, _, err := manager.Generate(context.Background(), []Root{root}, slot)
		generateDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("scan did not start")
	}
	listDone := make(chan struct{})
	go func() {
		manager.List("alice", time.Time{}, 10)
		close(listDone)
	}()
	select {
	case <-listDone:
	case <-time.After(time.Second):
		close(release)
		<-generateDone
		t.Fatal("List blocked while background scan was running")
	}
	close(release)
	if err := <-generateDone; err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

func TestRetentionPrunesExpiredCards(t *testing.T) {
	cfg := testConfig()
	cfg.RetentionDays = 1
	manager, _, root := testManager(t, cfg)
	firstSlot := time.Date(2025, time.January, 8, 12, 0, 0, 0, time.UTC)
	writeMedia(t, root.Path, "a.jpg", firstSlot.Add(-time.Hour))
	if _, created, err := manager.Generate(context.Background(), []Root{root}, firstSlot); err != nil || !created {
		t.Fatalf("first Generate = created %v, err %v; want created", created, err)
	}
	secondSlot := firstSlot.Add(48 * time.Hour)
	writeMedia(t, root.Path, "a.jpg", secondSlot.Add(-time.Hour))
	if _, created, err := manager.Generate(context.Background(), []Root{root}, secondSlot); err != nil || !created {
		t.Fatalf("second Generate = created %v, err %v; want created", created, err)
	}
	cards, _ := manager.List("alice", time.Time{}, 10)
	if len(cards) != 1 || !cards[0].GeneratedAt.Equal(secondSlot) {
		t.Fatalf("retained cards = %+v; want only slot %s", cards, secondSlot)
	}
}
