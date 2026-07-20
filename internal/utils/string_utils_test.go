package utils

import "testing"

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tavily dev key preserves prefix",
			input:    "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",
			expected: "tvly-dev-****O42X",
		},
		{
			name:     "tavily key preserves prefix",
			input:    "tvly-abc123def456ghi789jkl",
			expected: "tvly-****9jkl",
		},
		{
			name:     "openai sk-proj key preserves prefix",
			input:    "sk-proj-abcdefghijklmnopqrstuvwxyz123456",
			expected: "sk-proj-****3456",
		},
		{
			name:     "openai sk key preserves prefix",
			input:    "sk-abcdefghijklmnopqrstuvwxyz",
			expected: "sk-****wxyz",
		},
		{
			name:     "generic key preserves prefix",
			input:    "key-abcdefghijklmnop",
			expected: "key-****mnop",
		},
		{
			name:     "unknown prefix falls back to first 0 chars",
			input:    "abcdefghijklmnop",
			expected: "****mnop",
		},
		{
			name:     "short key 8 chars returns stars",
			input:    "12345678",
			expected: "****",
		},
		{
			name:     "very short key returns stars",
			input:    "abc",
			expected: "****",
		},
		{
			name:     "empty key returns stars",
			input:    "",
			expected: "****",
		},
		{
			name:     "9 char key no known prefix",
			input:    "abcdefghi",
			expected: "****fghi",
		},
		{
			name:     "anthropic key preserves prefix",
			input:    "ant-abcdefghijklmnopqrstuv",
			expected: "ant-****stuv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskAPIKey(tt.input)
			if result != tt.expected {
				t.Errorf("MaskAPIKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMaskAPIKey_NoFullKeyLeak(t *testing.T) {
	// Ensure no test case ever returns the full key
	keys := []string{
		"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",
		"sk-proj-abc123def456",
		"short",
		"ab",
		"",
	}
	for _, key := range keys {
		masked := MaskAPIKey(key)
		if key != "" && masked == key {
			t.Errorf("MaskAPIKey(%q) returned the full key unchanged", key)
		}
	}
}


// TestRedactSecret asserts that every occurrence of a known secret inside an
// arbitrary text is replaced with its masked form. Regression test for the
// Gemini-channel upstream key leak via unmasked transport errors (CWE-200).
func TestRedactSecret(t *testing.T) {
	secret := "sk-secret-upstream-key-1234"
	text := `Post "http://upstream/v1beta/models/gemini-pro:generateContent?key=sk-secret-upstream-key-1234": context deadline exceeded`

	got := RedactSecret(text, secret)

	if got == text {
		t.Fatalf("RedactSecret did not modify the text: %q", got)
	}
	for i := 0; i+len(secret) <= len(got); i++ {
		if got[i:i+len(secret)] == secret {
			t.Fatalf("raw secret still present in redacted text: %q", got)
		}
	}
	want := `Post "http://upstream/v1beta/models/gemini-pro:generateContent?key=sk-****1234": context deadline exceeded`
	if got != want {
		t.Errorf("RedactSecret() = %q, want %q", got, want)
	}
}

// TestRedactSecretMultipleOccurrences asserts all occurrences are redacted, not just the first.
func TestRedactSecretMultipleOccurrences(t *testing.T) {
	secret := "sk-secret-upstream-key-1234"
	text := secret + " appears twice: " + secret

	got := RedactSecret(text, secret)

	masked := MaskAPIKey(secret)
	want := masked + " appears twice: " + masked
	if got != want {
		t.Errorf("RedactSecret() = %q, want %q", got, want)
	}
}

// TestRedactSecretEmptySecret asserts an empty secret leaves the text untouched.
func TestRedactSecretEmptySecret(t *testing.T) {
	text := "some upstream error with no secret"

	got := RedactSecret(text, "")

	if got != text {
		t.Errorf("RedactSecret() with empty secret = %q, want unchanged %q", got, text)
	}
}

// TestRedactSecretNotPresent asserts text without the secret is returned unchanged.
func TestRedactSecretNotPresent(t *testing.T) {
	text := "connection refused"

	got := RedactSecret(text, "sk-secret-upstream-key-1234")

	if got != text {
		t.Errorf("RedactSecret() = %q, want unchanged %q", got, text)
	}
}
