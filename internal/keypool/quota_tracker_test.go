package keypool

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-load/internal/models"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestQuotaTracker creates a QuotaTracker backed by in-memory SQLite with auto-migrated schemas.
func newTestQuotaTracker(t *testing.T) (*QuotaTracker, *gorm.DB) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "qt-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	db, err := gorm.Open(sqlite.Open(tmpFile.Name()), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&models.APIKey{}, &models.Group{}); err != nil {
		t.Fatalf("failed to auto-migrate: %v", err)
	}

	qt := &QuotaTracker{
		db:              db,
		settingsManager: nil, // not needed for these tests
		httpClient:      &http.Client{Timeout: 5 * time.Second},
		stopChan:        make(chan struct{}),
	}
	return qt, db
}

// waitForCondition polls until the condition is true or timeout is reached.
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v: %s", timeout, msg)
}

// ---------- TestIncrementUsed ----------

func TestIncrementUsed(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// Create a key with TotalQuota=100, UsedQuota=0.
	key := models.APIKey{KeyValue: "tvly-test-1", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 100, UsedQuota: 0}
	if err := db.Create(&key).Error; err != nil {
		t.Fatalf("failed to create key: %v", err)
	}

	qt.IncrementUsed(key.ID)
	waitForCondition(t, 2*time.Second, func() bool {
		var k models.APIKey
		db.First(&k, key.ID)
		return k.UsedQuota == 1
	}, "used_quota should be 1")

	var k models.APIKey
	db.First(&k, key.ID)
	if k.UsedQuota != 1 {
		t.Errorf("expected used_quota=1, got %d", k.UsedQuota)
	}
	if k.LastUsedAt == nil {
		t.Error("expected last_used_at to be set")
	}
}

func TestIncrementUsed_CapsAtTotalQuota(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// Key with TotalQuota=5, UsedQuota=5 (already at cap).
	key := models.APIKey{KeyValue: "tvly-test-cap", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 5, UsedQuota: 5}
	db.Create(&key)

	qt.IncrementUsed(key.ID)
	time.Sleep(100 * time.Millisecond) // wait for async goroutine

	var k models.APIKey
	db.First(&k, key.ID)
	if k.UsedQuota != 5 {
		t.Errorf("expected used_quota to stay at 5 (cap), got %d", k.UsedQuota)
	}
}

func TestIncrementUsed_UnlimitedQuota(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// TotalQuota=0 means unlimited — should not cap.
	key := models.APIKey{KeyValue: "tvly-test-unlimited", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 0, UsedQuota: 999}
	db.Create(&key)

	qt.IncrementUsed(key.ID)
	waitForCondition(t, 2*time.Second, func() bool {
		var k models.APIKey
		db.First(&k, key.ID)
		return k.UsedQuota == 1000
	}, "used_quota should be 1000 for unlimited key")
}

func TestIncrementUsed_Concurrent(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	key := models.APIKey{KeyValue: "tvly-test-concurrent", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 0, UsedQuota: 0}
	db.Create(&key)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			qt.IncrementUsed(key.ID)
		}()
	}
	wg.Wait()

	// Wait for all async goroutines to finish.
	waitForCondition(t, 5*time.Second, func() bool {
		var k models.APIKey
		db.First(&k, key.ID)
		return k.UsedQuota == n
	}, fmt.Sprintf("used_quota should be %d after concurrent increments", n))

	var k models.APIKey
	db.First(&k, key.ID)
	t.Logf("concurrent result: used_quota=%d (expected %d)", k.UsedQuota, n)
}

// ---------- TestMarkExhausted ----------

func TestMarkExhausted(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	key := models.APIKey{KeyValue: "tvly-test-exhaust", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500}
	db.Create(&key)

	qt.MarkExhausted(key.ID)
	waitForCondition(t, 2*time.Second, func() bool {
		var k models.APIKey
		db.First(&k, key.ID)
		return k.UsedQuota == 1000
	}, "used_quota should equal total_quota after MarkExhausted")

	var k models.APIKey
	db.First(&k, key.ID)
	if k.UsedQuota != k.TotalQuota {
		t.Errorf("expected used_quota=%d (total), got %d", k.TotalQuota, k.UsedQuota)
	}
}

// ---------- TestMonthlyReset ----------

func TestMonthlyReset(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// Create a Tavily group.
	tavilyGroup := models.Group{
		Name:        "tavily-test",
		ChannelType: "tavily",
		Upstreams:   []byte(`[{"url":"https://api.tavily.com"}]`),
		TestModel:   "tavily-search",
	}
	db.Create(&tavilyGroup)

	// Create a non-Tavily group.
	openaiGroup := models.Group{
		Name:        "openai-test",
		ChannelType: "openai",
		Upstreams:   []byte(`[{"url":"https://api.openai.com"}]`),
		TestModel:   "gpt-4",
	}
	db.Create(&openaiGroup)

	// Add keys to both groups.
	tavilyKey := models.APIKey{KeyValue: "tvly-key-1", GroupID: tavilyGroup.ID, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 800}
	openaiKey := models.APIKey{KeyValue: "sk-key-1", GroupID: openaiGroup.ID, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 600}
	db.Create(&tavilyKey)
	db.Create(&openaiKey)

	// Run monthly reset.
	if err := qt.MonthlyReset(); err != nil {
		t.Fatalf("MonthlyReset failed: %v", err)
	}

	// Tavily key should be reset to 0.
	var tk models.APIKey
	db.First(&tk, tavilyKey.ID)
	if tk.UsedQuota != 0 {
		t.Errorf("Tavily key: expected used_quota=0 after reset, got %d", tk.UsedQuota)
	}

	// Non-Tavily key should be unchanged.
	var ok models.APIKey
	db.First(&ok, openaiKey.ID)
	if ok.UsedQuota != 600 {
		t.Errorf("non-Tavily key: expected used_quota=600 (unchanged), got %d", ok.UsedQuota)
	}
}

