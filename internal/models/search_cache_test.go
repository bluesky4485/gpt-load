package models

import (
	"testing"
)

func TestGenerateCacheKey_SameFieldsSameOrder(t *testing.T) {
	body := []byte(`{"query":"golang","max_results":5}`)
	k1, err := GenerateCacheKey(1, "POST", "search", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, err := GenerateCacheKey(1, "POST", "search", body)
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

	k1, _ := GenerateCacheKey(1, "POST", "search", body1)
	k2, _ := GenerateCacheKey(1, "POST", "search", body2)
	if k1 != k2 {
		t.Errorf("different field order should produce same key: %s != %s", k1, k2)
	}
}

func TestGenerateCacheKey_DifferentQueries(t *testing.T) {
	body1 := []byte(`{"query":"golang"}`)
	body2 := []byte(`{"query":"python"}`)

	k1, _ := GenerateCacheKey(1, "POST", "search", body1)
	k2, _ := GenerateCacheKey(1, "POST", "search", body2)
	if k1 == k2 {
		t.Error("different queries should produce different keys")
	}
}

func TestGenerateCacheKey_EndpointMatters(t *testing.T) {
	body := []byte(`{"query":"test"}`)

	k1, _ := GenerateCacheKey(1, "POST", "search", body)
	k2, _ := GenerateCacheKey(1, "POST", "extract", body)
	if k1 == k2 {
		t.Error("different endpoints should produce different keys")
	}
}

func TestGenerateCacheKey_GroupIsolation(t *testing.T) {
	body := []byte(`{"query":"test"}`)

	k1, _ := GenerateCacheKey(1, "POST", "search", body)
	k2, _ := GenerateCacheKey(2, "POST", "search", body)
	if k1 == k2 {
		t.Error("same request against different groups must not share a cache key")
	}
}

func TestGenerateCacheKey_MethodMatters(t *testing.T) {
	body := []byte(`{"query":"test"}`)

	k1, _ := GenerateCacheKey(1, "GET", "search", body)
	k2, _ := GenerateCacheKey(1, "POST", "search", body)
	if k1 == k2 {
		t.Error("same endpoint with different methods should produce different keys")
	}
}

func TestGenerateCacheKey_EmptyBody(t *testing.T) {
	// GET requests have an empty body — this must not be an error, otherwise
	// GET-based channels (Fengniao) can never be cached.
	k1, err := GenerateCacheKey(1, "GET", "search", nil)
	if err != nil {
		t.Fatalf("unexpected error for empty body: %v", err)
	}
	if len(k1) != 64 {
		t.Errorf("expected 64-char hex SHA-256, got %d chars", len(k1))
	}

	body := []byte(`{}`)
	k2, err := GenerateCacheKey(1, "GET", "search", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k1 != k2 {
		t.Errorf("nil body and empty JSON object should produce the same key: %s != %s", k1, k2)
	}
}

func TestGenerateCacheKey_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	_, err := GenerateCacheKey(1, "POST", "search", body)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestGenerateCacheKey_QueryParams(t *testing.T) {
	// GET endpoint with query: different params → different keys.
	k1, err := GenerateCacheKey(1, "GET", "dataDimension?version=B1&entid=X", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, _ := GenerateCacheKey(1, "GET", "dataDimension?version=B2&entid=X", nil)
	if k1 == k2 {
		t.Error("different query params should produce different keys")
	}

	// Parameter order in the query must not matter.
	k3, _ := GenerateCacheKey(1, "GET", "dataDimension?entid=X&version=B1", nil)
	if k1 != k3 {
		t.Errorf("query param order should not affect key: %s != %s", k1, k3)
	}
}

func TestGenerateCacheKey_EndpointNormalization(t *testing.T) {
	body := []byte(`{"query":"test"}`)

	k1, _ := GenerateCacheKey(1, "POST", "search", body)
	k2, _ := GenerateCacheKey(1, "POST", "/search", body)
	k3, _ := GenerateCacheKey(1, "POST", "search?", body)
	if k1 != k2 || k1 != k3 {
		t.Errorf("leading slash / trailing ? should not affect key: %s, %s, %s", k1, k2, k3)
	}
}

func TestGenerateCacheKey_NestedObjects(t *testing.T) {
	body1 := []byte(`{"query":"test","options":{"depth":"advanced"}}`)
	body2 := []byte(`{"options":{"depth":"advanced"},"query":"test"}`)

	k1, _ := GenerateCacheKey(1, "POST", "search", body1)
	k2, _ := GenerateCacheKey(1, "POST", "search", body2)
	if k1 != k2 {
		t.Errorf("nested objects with different field order should produce same key: %s != %s", k1, k2)
	}
}

func TestGenerateCacheKey_ExtraFields(t *testing.T) {
	body1 := []byte(`{"query":"test"}`)
	body2 := []byte(`{"query":"test","max_results":10}`)

	k1, _ := GenerateCacheKey(1, "POST", "search", body1)
	k2, _ := GenerateCacheKey(1, "POST", "search", body2)
	if k1 == k2 {
		t.Error("different field sets should produce different keys")
	}
}

func TestGenerateCacheKey_ArrayValues(t *testing.T) {
	body1 := []byte(`{"urls":["https://a.com","https://b.com"]}`)
	body2 := []byte(`{"urls":["https://a.com","https://b.com"]}`)
	body3 := []byte(`{"urls":["https://b.com","https://a.com"]}`)

	k1, _ := GenerateCacheKey(1, "POST", "extract", body1)
	k2, _ := GenerateCacheKey(1, "POST", "extract", body2)
	k3, _ := GenerateCacheKey(1, "POST", "extract", body3)

	if k1 != k2 {
		t.Error("identical arrays should produce same key")
	}
	if k1 == k3 {
		t.Error("different array order should produce different key")
	}
}
