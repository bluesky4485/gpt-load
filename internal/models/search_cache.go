package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SearchCache stores cached search/data API responses.
// Cache key is SHA-256 of sorted canonical request fields.
// Cache hits skip key selection and quota consumption entirely — saving both keys and quota.
type SearchCache struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CacheKey     string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"cache_key"` // SHA-256 hex
	GroupID      uint      `gorm:"not null;index" json:"group_id"`
	Endpoint     string    `gorm:"type:varchar(50);not null" json:"endpoint"` // e.g., "search", "extract", "searchHint"
	ResponseBody string    `gorm:"type:text;not null" json:"response_body"`   // cached HTTP response body
	StatusCode   int       `gorm:"not null;default:200" json:"status_code"`   // cached HTTP status code
	HitCount     int64     `gorm:"not null;default:0" json:"hit_count"`
	ExpiresAt    time.Time `gorm:"not null;index" json:"expires_at"` // 过期时间，用于定时清理
	CreatedAt    time.Time `json:"created_at"`
	LastAccessAt time.Time `json:"last_access_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName specifies the table name for GORM.
func (SearchCache) TableName() string {
	return "search_caches"
}

// GenerateCacheKey produces a deterministic SHA-256 hash from the endpoint and
// the canonical (sorted-key) form of the JSON request body. This ensures that
// identical requests always map to the same cache entry regardless of JSON
// field ordering in the original request.
func GenerateCacheKey(endpoint string, body []byte) (string, error) {
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", fmt.Errorf("failed to parse request body for cache key: %w", err)
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		v, _ := json.Marshal(fields[k])
		sb.WriteString(k)
		sb.WriteByte(':')
		sb.Write(v)
	}

	raw := endpoint + "|" + sb.String()
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:]), nil
}
