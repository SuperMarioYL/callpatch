package repair

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestitchFence_OpenAndTruncatedClose(t *testing.T) {
	// Opening ```json fence, closing fence truncated to two backticks.
	in := []byte("```json\n{\"x\":1}\n``")
	out, ok := RestitchFence(in)
	assert.True(t, ok)
	assert.True(t, json.Valid(out))
	assert.JSONEq(t, `{"x":1}`, string(out))
}

func TestRestitchFence_OpenOnly(t *testing.T) {
	// Opening fence present, closing fence entirely dropped (truncation).
	in := []byte("```json\n{\"x\":1}")
	out, ok := RestitchFence(in)
	assert.True(t, ok)
	assert.JSONEq(t, `{"x":1}`, string(out))
}

func TestRestitchFence_NoFence(t *testing.T) {
	in := []byte(`{"x":1}`)
	out, ok := RestitchFence(in)
	assert.False(t, ok)
	assert.Equal(t, in, out)
}

// TestRestitchFence_InValueFenceKept guards the v0.4.0 fix: coding agents write
// markdown files through tool calls, so a fence inside a string value is data,
// not the Qwen3 envelope. Promoting it rewrote the whole arguments value (the
// pre-fix pipeline returned "123" here with _repaired=true).
func TestRestitchFence_InValueFenceKept(t *testing.T) {
	in := []byte(`{"n":1,"s":"` + "```" + "\n" + `123` + "\n")
	out, ok := RestitchFence(in)
	assert.False(t, ok, "a fence that is not on a buffer boundary is data")
	assert.Equal(t, in, out)
}

// TestRestitchFence_ClosingFenceOnly covers the documented Qwen3 shape whose
// opening fence the local server already stripped: the trailing (possibly
// truncated) closing fence is removed and the JSON is preserved. Pre-fix this
// returned an empty buffer.
func TestRestitchFence_ClosingFenceOnly(t *testing.T) {
	out, ok := RestitchFence([]byte(`{"a":1}` + "\n" + "```"))
	assert.True(t, ok)
	assert.JSONEq(t, `{"a":1}`, string(out))
}

// TestRestitchFence_EnvelopeStillStripped guards the other half: the real
// envelope shapes keep working, whichever fence half survived truncation.
func TestRestitchFence_EnvelopeStillStripped(t *testing.T) {
	cases := []struct{ in, want string }{
		{"```json\n{\"a\":1}", `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```json\n{\"a\":1}\n``", `{"a":1}`},
		{"  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
	}
	for _, tc := range cases {
		out, ok := RestitchFence([]byte(tc.in))
		assert.True(t, ok, "input %q", tc.in)
		assert.JSONEq(t, tc.want, string(out), "input %q", tc.in)
	}
}

// TestRestitchFence_FenceWithoutInnerKeepsBuffer guards the destructive corner:
// a buffer that is only fence markers must be left for an honest failure rather
// than replaced by an empty string.
func TestRestitchFence_FenceWithoutInnerKeepsBuffer(t *testing.T) {
	for _, in := range []string{"```json\n", "```json\n```", "```"} {
		out, ok := RestitchFence([]byte(in))
		assert.False(t, ok, "input %q", in)
		assert.Equal(t, in, string(out))
	}
}

func TestStripBetween_TwoSentinels(t *testing.T) {
	in := []byte("<｜tool▁call▁arguments▁begin｜>{\"q\":1}<｜tool▁call▁end｜>")
	out, n := StripBetween(in, "<｜", "｜>")
	assert.Equal(t, 2, n)
	assert.JSONEq(t, `{"q":1}`, string(out))
}

func TestStripBetween_DanglingOpen(t *testing.T) {
	// Open sentinel with no closing sentinel: the open token is excised and the
	// function returns without looping. A no-close sentinel is not
	// deterministically repairable beyond dropping the open token, so the
	// remaining text is left for the caller — the contract here is "no crash, no
	// infinite loop, open token gone, JSON body preserved".
	in := []byte("<｜sentinel>{\"q\":1}")
	out, n := StripBetween(in, "<｜", "｜>")
	assert.Equal(t, 1, n)
	assert.NotContains(t, string(out), "<｜")
	assert.Contains(t, string(out), `{"q":1}`)
}

func TestUnescapeQuotes_DanglingBackslash(t *testing.T) {
	// Stream ended mid-escape: a dangling trailing backslash is dropped.
	in := []byte(`{"a":"hello\`)
	out, n := UnescapeQuotes(in)
	assert.Equal(t, 1, n)
	assert.Equal(t, `{"a":"hello`, string(out))
}

func TestUnescapeQuotes_NoOp(t *testing.T) {
	in := []byte(`{"a":"hello"}`)
	out, n := UnescapeQuotes(in)
	assert.Equal(t, 0, n)
	assert.Equal(t, in, out)
}
