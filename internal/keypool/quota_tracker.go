package keypool

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-load/internal/channel"
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

// QuotaTracker 实现额度追踪机制：
//  1. IncrementUsed   — 实时本地递增（每次成功请求后 +1）
//  2. SyncKey         — 定期从 /usage API 同步权威用量（仅 SyncAvailable 的 channel）
//  3. PeriodicReset   — 按周期重置 used_quota（日度/月度）
//  4. MarkExhausted   — 被动耗尽检测（status_code 或 response_body 判断）
type QuotaTracker struct {
	db              *gorm.DB
	channelFactory  *channel.Factory
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
func NewQuotaTracker(db *gorm.DB, channelFactory *channel.Factory, settingsManager *config.SystemSettingsManager, encryptionSvc encryption.Service) *QuotaTracker {
	return &QuotaTracker{
		db:              db,
		channelFactory:  channelFactory,
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

// MarkExhausted sets used_quota = total_quota when exhaustion is detected.
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
		logrus.WithField("keyID", keyID).Warn("QuotaTracker: Key marked as exhausted")
	}()
}

// --- Layer 2: Periodic /usage API sync (only for channels with SyncAvailable) ---

// tavilyUsageResponse is the response from Tavily GET /usage.
type tavilyUsageResponse struct {
	Key struct {
		Usage int  `json:"usage"`
		Limit *int `json:"limit"`
	} `json:"key"`
	Account struct {
		PlanLimit *int `json:"plan_limit"`
	} `json:"account"`
}

// SyncKey calls the upstream /usage API and syncs the authoritative usage numbers.
// Currently only Tavily provides a /usage endpoint.
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
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			logrus.WithFields(logrus.Fields{
				"keyID": keyID,
				"code":  resp.StatusCode,
			}).Warn("QuotaTracker: Upstream returned 401 during usage sync")
			qt.db.Model(&models.APIKey{}).Where("id = ?", keyID).
				Update("status", models.KeyStatusInvalid)
		case 432, 433:
			qt.MarkExhausted(keyID)
		}
		return fmt.Errorf("usage API returned %d: %s", resp.StatusCode, string(body))
	}

	var usageResp tavilyUsageResponse
	if err := json.Unmarshal(body, &usageResp); err != nil {
		return fmt.Errorf("failed to parse usage response: %w", err)
	}

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
	}).Debug("QuotaTracker: Synced key usage from upstream")

	return nil
}

// --- Layer 3: Periodic reset (daily + monthly) ---

// DailyReset resets used_quota = 0 for all keys in groups with daily quota cycle.
func (qt *QuotaTracker) DailyReset() error {
	groupIDs, err := qt.getGroupIDsByQuotaCycle(channel.QuotaCycleDaily)
	if err != nil {
		return fmt.Errorf("failed to query daily quota groups: %w", err)
	}
	if len(groupIDs) == 0 {
		return nil
	}

	result := qt.db.Model(&models.APIKey{}).
		Where("group_id IN ?", groupIDs).
		Update("used_quota", 0)
	if result.Error != nil {
		return fmt.Errorf("failed to reset daily usage: %w", result.Error)
	}
	logrus.WithField("affected_keys", result.RowsAffected).Info("QuotaTracker: Daily usage reset completed")
	return nil
}

// MonthlyReset resets used_quota = 0 for all keys in groups with monthly quota cycle.
func (qt *QuotaTracker) MonthlyReset() error {
	groupIDs, err := qt.getGroupIDsByQuotaCycle(channel.QuotaCycleMonthly)
	if err != nil {
		return fmt.Errorf("failed to query monthly quota groups: %w", err)
	}
	if len(groupIDs) == 0 {
		return nil
	}

	result := qt.db.Model(&models.APIKey{}).
		Where("group_id IN ?", groupIDs).
		Update("used_quota", 0)
	if result.Error != nil {
		return fmt.Errorf("failed to reset monthly usage: %w", result.Error)
	}
	logrus.WithField("affected_keys", result.RowsAffected).Info("QuotaTracker: Monthly usage reset completed")
	return nil
}

// getGroupIDsByQuotaCycle returns group IDs whose channel type has the given quota cycle.
func (qt *QuotaTracker) getGroupIDsByQuotaCycle(cycle channel.QuotaCycle) ([]uint, error) {
	// Scan all non-aggregate groups and filter by their channel's quota config
	var groups []models.Group
	if err := qt.db.Where("group_type != ? OR group_type IS NULL", "aggregate").Find(&groups).Error; err != nil {
		return nil, err
	}

	var result []uint
	for _, g := range groups {
		ch, err := qt.channelFactory.GetChannel(&g)
		if err != nil {
			continue
		}
		if channel.GetQuotaConfig(ch).Cycle == cycle {
			result = append(result, g.ID)
		}
	}
	return result, nil
}

// --- Background jobs ---

// Start launches the background sync and periodic reset goroutines.
func (qt *QuotaTracker) Start() {
	logrus.Info("QuotaTracker: Starting background jobs...")
	qt.wg.Add(3)
	go qt.syncLoop()
	go qt.dailyResetLoop()
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

// syncLoop periodically syncs usage data for all groups with SyncAvailable channels.
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
				continue
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

// syncAllKeys queries all active keys from groups with SyncAvailable channels and syncs their usage.
func (qt *QuotaTracker) syncAllKeys() {
	// Find groups whose channel supports usage sync
	var allGroups []models.Group
	if err := qt.db.Where("group_type != ? OR group_type IS NULL", "aggregate").Find(&allGroups).Error; err != nil {
		logrus.WithError(err).Error("QuotaTracker: Failed to query groups")
		return
	}

	var syncGroups []models.Group
	for _, g := range allGroups {
		ch, err := qt.channelFactory.GetChannel(&g)
		if err != nil {
			continue
		}
		if channel.GetQuotaConfig(ch).SyncAvailable {
			syncGroups = append(syncGroups, g)
		}
	}

	if len(syncGroups) == 0 {
		return
	}

	groupIDs := make([]uint, len(syncGroups))
	upstreamMap := make(map[uint]string)
	for i, g := range syncGroups {
		groupIDs[i] = g.ID
		upstreamMap[g.ID] = qt.extractUpstreamBase(g)
	}

	var keys []models.APIKey
	if err := qt.db.Where("group_id IN ? AND status = ?", groupIDs, models.KeyStatusActive).Find(&keys).Error; err != nil {
		logrus.WithError(err).Error("QuotaTracker: Failed to query keys for sync")
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

// dailyResetLoop waits for midnight (Asia/Shanghai) and resets daily quota keys.
// 风鸟等日度额度的 provider 在北京时间零点重置。
func (qt *QuotaTracker) dailyResetLoop() {
	defer qt.wg.Done()

	// Use Asia/Shanghai timezone for daily reset alignment with Chinese business APIs
	cst, _ := time.LoadLocation("Asia/Shanghai")

	for {
		now := time.Now().In(cst)
		// Next midnight in CST
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, cst)
		timer := time.NewTimer(time.Until(nextMidnight))

		select {
		case <-qt.stopChan:
			timer.Stop()
			return
		case <-timer.C:
			logrus.Info("QuotaTracker: Daily reset triggered (midnight CST)")
			if err := qt.DailyReset(); err != nil {
				logrus.WithError(err).Error("QuotaTracker: Daily reset failed")
			}
		}
	}
}

// monthlyResetLoop waits for midnight on the 1st of each month and resets monthly quota keys.
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
