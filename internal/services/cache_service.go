package services

import (
	"context"
	"fmt"
	"gpt-load/internal/channel"
	"gpt-load/internal/models"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cache cleanup configuration.
const (
	cacheCleanupInterval = 1 * time.Hour      // how often to run cleanup
	cacheDefaultTTL      = 7 * 24 * time.Hour // default TTL for channels without explicit CacheTTL
)

// CacheService manages search/data API response caching.
// Cache hits skip key selection and quota consumption entirely.
type CacheService struct {
	db             *gorm.DB
	channelFactory *channel.Factory
	stopChan       chan struct{}
	wg             sync.WaitGroup
	running        atomic.Bool
}

// NewCacheService creates a new CacheService.
func NewCacheService(db *gorm.DB, channelFactory *channel.Factory) *CacheService {
	return &CacheService{
		db:             db,
		channelFactory: channelFactory,
		stopChan:       make(chan struct{}),
	}
}

// Get looks up a cached response by its SHA-256 cache key.
// On hit, atomically increments HitCount and updates LastAccessAt.
// Returns nil if no cache entry is found or the entry has expired.
func (cs *CacheService) Get(cacheKey string) *models.SearchCache {
	var entry models.SearchCache
	if err := cs.db.Where("cache_key = ? AND expires_at > ?", cacheKey, time.Now()).First(&entry).Error; err != nil {
		return nil // cache miss or expired
	}

	now := time.Now()
	cs.db.Model(&models.SearchCache{}).Where("id = ?", entry.ID).
		Updates(map[string]any{
			"hit_count":      gorm.Expr("hit_count + 1"),
			"last_access_at": now,
		})
	entry.HitCount++
	entry.LastAccessAt = now

	return &entry
}

// Put stores or updates a cached response. Uses UPSERT on the unique cache_key index.
// ttlSeconds determines the cache expiration time. If 0, the system default is used.
func (cs *CacheService) Put(cacheKey string, groupID uint, endpoint string, responseBody string, statusCode int, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		ttlSeconds = int(cacheDefaultTTL.Seconds())
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second)

	entry := models.SearchCache{
		CacheKey:     cacheKey,
		GroupID:      groupID,
		Endpoint:     endpoint,
		ResponseBody: responseBody,
		StatusCode:   statusCode,
		ExpiresAt:    expiresAt,
		HitCount:     0,
		CreatedAt:    now,
		LastAccessAt: now,
	}

	result := cs.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "cache_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"response_body", "status_code", "expires_at", "last_access_at", "updated_at"}),
	}).Create(&entry)

	if result.Error != nil {
		return fmt.Errorf("failed to cache response: %w", result.Error)
	}
	return nil
}

// GetTTLForGroup returns the cache TTL in seconds for a group based on its channel type.
func (cs *CacheService) GetTTLForGroup(groupID uint) int {
	var group models.Group
	if err := cs.db.First(&group, groupID).Error; err != nil {
		return int(cacheDefaultTTL.Seconds())
	}

	ch, err := cs.channelFactory.GetChannel(&group)
	if err != nil {
		return int(cacheDefaultTTL.Seconds())
	}

	ttl := channel.GetCacheTTL(ch)
	if ttl > 0 {
		return ttl
	}
	return int(cacheDefaultTTL.Seconds())
}

// Cleanup removes cache entries whose expires_at is in the past.
// Returns the number of deleted entries.
func (cs *CacheService) Cleanup() (int64, error) {
	now := time.Now()
	result := cs.db.Where("expires_at < ?", now).Delete(&models.SearchCache{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to cleanup cache: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		logrus.WithField("deleted", result.RowsAffected).Info("CacheService: Cleaned up expired cache entries")
	}
	return result.RowsAffected, nil
}

// Stats returns total cache entry count and aggregate hit count.
func (cs *CacheService) Stats() (totalEntries int64, totalHits int64, err error) {
	type result struct {
		Count int64
		Hits  int64
	}
	var r result
	err = cs.db.Model(&models.SearchCache{}).
		Select("COUNT(*) as count, COALESCE(SUM(hit_count), 0) as hits").
		Scan(&r).Error
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get cache stats: %w", err)
	}
	return r.Count, r.Hits, nil
}

// StatsByGroup returns cache entry count and hits for a specific group.
func (cs *CacheService) StatsByGroup(groupID uint) (totalEntries int64, totalHits int64, err error) {
	type result struct {
		Count int64
		Hits  int64
	}
	var r result
	err = cs.db.Model(&models.SearchCache{}).
		Select("COUNT(*) as count, COALESCE(SUM(hit_count), 0) as hits").
		Where("group_id = ?", groupID).
		Scan(&r).Error
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get cache stats for group %d: %w", groupID, err)
	}
	return r.Count, r.Hits, nil
}

// --- Background cleanup job ---

// Start launches the background cache cleanup goroutine.
func (cs *CacheService) Start() {
	logrus.Info("CacheService: Starting background cleanup job...")
	cs.wg.Add(1)
	go cs.cleanupLoop()
}

// Stop gracefully shuts down the background cache cleanup goroutine.
func (cs *CacheService) Stop(ctx context.Context) {
	close(cs.stopChan)

	done := make(chan struct{})
	go func() {
		cs.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("CacheService stopped gracefully.")
	case <-ctx.Done():
		logrus.Warn("CacheService stop timed out.")
	}
}

// cleanupLoop periodically removes expired cache entries.
func (cs *CacheService) cleanupLoop() {
	defer cs.wg.Done()

	// Run immediately on startup.
	if !cs.running.Swap(true) {
		cs.Cleanup()
		cs.running.Store(false)
	}

	ticker := time.NewTicker(cacheCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if cs.running.Load() {
				continue // previous cleanup still running
			}
			cs.running.Store(true)
			cs.Cleanup()
			cs.running.Store(false)
		case <-cs.stopChan:
			return
		}
	}
}
