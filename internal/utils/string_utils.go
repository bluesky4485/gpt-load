package utils

import (
	"fmt"
	"strings"
)

// knownKeyPrefixes lists well-known API key prefixes that are safe to display in logs.
// Preserving these prefixes aids identification without compromising security.
var knownKeyPrefixes = []string{
	"tvly-dev-",
	"tvly-",
	"sk-proj-",
	"sk-",
	"key-",
	"ant-",
}

// MaskAPIKey masks an API key for safe logging, preserving known prefixes.
// Examples:
//
//	"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" → "tvly-dev-****O42X"
//	"sk-proj-abc123def456xyz" → "sk-proj-****6xyz"
//	"short" → "****"
func MaskAPIKey(key string) string {
	length := len(key)
	if length <= 8 {
		return "****"
	}

	// Detect and preserve known prefixes
	prefixLen := 0
	for _, prefix := range knownKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			prefixLen = len(prefix)
			break
		}
	}

	// Ensure we don't expose more than half the key via prefix
	maxPrefix := length / 2
	if prefixLen > maxPrefix {
		prefixLen = maxPrefix
	}

	suffixLen := 4
	// Ensure prefix + suffix doesn't overlap
	if prefixLen+suffixLen >= length {
		suffixLen = length - prefixLen - 1
		if suffixLen < 0 {
			suffixLen = 0
		}
	}

	prefix := key[:prefixLen]
	suffix := key[length-suffixLen:]
	return fmt.Sprintf("%s****%s", prefix, suffix)
}


// RedactSecret replaces every occurrence of secret in text with its masked form.
func RedactSecret(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, MaskAPIKey(secret))
}

// TruncateString shortens a string to a maximum length.
func TruncateString(s string, maxLength int) string {
	if len(s) > maxLength {
		return s[:maxLength]
	}
	return s
}

// SplitAndTrim splits a string by a separator
func SplitAndTrim(s string, sep string) []string {
	if s == "" {
		return []string{}
	}

	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// StringToSet converts a separator-delimited string into a set
func StringToSet(s string, sep string) map[string]struct{} {
	parts := SplitAndTrim(s, sep)
	if len(parts) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		set[part] = struct{}{}
	}
	return set
}
