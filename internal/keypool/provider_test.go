package keypool

import (
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestProvider creates a KeyProvider backed by an in-memory SQLite DB and no-op encryption.
func newTestProvider(t *testing.T) (*KeyProvider, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("failed to auto-migrate: %v", err)
	}

	encSvc, _ := encryption.NewService("") // no-op encryption
	memStore := store.NewMemoryStore()

	return NewProvider(db, memStore, nil, encSvc), db
}

// seedKeys inserts test keys into the DB.
func seedKeys(t *testing.T, db *gorm.DB, keys []models.APIKey) {
	t.Helper()
	if err := db.Create(&keys).Error; err != nil {
		t.Fatalf("failed to seed keys: %v", err)
	}
}

// ---------- TestQuotaPriority ----------

func TestQuotaPriority(t *testing.T) {
	unlimited := quotaPriority(0, 0)        // 2_000_000_000
	highQuota := quotaPriority(1000, 500)   // 1_000_000_500
	lowQuota := quotaPriority(1000, 900)    // 1_000_000_100
	exhausted := quotaPriority(1000, 1000)  // 0
	overused := quotaPriority(1000, 1200)   // -200

	t.Logf("priorities: unlimited=%d, highQuota=%d, lowQuota=%d, exhausted=%d, overused=%d",
		unlimited, highQuota, lowQuota, exhausted, overused)

	if unlimited <= highQuota {
		t.Errorf("unlimited (%d) should be > highQuota (%d)", unlimited, highQuota)
	}
	if highQuota <= lowQuota {
		t.Errorf("highQuota (%d) should be > lowQuota (%d)", highQuota, lowQuota)
	}
	if lowQuota <= exhausted {
		t.Errorf("lowQuota (%d) should be > exhausted (%d)", lowQuota, exhausted)
	}
	if exhausted <= overused {
		t.Errorf("exhausted (%d) should be > overused (%d)", exhausted, overused)
	}
}

// ---------- TestQuotaFirstSelect_NoKeys ----------

func TestQuotaFirstSelect_NoKeys(t *testing.T) {
	p, _ := newTestProvider(t)

	_, err := p.quotaFirstSelect(999)
	if err == nil {
		t.Fatal("expected error for non-existent group, got nil")
	}
}

// ---------- TestQuotaFirstSelect_SingleKey ----------

func TestQuotaFirstSelect_SingleKey(t *testing.T) {
	p, db := newTestProvider(t)

	seedKeys(t, db, []models.APIKey{
		{KeyValue: "tvly-test-key-aaa", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 100},
	})

	key, err := p.quotaFirstSelect(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.KeyValue != "tvly-test-key-aaa" {
		t.Errorf("expected key value 'tvly-test-key-aaa', got '%s'", key.KeyValue)
	}
	if key.TotalQuota != 1000 || key.UsedQuota != 100 {
		t.Errorf("quota fields not populated: TotalQuota=%d, UsedQuota=%d", key.TotalQuota, key.UsedQuota)
	}
}

// ---------- TestQuotaFirstSelect_PriorityOrdering ----------

func TestQuotaFirstSelect_PriorityOrdering(t *testing.T) {
	p, db := newTestProvider(t)

	// Seed 3 keys with different quotas. Key B has the most remaining quota.
	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-a-low-remaining", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 900},  // remaining=100
		{KeyValue: "key-b-high-remaining", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 200}, // remaining=800
		{KeyValue: "key-c-mid-remaining", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500},  // remaining=500
	})

	key, err := p.quotaFirstSelect(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.KeyValue != "key-b-high-remaining" {
		t.Errorf("expected key with highest remaining quota, got '%s'", key.KeyValue)
	}
}

// ---------- TestQuotaFirstSelect_UnlimitedPreferred ----------

func TestQuotaFirstSelect_UnlimitedPreferred(t *testing.T) {
	p, db := newTestProvider(t)

	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-high-quota", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 10000, UsedQuota: 0}, // remaining=10000
		{KeyValue: "key-unlimited", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 0, UsedQuota: 0},     // unlimited
		{KeyValue: "key-low-quota", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 100, UsedQuota: 10},   // remaining=90
	})

	key, err := p.quotaFirstSelect(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.KeyValue != "key-unlimited" {
		t.Errorf("expected unlimited key to be selected, got '%s'", key.KeyValue)
	}
}

// ---------- TestQuotaFirstSelect_ExhaustedLast ----------

func TestQuotaFirstSelect_ExhaustedLast(t *testing.T) {
	p, db := newTestProvider(t)

	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-exhausted", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 1000}, // exhausted
		{KeyValue: "key-has-quota", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 999},  // remaining=1
	})

	key, err := p.quotaFirstSelect(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.KeyValue != "key-has-quota" {
		t.Errorf("expected key with remaining quota to be preferred over exhausted, got '%s'", key.KeyValue)
	}
}

// ---------- TestQuotaFirstSelect_ExhaustedFallback ----------

