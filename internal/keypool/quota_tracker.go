package keypool

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// QuotaTracker 实现四层额度追踪机制：
//  1. IncrementUsed   — 实时本地递增（每次成功请求后 +1）
//  2. SyncKey         — 定期从 Tavily /usage API 同步权威用量
//  3. MonthlyReset    — 每月 1 日重置 used_quota
//  4. MarkExhausted   — 被动耗尽检测（432/433 响应时设置 used=total）
type QuotaTracker struct {
	db              *gorm.DB
	settingsManager *config.SystemSettingsManager
	encryptionSvc   encryption.Service
	httpClient      *http.Client
	stopChan        chan struct{}
	wg              sync.WaitGroup
	running         atomic.Bool
}

// Sync configuration for the worker pool used in syncAllKeys.
const (
	syncConcurrency = defaultSyncConcurrency // 4 workers by default
	syncPacing      = defaultSyncPacing      // 200ms between request starts
)

// NewQuotaTracker creates a new QuotaTracker.
func NewQuotaTracker(db *gorm.DB, settingsManager *config.SystemSettingsManager, encryptionSvc encryption.Service) *QuotaTracker {
	return &QuotaTracker{
		db:              db,
		settingsManager: settingsManager,
		encryptionSvc:   encryptionSvc,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		stopChan: make(chan struct{}),
	}
}

// --- Layer 1: Real-time local increment ---

// IncrementUsed atomically increments used_quota by 1 after a successful request.
// Uses CASE WHEN to cap at total_quota (only when total_quota > 0).
func (qt *QuotaTracker) IncrementUsed(keyID uint) {
	go func() {
		now := time.Now()
		err := qt.db.Model(&models.APIKey{}).Where("id = ?", keyID).Updates(map[string]any{
			"used_quota":   gorm.Expr("CASE WHEN total_quota > 0 AND used_quota + 1 > total_quota THEN total_quota ELSE used_quota + 1 END"),
			"last_used_at": &now,
		}).Error
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"keyID": keyID,
				"error": err,
			}).Error("QuotaTracker: Failed to increment used_quota")
		}
	}()
}

// --- Layer 4: Passive exhaustion detection ---

// MarkExhausted sets used_quota = total_quota when a 432/433 response is received.
func (qt *QuotaTracker) MarkExhausted(keyID uint) {
	go func() {
		err := qt.db.Model(&models.APIKey{}).Where("id = ?", keyID).
			Update("used_quota", gorm.Expr("total_quota")).Error
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"keyID": keyID,
				"error": err,
			}).Error("QuotaTracker: Failed to mark key as exhausted")
		}
		logrus.WithField("keyID", keyID).Warn("QuotaTracker: Key marked as exhausted (432/433)")
	}()
}

// --- Layer 2: Periodic Tavily /usage API sync ---

// tavilyUsageResponse is the response from GET /usage.
type tavilyUsageResponse struct {
	Key struct {
		Usage int  `json:"usage"`
		Limit *int `json:"limit"`
	} `json:"key"`
	Account struct {
		PlanLimit *int `json:"plan_limit"`
	} `json:"account"`
}

// SyncKey calls Tavily /usage API and syncs the authoritative usage numbers.
func (qt *QuotaTracker) SyncKey(ctx context.Context, keyID uint, keyValue string, upstreamURL string) error {
	url := upstreamURL + "/usage"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+keyValue)

	resp, err := qt.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch usage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read usage response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// 401 → invalid key, 432/433 → exhausted
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			logrus.WithFields(logrus.Fields{
				"keyID": keyID,
				"code":  resp.StatusCode,
			}).Warn("QuotaTracker: Tavily returned 401 during usage sync")
			qt.db.Model(&models.APIKey{}).Where("id = ?", keyID).
				Update("status", models.KeyStatusInvalid)
		case 432, 433:
			qt.MarkExhausted(keyID)
		}
		return fmt.Errorf("tavily usage API returned %d: %s", resp.StatusCode, string(body))
	}

	var usageResp tavilyUsageResponse
	if err := json.Unmarshal(body, &usageResp); err != nil {
		return fmt.Errorf("failed to parse usage response: %w", err)
	}

	// Determine the authoritative limit
	limit := usageResp.Key.Limit
	if limit == nil {
		limit = usageResp.Account.PlanLimit
	}

	usage := usageResp.Key.Usage
	updates := map[string]any{"used_quota": usage}
	if limit != nil && *limit > 0 {
		updates["total_quota"] = *limit
		if usage > *limit {
			updates["used_quota"] = *limit
		}
	}

	if err := qt.db.Model(&models.APIKey{}).Where("id = ?", keyID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update usage in DB: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"keyID": keyID,
		"usage": usage,
		"limit": limit,
	}).Debug("QuotaTracker: Synced key usage from Tavily")

	return nil
}

// --- Layer 3: Monthly reset ---