// ---------- TestSyncKey_MockServer ----------

func TestSyncKey_MockServer(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// Create a mock Tavily /usage server.
	limit := 1000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" {
			t.Errorf("expected /usage path, got %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer tvly-test-key-sync" {
			t.Errorf("unexpected Authorization header: %s", auth)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := tavilyUsageResponse{}
		resp.Key.Usage = 42
		resp.Key.Limit = &limit
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	key := models.APIKey{KeyValue: "tvly-test-key-sync", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 500, UsedQuota: 30}
	db.Create(&key)

	err := qt.SyncKey(context.Background(), key.ID, "tvly-test-key-sync", server.URL)
	if err != nil {
		t.Fatalf("SyncKey failed: %v", err)
	}

	var k models.APIKey
	db.First(&k, key.ID)
	if k.UsedQuota != 42 {
		t.Errorf("expected used_quota=42 (from mock), got %d", k.UsedQuota)
	}
	if k.TotalQuota != 1000 {
		t.Errorf("expected total_quota=1000 (from mock limit), got %d", k.TotalQuota)
	}
}

func TestSyncKey_401MarksInvalid(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer server.Close()

	key := models.APIKey{KeyValue: "tvly-bad-key", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 0}
	db.Create(&key)

	err := qt.SyncKey(context.Background(), key.ID, "tvly-bad-key", server.URL)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}

	// Key should be marked invalid.
	waitForCondition(t, 2*time.Second, func() bool {
		var k models.APIKey
		db.First(&k, key.ID)
		return k.Status == models.KeyStatusInvalid
	}, "key should be marked invalid after 401")
}

func TestSyncKey_432MarksExhausted(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(432)
		w.Write([]byte(`{"error":"quota exhausted"}`))
	}))
	defer server.Close()

	key := models.APIKey{KeyValue: "tvly-exhausted-key", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500}
	db.Create(&key)

	err := qt.SyncKey(context.Background(), key.ID, "tvly-exhausted-key", server.URL)
	if err == nil {
		t.Fatal("expected error for 432 response")
	}

	// Key should have used_quota set to total_quota.
	waitForCondition(t, 2*time.Second, func() bool {
		var k models.APIKey
		db.First(&k, key.ID)
		return k.UsedQuota == k.TotalQuota
	}, "key should be marked exhausted (used=total) after 432")
}

func TestSyncKey_AccountPlanLimit(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// Server returns usage with key.limit=nil, should fall back to account.plan_limit.
	planLimit := 5000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"key":     map[string]any{"usage": 100, "limit": nil},
			"account": map[string]any{"plan_limit": planLimit},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	key := models.APIKey{KeyValue: "tvly-plan-key", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 0, UsedQuota: 0}
	db.Create(&key)

	err := qt.SyncKey(context.Background(), key.ID, "tvly-plan-key", server.URL)
	if err != nil {
		t.Fatalf("SyncKey failed: %v", err)
	}

	var k models.APIKey
	db.First(&k, key.ID)
	if k.TotalQuota != 5000 {
		t.Errorf("expected total_quota=5000 (from account plan_limit), got %d", k.TotalQuota)
	}
	if k.UsedQuota != 100 {
		t.Errorf("expected used_quota=100, got %d", k.UsedQuota)
	}
}

// ---------- TestQuotaTracker_Lifecycle ----------

func TestQuotaTracker_Lifecycle(t *testing.T) {
	qt, _ := newTestQuotaTracker(t)

	qt.Start()

	// Give it a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Stop with a generous timeout — should not hang.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		qt.Stop(ctx)
		close(done)
	}()

	select {
	case <-done:
		// OK — graceful shutdown.
	case <-time.After(5 * time.Second):
		t.Fatal("QuotaTracker.Stop() did not return within 5 seconds")
	}
}

// ---------- TestWorkerPool_IntegrationWithSyncAllKeys ----------

func TestWorkerPool_IntegrationWithSyncAllKeys(t *testing.T) {
	qt, db := newTestQuotaTracker(t)

	// Create a mock server that tracks request count.
	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		limit := 1000
		resp := tavilyUsageResponse{}
		resp.Key.Usage = requestCount * 10
		resp.Key.Limit = &limit
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create a Tavily group pointing to the mock server.
	group := models.Group{
		Name:        "tavily-integration",
		ChannelType: "tavily",
		Upstreams:   []byte(fmt.Sprintf(`[{"url":"%s"}]`, server.URL)),
		TestModel:   "tavily-search",
	}
	db.Create(&group)

	// Create 8 keys in this group.
	for i := 0; i < 8; i++ {
		key := models.APIKey{
			KeyValue:   fmt.Sprintf("tvly-integration-key-%d", i),
			GroupID:    group.ID,
			Status:     models.KeyStatusActive,
			TotalQuota: 1000,
			UsedQuota:  0,
		}
		db.Create(&key)
	}

	// Run syncAllKeys.
	qt.syncAllKeys()

	// Verify all 8 keys were synced.
	mu.Lock()
	count := requestCount
	mu.Unlock()
	if count != 8 {
		t.Errorf("expected 8 /usage requests, got %d", count)
	}

	// Verify keys were updated in DB.
	var keys []models.APIKey
	db.Where("group_id = ?", group.ID).Find(&keys)
	for _, k := range keys {
		if k.UsedQuota == 0 {
			t.Errorf("key %d: expected used_quota > 0 after sync, got %d", k.ID, k.UsedQuota)
		}
	}

	t.Logf("integration sync: %d requests processed, %d keys updated", count, len(keys))
}