func TestQuotaFirstSelect_ExhaustedFallback(t *testing.T) {
	p, db := newTestProvider(t)

	// All keys exhausted — should still return one (graceful degradation).
	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-exhausted-a", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 100, UsedQuota: 100},
		{KeyValue: "key-exhausted-b", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 200, UsedQuota: 200},
	})

	key, err := p.quotaFirstSelect(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected a key even when all exhausted, got nil")
	}
}

// ---------- TestQuotaFirstSelect_RandomDistribution ----------

func TestQuotaFirstSelect_RandomDistribution(t *testing.T) {
	p, db := newTestProvider(t)

	// 3 keys with identical quotas — should be randomly distributed.
	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-x", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500},
		{KeyValue: "key-y", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500},
		{KeyValue: "key-z", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500},
	})

	counts := make(map[string]int)
	const iterations = 300

	for i := 0; i < iterations; i++ {
		key, err := p.quotaFirstSelect(1)
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
		counts[key.KeyValue]++
	}

	// Each key should be selected at least 10% of the time (expected ~33%).
	minCount := iterations / 10
	for _, kv := range []string{"key-x", "key-y", "key-z"} {
		if counts[kv] < minCount {
			t.Errorf("key '%s' selected only %d/%d times (minimum expected %d)", kv, counts[kv], iterations, minCount)
		}
	}
	t.Logf("distribution: key-x=%d, key-y=%d, key-z=%d", counts["key-x"], counts["key-y"], counts["key-z"])
}

// ---------- TestQuotaFirstSelect_SkipsInvalidKeys ----------

func TestQuotaFirstSelect_SkipsInvalidKeys(t *testing.T) {
	p, db := newTestProvider(t)

	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-invalid-best-quota", GroupID: 1, Status: models.KeyStatusInvalid, TotalQuota: 10000, UsedQuota: 0},
		{KeyValue: "key-active-worse-quota", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 100, UsedQuota: 50},
	})

	key, err := p.quotaFirstSelect(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.KeyValue != "key-active-worse-quota" {
		t.Errorf("expected active key to be selected over invalid key, got '%s'", key.KeyValue)
	}
}

// ---------- TestSelectKey_StrategyDispatch ----------

func TestSelectKey_StrategyDispatch(t *testing.T) {
	p, db := newTestProvider(t)

	seedKeys(t, db, []models.APIKey{
		{KeyValue: "key-unlimited", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 0, UsedQuota: 0},
		{KeyValue: "key-limited", GroupID: 1, Status: models.KeyStatusActive, TotalQuota: 1000, UsedQuota: 500},
	})

	// quota_first should select the unlimited key (highest priority).
	key, err := p.SelectKey(1, models.KeySelectionQuotaFirst)
	if err != nil {
		t.Fatalf("quota_first strategy failed: %v", err)
	}
	if key.KeyValue != "key-unlimited" {
		t.Errorf("quota_first: expected 'key-unlimited', got '%s'", key.KeyValue)
	}

	// round_robin requires store setup; just verify it doesn't panic with empty store.
	_, err = p.SelectKey(1, models.KeySelectionRoundRobin)
	if err == nil {
		t.Error("round_robin: expected error with empty store, got nil")
	}

	// Unknown strategy should fall back to round_robin (same error).
	_, err = p.SelectKey(1, "unknown_strategy")
	if err == nil {
		t.Error("unknown strategy: expected error with empty store, got nil")
	}
}

// ---------- TestDecryptKey ----------

func TestDecryptKey_NoOp(t *testing.T) {
	p, _ := newTestProvider(t)
	result := p.decryptKey(1, "tvly-test-key-12345")
	if result != "tvly-test-key-12345" {
		t.Errorf("expected 'tvly-test-key-12345', got '%s'", result)
	}
}

func TestDecryptKey_WithEncryption(t *testing.T) {
	encSvc, err := encryption.NewService("test-encryption-key-32bytes-long!!")
	if err != nil {
		t.Fatalf("failed to create encryption service: %v", err)
	}

	db, err2 := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err2 != nil {
		t.Fatalf("failed to open sqlite: %v", err2)
	}

	p := NewProvider(db, nil, nil, encSvc)

	encrypted, err := encSvc.Encrypt("tvly-secret-key-abc")
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	result := p.decryptKey(1, encrypted)
	if result != "tvly-secret-key-abc" {
		t.Errorf("expected 'tvly-secret-key-abc', got '%s'", result)
	}
}

func TestDecryptKey_FallbackOnFailure(t *testing.T) {
	encSvc, _ := encryption.NewService("test-encryption-key-32bytes-long!!")
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	p := NewProvider(db, nil, nil, encSvc)

	// Garbage data that will fail decryption — should fall back to raw value.
	result := p.decryptKey(1, "not-valid-hex-data")
	if result != "not-valid-hex-data" {
		t.Errorf("expected fallback to raw value 'not-valid-hex-data', got '%s'", result)
	}
}