// MonthlyReset resets used_quota = 0 for all keys in Tavily groups.
func (qt *QuotaTracker) MonthlyReset() error {
	result := qt.db.Model(&models.APIKey{}).
		Where("group_id IN (?)", qt.db.Model(&models.Group{}).Select("id").Where("channel_type = ?", "tavily")).
		Update("used_quota", 0)
	if result.Error != nil {
		return fmt.Errorf("failed to reset monthly usage: %w", result.Error)
	}
	logrus.WithField("affected_keys", result.RowsAffected).Info("QuotaTracker: Monthly usage reset completed")
	return nil
}

// --- Background jobs ---

// Start launches the background sync and monthly reset goroutines.
func (qt *QuotaTracker) Start() {
	logrus.Info("QuotaTracker: Starting background jobs...")
	qt.wg.Add(2)
	go qt.syncLoop()
	go qt.monthlyResetLoop()
}

// Stop gracefully shuts down background goroutines.
func (qt *QuotaTracker) Stop(ctx context.Context) {
	close(qt.stopChan)

	done := make(chan struct{})
	go func() {
		qt.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("QuotaTracker stopped gracefully.")
	case <-ctx.Done():
		logrus.Warn("QuotaTracker stop timed out.")
	}
}

// syncLoop periodically syncs usage data for all Tavily group keys.
// Uses a 30-second polling ticker; the actual sync interval is 60 minutes by default.
func (qt *QuotaTracker) syncLoop() {
	defer qt.wg.Done()

	lastRun := time.Time{}
	pollInterval := 30 * time.Second
	syncInterval := 60 * time.Minute

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-qt.stopChan:
			return
		case <-ticker.C:
			if qt.running.Load() {
				continue // previous sync still running
			}
			if time.Since(lastRun) < syncInterval {
				continue
			}
			qt.running.Store(true)
			go func() {
				defer qt.running.Store(false)
				qt.syncAllKeys()
				lastRun = time.Now()
			}()
		}
	}
}

// syncAllKeys queries all active keys from Tavily groups and syncs their usage
// using a worker pool with pacing to control request rate.
func (qt *QuotaTracker) syncAllKeys() {
	var tavilyGroups []models.Group
	if err := qt.db.Where("channel_type = ?", "tavily").Find(&tavilyGroups).Error; err != nil {
		logrus.WithError(err).Error("QuotaTracker: Failed to query Tavily groups")
		return
	}

	if len(tavilyGroups) == 0 {
		return
	}

	groupIDs := make([]uint, len(tavilyGroups))
	upstreamMap := make(map[uint]string)
	for i, g := range tavilyGroups {
		groupIDs[i] = g.ID
		upstreamMap[g.ID] = qt.extractUpstreamBase(g)
	}

	var keys []models.APIKey
	if err := qt.db.Where("group_id IN ? AND status = ?", groupIDs, models.KeyStatusActive).Find(&keys).Error; err != nil {
		logrus.WithError(err).Error("QuotaTracker: Failed to query Tavily keys")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	synced, errors := WorkerPool(ctx, keys, syncConcurrency, syncPacing,
		func(ctx context.Context, key models.APIKey) error {
			upstream := upstreamMap[key.GroupID]
			if upstream == "" {
				return fmt.Errorf("no upstream URL for group %d", key.GroupID)
			}
			decryptedKey, err := qt.encryptionSvc.Decrypt(key.KeyValue)
				if err != nil {
					logrus.WithError(err).WithField("keyID", key.ID).Error("QuotaTracker: Failed to decrypt key, skipping sync")
					return fmt.Errorf("failed to decrypt key %d: %w", key.ID, err)
				}
				return qt.SyncKey(ctx, key.ID, decryptedKey, upstream)
		},
	)

	logrus.WithFields(logrus.Fields{
		"synced": synced,
		"errors": errors,
		"total":  len(keys),
	}).Info("QuotaTracker: Sync cycle completed")
}

// extractUpstreamBase extracts the base URL from a group's upstream config.
func (qt *QuotaTracker) extractUpstreamBase(group models.Group) string {
	type upstreamDef struct {
		URL string `json:"url"`
	}
	var defs []upstreamDef
	if err := json.Unmarshal(group.Upstreams, &defs); err != nil || len(defs) == 0 {
		return ""
	}
	return defs[0].URL
}

// monthlyResetLoop waits for midnight on the 1st of each month and resets usage.
func (qt *QuotaTracker) monthlyResetLoop() {
	defer qt.wg.Done()

	for {
		now := time.Now()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		timer := time.NewTimer(time.Until(nextMidnight))

		select {
		case <-qt.stopChan:
			timer.Stop()
			return
		case <-timer.C:
			if nextMidnight.Day() == 1 {
				logrus.Info("QuotaTracker: Monthly reset triggered (1st of month)")
				if err := qt.MonthlyReset(); err != nil {
					logrus.WithError(err).Error("QuotaTracker: Monthly reset failed")
				}
			}
		}
	}
}
