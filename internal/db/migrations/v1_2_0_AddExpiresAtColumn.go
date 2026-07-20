package db

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// V1_2_0_AddExpiresAtColumn adds expires_at column to search_caches table.
// Existing entries get expires_at = created_at + 7 days (backward compatible with old Tavily-only cache).
func V1_2_0_AddExpiresAtColumn(db *gorm.DB) error {
	// Check if search_caches table exists
	var tableCount int64
	if db.Dialector.Name() == "mysql" {
		db.Raw(`SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'search_caches'`).Count(&tableCount)
	} else {
		db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='search_caches'`).Count(&tableCount)
	}
	if tableCount == 0 {
		logrus.Info("search_caches table does not exist yet, skipping v1.2.0 (AutoMigrate will create it)...")
		return nil
	}

	// Check if column already exists
	var count int64
	if db.Dialector.Name() == "mysql" {
		db.Raw(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'search_caches' AND COLUMN_NAME = 'expires_at'`).Count(&count)
	} else {
		db.Raw(`SELECT COUNT(*) FROM pragma_table_info('search_caches') WHERE name = 'expires_at'`).Count(&count)
	}
	if count > 0 {
		logrus.Info("Column expires_at already exists in search_caches, skipping v1.2.0...")
		return nil
	}

	// Add column with a safe default value that works for both MySQL and SQLite
	if err := db.Exec("ALTER TABLE search_caches ADD COLUMN expires_at DATETIME NOT NULL DEFAULT '2099-01-01 00:00:00'").Error; err != nil {
		return fmt.Errorf("failed to add expires_at column: %w", err)
	}

	// Backfill: set expires_at = created_at + 7 days for existing entries
	if err := db.Exec("UPDATE search_caches SET expires_at = datetime(created_at, '+7 days')").Error; err != nil {
		logrus.WithError(err).Warn("Failed to backfill expires_at, existing entries will use cleanup fallback")
	}

	logrus.Info("Migration v1.2.0 completed: added expires_at column to search_caches")
	return nil
}
