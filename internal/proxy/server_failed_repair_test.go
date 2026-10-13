package proxy_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The v0.3.0 failed-repair contract: when any tool call in a stream cannot be
// repaired to valid JSON, the proxy replays the upstream body verbatim instead
// of synthesizing a new stream (which would drop unparseable chunks, re-parent
// tool calls to choice 0 and fabricate a finish_reason).

// TestServe_MixedRepairabilityReplaysVerbatim pins the all-or-nothing rule: a
// stream mixing one repairable call and one unrepairable call is replayed
// byte-identically — the proxy never emits a half-repaired stream.
func TestServe_MixedRepairabilityReplaysVerbatim(t *testing.T) {
	mixed := "data: {\"id\":\"c4\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_ok\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"北京\\\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c4\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_bad\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"{\\\"a\\\": b\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c4\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	up := newUpstream(t, []byte(mixed))
	front, trace := newProxy(t, up)

	resp, body := postChatCompletion(t, front)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, mixed, body,
		"one unrepairable call must veto synthesis for the whole stream")
	assert.Contains(t, trace.String(), "repair failed:")
}

// TestServe_RepairableStreamStillSynthesized pins the complement: when every
// call repairs, synthesis still happens (the m2 acceptance path exercises a
// fixture stream; here a minimal inline repairable stream is asserted to be
// rewritten with the repaired arguments).
func TestServe_RepairableStreamStillSynthesized(t *testing.T) {
	broken := "data: {\"id\":\"c5\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_z\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"北京\\\"\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c5\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	up := newUpstream(t, []byte(broken))
	front, _ := newProxy(t, up)

	resp, body := postChatCompletion(t, front)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	cs, err := proxy.ReassembleChatStream(bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	require.Len(t, cs.Calls, 1)
	assert.JSONEq(t, `{"city":"北京"}`, cs.Calls[0].Arguments)
	assert.Contains(t, body, "data: [DONE]")
}
