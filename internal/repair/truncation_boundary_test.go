package repair

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBalanceBraces_MemberBoundaryTruncation pins the v0.4.0 fix: a stream that
// ends at a member boundary leaves a partial token (dangling comma/colon, or an
// object key with no value) that closing alone can never make parseable. The
// incomplete trailing member is dropped and every complete member survives.
func TestBalanceBraces_MemberBoundaryTruncation(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"after comma in object", `{"a":1,`, `{"a":1}`},
		{"after string value and comma", `{"a":"x",`, `{"a":"x"}`},
		{"after colon", `{"a":`, `{}`},
		{"mid key", `{"a":"x","b`, `{"a":"x"}`},
		{"only a partial key", `{"unit`, `{}`},
		{"after comma in array", `[1,2,`, `[1,2]`},
		{"single element array comma", `[1,`, `[1]`},
		{"after nested object and comma", `{"a":{"b":1},`, `{"a":{"b":1}}`},
		{"after nested array and comma", `{"a":[1,2],`, `{"a":[1,2]}`},
		{"space before dangling colon", `{"a":1, "b":`, `{"a":1}`},
		{"complete member then truncation", `{"a":{"b":1}`, `{"a":{"b":1}}`},
		{"empty container value", `{"a":[]`, `{"a":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, n := BalanceBraces([]byte(tc.in))
			assert.Equal(t, tc.want, string(out))
			assert.True(t, json.Valid(out), "expected valid JSON, got %s", out)
			assert.Positive(t, n, "a truncation repair must be counted")
		})
	}
}

// TestBalanceBraces_CompleteBufferUntouched guards the other half of the
// contract: a buffer that is already balanced is returned byte-identically and
// reported as zero repairs, so the pipeline's clean fast path stays intact.
func TestBalanceBraces_CompleteBufferUntouched(t *testing.T) {
	in := []byte(`{"a":1,"b":[2,3],"c":{"d":true}}`)
	out, n := BalanceBraces(in)
	assert.Equal(t, 0, n)
	assert.Equal(t, in, out)
}

// TestBalanceBraces_DropsOnlyIncompleteTail makes the "no invented values"
// promise explicit: a complete member followed by a truncated one keeps the
// complete value and never fabricates the missing one.
func TestBalanceBraces_DropsOnlyIncompleteTail(t *testing.T) {
	out, n := BalanceBraces([]byte(`{"city":"Beijing","unit":`))
	assert.Positive(t, n)
	assert.JSONEq(t, `{"city":"Beijing"}`, string(out))
}
