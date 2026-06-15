package models

import (
	"testing"
)

func TestGenerateCacheKey_SameFieldsSameOrder(t *testing.T) {
	body := []byte(`{"query":"golang","max_results":5}`)
	k1, err := GenerateCacheKey("search", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, err := GenerateCacheKey("search", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k1 != k2 {
		t.Errorf("same input should produce same key: %s != %s", k1, k2)
	}
	if len(k1) != 64 {
		t.Errorf("expected 64-char hex SHA-256, got %d chars", len(k1))
	}
}

func TestGenerateCacheKey_FieldOrderInvariant(t *testing.T) {
	body1 := []byte(`{"query":"golang","max_results":5}`)
	body2 := []byte(`{"max_results":5,"query":"golang"}`)

	k1, _ := GenerateCacheKey("search", body1)
	k2, _ := GenerateCacheKey("search", body2)
	if k1 != k2 {
		t.Errorf("different field order should produce same key: %s != %s", k1, k2)
	}
}

func TestGenerateCacheKey_DifferentQueries(t *testing.T) {
	body1 := []byte(`{"query":"golang"}`)
	body2 := []byte(`{"query":"python"}`)

	k1, _ := GenerateCacheKey("search", body1)
	k2, _ := GenerateCacheKey("search", body2)
	if k1 == k2 {
		t.Error("different queries should produce different keys")
	}
}

func TestGenerateCacheKey_EndpointMatters(t *testing.T) {
	body := []byte(`{"query":"test"}`)

	k1, _ := GenerateCacheKey("search", body)
	k2, _ := GenerateCacheKey("extract", body)
	if k1 == k2 {
		t.Error("different endpoints should produce different keys")
	}
}

func TestGenerateCacheKey_EmptyBody(t *testing.T) {
	body := []byte(`{}`)
	k1, err := GenerateCacheKey("search", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k1) != 64 {
		t.Errorf("expected 64-char hex SHA-256, got %d chars", len(k1))
	}
}

func TestGenerateCacheKey_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	_, err := GenerateCacheKey("search", body)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestGenerateCacheKey_NestedObjects(t *testing.T) {
	body1 := []byte(`{"query":"test","options":{"depth":"advanced"}}`)
	body2 := []byte(`{"options":{"depth":"advanced"},"query":"test"}`)

	k1, _ := GenerateCacheKey("search", body1)
	k2, _ := GenerateCacheKey("search", body2)
	if k1 != k2 {
		t.Errorf("nested objects with different field order should produce same key: %s != %s", k1, k2)
	}
}

func TestGenerateCacheKey_ExtraFields(t *testing.T) {
	body1 := []byte(`{"query":"test"}`)
	body2 := []byte(`{"query":"test","max_results":10}`)

	k1, _ := GenerateCacheKey("search", body1)
	k2, _ := GenerateCacheKey("search", body2)
	if k1 == k2 {
		t.Error("different field sets should produce different keys")
	}
}

func TestGenerateCacheKey_ArrayValues(t *testing.T) {
	body1 := []byte(`{"urls":["https://a.com","https://b.com"]}`)
	body2 := []byte(`{"urls":["https://a.com","https://b.com"]}`)
	body3 := []byte(`{"urls":["https://b.com","https://a.com"]}`)

	k1, _ := GenerateCacheKey("extract", body1)
	k2, _ := GenerateCacheKey("extract", body2)
	k3, _ := GenerateCacheKey("extract", body3)

	if k1 != k2 {
		t.Error("identical arrays should produce same key")
	}
	if k1 == k3 {
		t.Error("different array order should produce different key")
	}
}
