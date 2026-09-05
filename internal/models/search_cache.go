package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SearchCache stores cached search/data API responses.
// Cache key is SHA-256 of group + method + endpoint (path + query) + sorted canonical request fields.
// Cache hits skip key selection and quota consumption entirely — saving both keys and quota.
type SearchCache struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CacheKey     string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"cache_key"` // SHA-256 hex
	GroupID      uint      `gorm:"not null;index" json:"group_id"`
	Endpoint     string    `gorm:"type:varchar(255);not null" json:"endpoint"` // e.g. "search" or "dataDimension?version=B1&key=..."
	ResponseBody string    `gorm:"type:text;not null" json:"response_body"`    // cached HTTP response body
	StatusCode   int       `gorm:"not null;default:200" json:"status_code"`    // cached HTTP status code
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

// GenerateCacheKey produces a deterministic SHA-256 hash from the group ID,
// HTTP method, endpoint (path with optional query string) and the canonical
// (sorted-key) form of the JSON request body.
//
// The group ID is part of the key so identical requests against different
// groups (i.e. different upstream accounts) never share cache entries.
// The query string is canonicalized (sorted) so that parameter order does not
// affect the key — this is what makes GET-based channels like Fengniao
// cacheable even though they carry all parameters in the query.
func GenerateCacheKey(groupID uint, method, endpoint string, body []byte) (string, error) {
	endpointPath, rawQuery := splitEndpoint(endpoint)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("g%d|%s|%s", groupID, strings.ToUpper(method), endpointPath))

	appendSortedValues := func(values url.Values) {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			vs := values[k]
			sort.Strings(vs)
			v, _ := json.Marshal(vs)
			sb.WriteByte('|')
			sb.WriteString(k)
			sb.WriteByte(':')
			sb.Write(v)
		}
	}

	if rawQuery != "" {
		values, err := url.ParseQuery(rawQuery)
		if err != nil {
			return "", fmt.Errorf("failed to parse endpoint query for cache key: %w", err)
		}
		appendSortedValues(values)
	}

	if len(body) > 0 {
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			return "", fmt.Errorf("failed to parse request body for cache key: %w", err)
		}
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v, _ := json.Marshal(fields[k])
			sb.WriteByte('|')
			sb.WriteString(k)
			sb.WriteByte(':')
			sb.Write(v)
		}
	}

	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:]), nil
}

// splitEndpoint splits an endpoint string like "dataDimension?version=B1" into
// its path ("dataDimension") and raw query ("version=B1"). Leading slashes are
// trimmed so "search", "/search" and "search?" all normalize to the same path.
func splitEndpoint(endpoint string) (path, rawQuery string) {
	endpoint = strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	if i := strings.IndexByte(endpoint, '?'); i >= 0 {
		return endpoint[:i], endpoint[i+1:]
	}
	return endpoint, ""
}
