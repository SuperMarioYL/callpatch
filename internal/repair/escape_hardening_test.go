package repair_test

import (
	"encoding/json"
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBalanceBraces_RawControlCharsInsideString guards the v0.3.0 fix: local
// CN models emit raw newlines/tabs inside string values (multiline shell
// commands, code snippets); those are illegal inside JSON strings and were
// previously unrepairable. They must be rewritten to their two-byte escapes.
func TestBalanceBraces_RawControlCharsInsideString(t *testing.T) {
	in := []byte("{\"cmd\": \"echo one\nline\ttwo\"}")
	out, n := repair.BalanceBraces(in)
	assert.True(t, json.Valid(out), "expected valid JSON, got %s", out)
	assert.JSONEq(t, "{\"cmd\": \"echo one\\nline\\ttwo\"}", string(out))
	assert.Positive(t, n)

	// Bare CR inside a string is SSE framing noise and is dropped.
	out, _ = repair.BalanceBraces([]byte("{\"a\": \"x\ry\"}"))
	assert.JSONEq(t, `{"a": "xy"}`, string(out))

	// Control characters outside strings are legal JSON whitespace and pass
	// through untouched.
	in = []byte("{\n\t\"a\": 1\n}")
	out, n = repair.BalanceBraces(in)
	assert.Zero(t, n)
	assert.Equal(t, in, out)
}

// TestBalanceBraces_CompleteEscapedBackslashSurvives guards the v0.3.0
// escape-parity fix: a stream truncated right after a COMPLETE escaped
// backslash (even run) must keep both backslashes — the v0.2.0 byte-inspection
// fixup dropped one, leaving \" before the closing quote (still invalid) and
// failing the deepseek pipeline, which has no UnescapeQuotes step.
func TestBalanceBraces_CompleteEscapedBackslashSurvives(t *testing.T) {
	in := []byte(`{"a": "x\\`) // ends with a complete escaped backslash
	out, n := repair.BalanceBraces(in)
	assert.True(t, json.Valid(out), "expected valid JSON, got %s", out)
	assert.JSONEq(t, `{"a": "x\\"}`, string(out), "the escaped backslash must survive")
	assert.Equal(t, 2, n) // closing quote + closing brace

	// The deepseek pipeline (no UnescapeQuotes step) must repair this shape.
	sp, ok := spec.Lookup("deepseek-v4")
	require.True(t, ok)
	clean, rpt := repair.NewPipeline(sp).Repair(in)
	assert.NoError(t, rpt.Err)
	assert.True(t, rpt.Repaired)
	assert.JSONEq(t, `{"a": "x\\"}`, string(clean), "data must be preserved")
}

// TestBalanceBraces_DanglingEscapeStillDropped pins the other half of the
// parity contract: an ODD trailing backslash run is an escape the stream never
// completed and is still dropped before the closing quote. Five trailing
// backslashes = two complete escaped pairs plus one dangling escape, so the
// value keeps two backslashes.
func TestBalanceBraces_DanglingEscapeStillDropped(t *testing.T) {
	in := []byte(`{"a":"hello\\\\\`)
	out, n := repair.BalanceBraces(in)
	assert.True(t, json.Valid(out), "expected valid JSON, got %s", out)
	assert.JSONEq(t, `{"a":"hello\\\\"}`, string(out))
	assert.Equal(t, 2, n)
}

// TestUnescapeQuotes_Parity guards the v0.3.0 fix: UnescapeQuotes strips only
// a dangling (odd) trailing backslash run; a complete escaped backslash (even
// run) must survive so the qwen3 pipeline stops mangling values ending in \.
func TestUnescapeQuotes_Parity(t *testing.T) {
	out, n := repair.UnescapeQuotes([]byte(`{"a":"hello\`))
	assert.Equal(t, 1, n)
	assert.Equal(t, `{"a":"hello`, string(out))

	in := []byte(`{"a":"hello\\`) // even run: complete escaped backslash
	out, n = repair.UnescapeQuotes(in)
	assert.Zero(t, n)
	assert.Equal(t, in, out)

	out, n = repair.UnescapeQuotes([]byte(`{"a":"hello\\\\\`)) // odd run (3)
	assert.Equal(t, 1, n)
	assert.Equal(t, `{"a":"hello\\\\`, string(out))
}

// TestPipeline_RawNewlineRepaired end-to-end: the full qwen3 pipeline repairs
// a raw-newline-inside-string failure that v0.2.0 reported as unrepairable.
func TestPipeline_RawNewlineRepaired(t *testing.T) {
	sp, ok := spec.Lookup("qwen3")
	require.True(t, ok)
	clean, rpt := repair.NewPipeline(sp).Repair([]byte("{\"cmd\": \"echo one\nline two\"}"))
	require.NoError(t, rpt.Err)
	assert.True(t, rpt.Repaired)
	assert.JSONEq(t, "{\"cmd\": \"echo one\\nline two\"}", string(clean))
}
