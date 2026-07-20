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

	// Pass nil channelFactory for tests (GetTTLForGroup will use default TTL)
	return NewCacheService(db, nil), db
}

func TestCacheService_PutAndGet(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cacheKey := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	err := cs.Put(cacheKey, 1, "search", `{"results":[]}`, 200, 0)
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

	err := cs.Put(cacheKey, 1, "search", `{"v":1}`, 200, 0)
	if err != nil {
		t.Fatalf("first Put failed: %v", err)
	}

	err = cs.Put(cacheKey, 1, "search", `{"v":2}`, 200, 0)
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
	cs.Put(cacheKey, 1, "search", `{"ok":true}`, 200, 0)

	e1 := cs.Get(cacheKey)
	if e1 == nil {
		t.Fatal("expected cache hit")
	}

	e2 := cs.Get(cacheKey)
	if e2 == nil {
		t.Fatal("expected cache hit")
	}
	if e2.HitCount != 2 {
		t.Errorf("expected hit_count=2 after 2 Gets, got %d", e2.HitCount)
	}
}

func TestCacheService_Cleanup_Expired(t *testing.T) {
	cs, db := newTestCacheService(t)

	// Insert an expired entry manually.
	expired := models.SearchCache{
		CacheKey:     "expired-key-00000000000000000000000000000000000000000000000",
		GroupID:      1,
		Endpoint:     "search",
		ResponseBody: `{"old":true}`,
		StatusCode:   200,
		HitCount:     5,
		ExpiresAt:    time.Now().Add(-1 * time.Hour), // 已过期
		CreatedAt:    time.Now().Add(-48 * time.Hour),
		LastAccessAt: time.Now().Add(-48 * time.Hour),
	}
	db.Create(&expired)

	// Insert a valid (not expired) entry.
	cs.Put("valid-key-000000000000000000000000000000000000000000000000000", 1, "search", `{"new":true}`, 200, 0)

	// Cleanup expired entries.
	deleted, err := cs.Cleanup()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// Expired entry should be gone.
	if cs.Get(expired.CacheKey) != nil {
		t.Error("expected expired entry to be cleaned up")
	}

	// Valid entry should still exist.
	if cs.Get("valid-key-000000000000000000000000000000000000000000000000000") == nil {
		t.Error("expected valid entry to survive cleanup")
	}
}

func TestCacheService_Stats(t *testing.T) {
	cs, _ := newTestCacheService(t)

	total, hits, err := cs.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if total != 0 || hits != 0 {
		t.Errorf("expected (0,0) for empty cache, got (%d,%d)", total, hits)
	}

	cs.Put("stats-key-1-000000000000000000000000000000000000000000000000000", 1, "search", `{"a":1}`, 200, 0)
	cs.Put("stats-key-2-000000000000000000000000000000000000000000000000000", 2, "search", `{"b":2}`, 200, 0)

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

func TestCacheService_Lifecycle(t *testing.T) {
	cs, _ := newTestCacheService(t)

	cs.Start()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cs.Stop(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CacheService.Stop() did not return within 5 seconds")
	}
}
