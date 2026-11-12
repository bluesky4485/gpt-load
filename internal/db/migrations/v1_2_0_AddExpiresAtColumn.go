package db

import (
	"fmt"

	"gpt-load/internal/models"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// V1_2_0_AddExpiresAtColumn adds expires_at column to search_caches table.
// Existing entries get expires_at = created_at + 7 days (backward compatible with old Tavily-only cache).
func V1_2_0_AddExpiresAtColumn(db *gorm.DB) error {
	// Migrator().HasTable/HasColumn work across MySQL/Postgres/SQLite; the previous
	// hand-written checks (information_schema / sqlite_master / pragma_table_info)
	// only covered MySQL and SQLite, so Postgres misdetected a fresh-install and
	// skipped the migration, leaving AutoMigrate to fail adding a NOT NULL column
	// to a table that already had rows.
	if !db.Migrator().HasTable("search_caches") {
		logrus.Info("search_caches table does not exist yet, skipping v1.2.0 (AutoMigrate will create it)...")
		return nil
	}

	if db.Migrator().HasColumn(&models.SearchCache{}, "ExpiresAt") {
		logrus.Info("Column expires_at already exists in search_caches, skipping v1.2.0...")
		return nil
	}

	switch db.Dialector.Name() {
	case "mysql":
		if err := db.Exec("ALTER TABLE search_caches ADD COLUMN expires_at DATETIME(3) NOT NULL DEFAULT '2099-01-01 00:00:00'").Error; err != nil {
			return fmt.Errorf("failed to add expires_at column: %w", err)
		}
		// Backfill: set expires_at = created_at + 7 days for existing entries
		if err := db.Exec("UPDATE search_caches SET expires_at = DATE_ADD(created_at, INTERVAL 7 DAY)").Error; err != nil {
			logrus.WithError(err).Warn("Failed to backfill expires_at, existing entries will use cleanup fallback")
		}
	case "postgres":
		if err := db.Exec("ALTER TABLE search_caches ADD COLUMN expires_at timestamptz NOT NULL DEFAULT '2099-01-01 00:00:00+00'").Error; err != nil {
			return fmt.Errorf("failed to add expires_at column: %w", err)
		}
		if err := db.Exec("UPDATE search_caches SET expires_at = created_at + interval '7 days'").Error; err != nil {
			logrus.WithError(err).Warn("Failed to backfill expires_at, existing entries will use cleanup fallback")
		}
	default: // sqlite
		if err := db.Exec("ALTER TABLE search_caches ADD COLUMN expires_at DATETIME NOT NULL DEFAULT '2099-01-01 00:00:00'").Error; err != nil {
			return fmt.Errorf("failed to add expires_at column: %w", err)
		}
		if err := db.Exec("UPDATE search_caches SET expires_at = datetime(created_at, '+7 days')").Error; err != nil {
			logrus.WithError(err).Warn("Failed to backfill expires_at, existing entries will use cleanup fallback")
		}
	}

	logrus.Info("Migration v1.2.0 completed: added expires_at column to search_caches")
	return nil
}
