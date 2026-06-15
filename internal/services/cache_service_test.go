package services

import (
	"context"
	"gpt-load/internal/models"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestCacheService(t *testing.T) (*CacheService, *gorm.DB) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "cache-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	db, err := gorm.Open(sqlite.Open(tmpFile.Name()), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.SearchCache{}); err != nil {
		t.Fatalf("failed to auto-migrate: %v", err)
	}

	return NewCacheService(db), db
}

func TestCacheService_PutAndGet(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cacheKey := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	err := cs.Put(cacheKey, 1, "search", `{"results":[]}`, 200)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	entry := cs.Get(cacheKey)
	if entry == nil {
		t.Fatal("expected cache hit, got nil")
	}
	if entry.ResponseBody != `{"results":[]}` {
		t.Errorf("expected response body '{\"results\":[]}', got '%s'", entry.ResponseBody)
	}
	if entry.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", entry.StatusCode)
	}
	if entry.GroupID != 1 {
		t.Errorf("expected group_id 1, got %d", entry.GroupID)
	}
	if entry.Endpoint != "search" {
		t.Errorf("expected endpoint 'search', got '%s'", entry.Endpoint)
	}
}

func TestCacheService_GetMiss(t *testing.T) {
	cs, _ := newTestCacheService(t)

	entry := cs.Get("nonexistent-key")
	if entry != nil {
		t.Error("expected nil for cache miss, got non-nil")
	}
}

func TestCacheService_PutUpsert(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cacheKey := "upsert-test-key-00000000000000000000000000000000000000000000000000"

	// First put.
	err := cs.Put(cacheKey, 1, "search", `{"v":1}`, 200)
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	// Second put with updated response.
	err = cs.Put(cacheKey, 1, "search", `{"v":2}`, 200)
	if err != nil {
		t.Fatalf("second Put failed: %v", err)
	}

	entry := cs.Get(cacheKey)
	if entry == nil {
		t.Fatal("expected cache hit after upsert")
	}
	if entry.ResponseBody != `{"v":2}` {
		t.Errorf("expected updated response '{\"v\":2}', got '%s'", entry.ResponseBody)
	}
}

func TestCacheService_HitCountIncrement(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cacheKey := "hitcount-key-00000000000000000000000000000000000000000000000000000"
	cs.Put(cacheKey, 1, "search", `{"ok":true}`, 200)

	// First Get — hit count goes from 0 to 1.
	e1 := cs.Get(cacheKey)
	if e1 == nil {
		t.Fatal("expected cache hit")
	}

	// Second Get — hit count goes to 2.
	e2 := cs.Get(cacheKey)
	if e2 == nil {
		t.Fatal("expected cache hit")
	}
	if e2.HitCount != 2 {
		t.Errorf("expected hit_count=2 after 2 Gets, got %d", e2.HitCount)
	}
}

func TestCacheService_Cleanup(t *testing.T) {
	cs, db := newTestCacheService(t)

	// Insert an old entry manually.
	old := models.SearchCache{
		CacheKey:     "old-key-000000000000000000000000000000000000000000000000000",
		GroupID:      1,
		Endpoint:     "search",
		ResponseBody: `{"old":true}`,
		StatusCode:   200,
		HitCount:     5,
		CreatedAt:    time.Now().Add(-48 * time.Hour),
		LastAccessAt: time.Now().Add(-48 * time.Hour),
	}
	db.Create(&old)

	// Insert a recent entry.
	cs.Put("recent-key-00000000000000000000000000000000000000000000000000", 1, "search", `{"new":true}`, 200)

	// Cleanup entries older than 24 hours.
	deleted, err := cs.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Old entry should be gone.
	if cs.Get(old.CacheKey) != nil {
		t.Error("expected old entry to be cleaned up")
	}

	// Recent entry should still exist.
	if cs.Get("recent-key-00000000000000000000000000000000000000000000000000") == nil {
		t.Error("expected recent entry to survive cleanup")
	}
}

func TestCacheService_Stats(t *testing.T) {
	cs, _ := newTestCacheService(t)

	// Empty cache.
	total, hits, err := cs.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if total != 0 || hits != 0 {
		t.Errorf("expected (0,0) for empty cache, got (%d,%d)", total, hits)
	}

	// Add entries.
	cs.Put("stats-key-1-000000000000000000000000000000000000000000000000000", 1, "search", `{"a":1}`, 200)
	cs.Put("stats-key-2-000000000000000000000000000000000000000000000000000", 2, "search", `{"b":2}`, 200)

	// Generate some hits.
	cs.Get("stats-key-1-000000000000000000000000000000000000000000000000000")
	cs.Get("stats-key-1-000000000000000000000000000000000000000000000000000")
	cs.Get("stats-key-2-000000000000000000000000000000000000000000000000000")

	total, hits, err = cs.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 entries, got %d", total)
	}
	if hits != 3 {
		t.Errorf("expected 3 total hits, got %d", hits)
	}
}

func TestCacheService_StatsByGroup(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cs.Put("grp1-key-0000000000000000000000000000000000000000000000000000", 1, "search", `{"a":1}`, 200)
	cs.Put("grp2-key-0000000000000000000000000000000000000000000000000000", 2, "search", `{"b":2}`, 200)

	cs.Get("grp1-key-0000000000000000000000000000000000000000000000000000")
	cs.Get("grp1-key-0000000000000000000000000000000000000000000000000000")

	total, hits, err := cs.StatsByGroup(1)
	if err != nil {
		t.Fatalf("StatsByGroup failed: %v", err)
	}
	if total != 1 {
		t.Errorf("group 1: expected 1 entry, got %d", total)
	}
	if hits != 2 {
		t.Errorf("group 1: expected 2 hits, got %d", hits)
	}

	total, hits, err = cs.StatsByGroup(2)
	if err != nil {
		t.Fatalf("StatsByGroup failed: %v", err)
	}
	if total != 1 {
		t.Errorf("group 2: expected 1 entry, got %d", total)
	}
	if hits != 0 {
		t.Errorf("group 2: expected 0 hits, got %d", hits)
	}
}

func TestCacheService_Lifecycle(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cs.Start()
	time.Sleep(100 * time.Millisecond) // let the initial cleanup run

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cs.Stop(ctx)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("CacheService.Stop() did not return within 5 seconds")
	}
}
