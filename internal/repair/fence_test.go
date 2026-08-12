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
