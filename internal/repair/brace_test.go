package repair

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBalanceBraces_TruncatedString(t *testing.T) {
	out, n := BalanceBraces([]byte(`{"city":"北京","unit":"celsius`))
	assert.Equal(t, 2, n) // one '"' + one '}'
	assert.True(t, json.Valid(out), "expected valid JSON, got %s", out)
	assert.JSONEq(t, `{"city":"北京","unit":"celsius"}`, string(out))
}

func TestBalanceBraces_NestedUnbalanced(t *testing.T) {
	in := []byte(`{"name":"search","args":{"q":"vllm tool call","limit":3`)
	out, n := BalanceBraces(in)
	assert.Equal(t, 2, n) // two '}'
	assert.True(t, json.Valid(out))
	assert.JSONEq(t, `{"name":"search","args":{"q":"vllm tool call","limit":3}}`, string(out))
}

func TestBalanceBraces_DanglingBackslash(t *testing.T) {
	// Stream cut mid-escape: the trailing '\' must be dropped before the quote.
	in := []byte(`{"a":"hello\`)
	out, n := BalanceBraces(in)
	assert.Equal(t, 2, n)
	assert.True(t, json.Valid(out))
	assert.JSONEq(t, `{"a":"hello"}`, string(out))
}

func TestBalanceBraces_AlreadyValid(t *testing.T) {
	in := []byte(`{"a":1,"b":[2,3]}`)
	out, n := BalanceBraces(in)
	assert.Equal(t, 0, n)
	assert.Equal(t, in, out)
}

func TestBalanceBraces_ArrayClose(t *testing.T) {
	in := []byte(`{"items":["a","b"`)
	out, n := BalanceBraces(in)
	assert.Equal(t, 2, n) // ']' + '}'
	assert.True(t, json.Valid(out))
	assert.JSONEq(t, `{"items":["a","b"]}`, string(out))
}
