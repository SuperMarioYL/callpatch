package spec_test

import (
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMatch_ExactKey asserts the exact key still resolves (the v0.1 path).
func TestMatch_ExactKey(t *testing.T) {
	for _, key := range []string{"qwen3", "deepseek-v4"} {
		s, ok := spec.Match(key)
		require.True(t, ok, "%q must match exactly", key)
		assert.Equal(t, key, s.Model)
	}
}

// TestMatch_CaseInsensitiveSubstring asserts realistic server-emitted model ids
// — which never carry the bare spec key — resolve to the right RepairSpec.
// v0.1.0's exact-match Lookup missed these and silently fell back to qwen3,
// leaving DeepSeek sentinel runs inside the "repaired" arguments.
func TestMatch_CaseInsensitiveSubstring(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"deepseek-ai/DeepSeek-V4", "deepseek-v4"},
		{"DeepSeek-V4-Instruct", "deepseek-v4"},
		{"Qwen/Qwen3-32B", "qwen3"},
		{"qwen3-235b-a22b", "qwen3"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			s, ok := spec.Match(tc.id)
			require.True(t, ok, "%q must match a registered spec", tc.id)
			assert.Equal(t, tc.want, s.Model)
		})
	}
}

// TestMatch_NoHit asserts an unrelated model name resolves to nothing (the
// caller owns the qwen3 fallback).
func TestMatch_NoHit(t *testing.T) {
	_, ok := spec.Match("llama-3.3-70b")
	assert.False(t, ok, "an unrelated model must not match a spec")
}

// TestMatch_LongestKeyWins asserts that when several registered keys are
// substrings of the model id, the most specific one wins.
func TestMatch_LongestKeyWins(t *testing.T) {
	// Registered keys are "qwen3" and "deepseek-v4"; a hypothetical
	// "qwen3-deepseek-v4-hybrid" contains both — the longer key must win.
	s, ok := spec.Match("qwen3-deepseek-v4-hybrid")
	require.True(t, ok)
	assert.Equal(t, "deepseek-v4", s.Model)
}
