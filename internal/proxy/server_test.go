package proxy_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/proxy"
	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdataPath resolves a fixture under the repo's testdata/ directory.
func testdataPath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", name)
	_, err := os.Stat(p)
	require.NoError(t, err, "fixture %s must exist", p)
	abs, err := filepath.Abs(p)
	require.NoError(t, err)
	return abs
}

// resolveViaRegistry mirrors the cmd wiring: fuzzy-match the detected model
// name against the registry.
func resolveViaRegistry(model string) repair.Spec {
	sp, _ := spec.Match(model)
	return sp
}

// newUpstream serves one recorded SSE trace on POST /v1/chat/completions and
// a canned model list on GET /v1/models.
func newUpstream(t *testing.T, sseBody []byte) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(sseBody)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	return up
}

// newProxy builds the repairing proxy in front of the given upstream.
func newProxy(t *testing.T, upstream *httptest.Server) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	u, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	trace := &bytes.Buffer{}
	srv := &proxy.Server{
		Upstream:    u,
		ListenAddr:  "127.0.0.1:0",
		ResolveSpec: resolveViaRegistry,
		LogOutput:   trace,
	}
	front := httptest.NewServer(srv.Handler())
	t.Cleanup(front.Close)
	return front, trace
}

// postChatCompletion streams a chat completion through the proxy.
func postChatCompletion(t *testing.T, front *httptest.Server) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json",
		bytes.NewBufferString(`{"model":"deepseek-v4","stream":true}`))
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	var body bytes.Buffer
	_, err = body.ReadFrom(resp.Body)
	require.NoError(t, err)
	return resp, body.String()
}

// TestServe_RepairsBrokenSSE is the m2 acceptance path: a streamed chat
// completion whose DeepSeek tool-call envelope carries an unstripped sentinel
// run and mid-number truncation comes back as a clean OpenAI SSE stream with
// repaired, valid-JSON arguments.
func TestServe_RepairsBrokenSSE(t *testing.T) {
	sse, err := os.ReadFile(testdataPath(t, "deepseek-broken.sse"))
	require.NoError(t, err)
	up := newUpstream(t, sse)
	front, trace := newProxy(t, up)

	resp, body := postChatCompletion(t, front)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	assert.NotContains(t, body, "｜", "sentinel bytes must not survive the proxy")

	cs, err := proxy.ReassembleChatStream(bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	require.Len(t, cs.Calls, 1)
	assert.JSONEq(t, `{"query":"vllm 工具调用","top_k":3}`, cs.Calls[0].Arguments)
	assert.Equal(t, "search_web", cs.Calls[0].Name)
	assert.Equal(t, "length", cs.Finish[0], "finish_reason must survive the repair")
	assert.Contains(t, body, "data: [DONE]", "stream must end with the SSE sentinel")
	assert.Contains(t, trace.String(), "repaired:", "the repair must be traced to the log")
}

// TestServe_CleanSSEPassesThroughVerbatim asserts the fast path: a stream
// whose tool calls are already valid is replayed byte-identically, so clean
// sessions are never distorted by the proxy.
func TestServe_CleanSSEPassesThroughVerbatim(t *testing.T) {
	clean := "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_x\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"北京\\\"}\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	up := newUpstream(t, []byte(clean))
	front, trace := newProxy(t, up)

	resp, body := postChatCompletion(t, front)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, clean, body, "a clean stream must be replayed byte-identically")
	assert.NotContains(t, trace.String(), "repaired:", "nothing should have been repaired")
}

// TestServe_UnrepairableCallReplaysUpstreamVerbatim guards the v0.3.0 fix
// (supersedes the v0.2.0 TestServe_UnrepairableCallEmitsBestEffort): a tool
// call that survives repair with invalid JSON must NOT be laundered into a
// synthesized stream — synthesize drops unparseable chunks, re-parents tool
// calls to choice 0 and can fabricate finish_reason. The upstream bytes are
// replayed verbatim instead.
func TestServe_UnrepairableCallReplaysUpstreamVerbatim(t *testing.T) {
	broken := "data: {\"id\":\"c2\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_y\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"{\\\"a\\\": b\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c2\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
		"data: [DONE]\n\n"
	up := newUpstream(t, []byte(broken))
	front, trace := newProxy(t, up)

	resp, body := postChatCompletion(t, front)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, broken, body, "an unrepairable stream must be replayed byte-identically")
	assert.Contains(t, trace.String(), "repair failed:", "the failure must be traced")
}

// TestServe_NonChatPathForwarded asserts /v1/models is proxied verbatim — the
// proxy only intercepts streamed chat completions.
func TestServe_NonChatPathForwarded(t *testing.T) {
	up := newUpstream(t, nil)
	front, _ := newProxy(t, up)

	resp, err := http.Get(front.URL + "/v1/models")
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	var body bytes.Buffer
	_, err = body.ReadFrom(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"object":"list","data":[{"id":"qwen3"}]}`, body.String())
}

// TestReassembleChatStream_ContentAndFinish covers the fields the synthesizer
// needs beyond tool calls: per-choice content, finish_reason, and the stream
// id/created echo.
func TestReassembleChatStream_ContentAndFinish(t *testing.T) {
	in := "data: {\"id\":\"c3\",\"created\":1737000099,\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你好\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c3\",\"created\":1737000099,\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"，世界\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"c3\",\"created\":1737000099,\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	cs, err := proxy.ReassembleChatStream(bytes.NewReader([]byte(in)))
	require.NoError(t, err)
	assert.Equal(t, "c3", cs.ID)
	assert.Equal(t, int64(1737000099), cs.Created)
	assert.Equal(t, "qwen3", cs.Model)
	assert.Equal(t, "你好，世界", cs.Content[0])
	assert.Equal(t, "stop", cs.Finish[0])
	assert.Empty(t, cs.Calls)
}
