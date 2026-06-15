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
