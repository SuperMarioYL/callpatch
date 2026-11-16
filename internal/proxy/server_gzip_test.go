package proxy_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/proxy"
	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServe_RepairsGzipStream guards the v0.4.0 fix: the proxy used to forward
// the agent's Accept-Encoding, so a gzip-encoded SSE body reached the repair
// layer as opaque bytes, reassembled to zero tool calls, and was replayed
// unrepaired with no trace. The proxy must always hand repair-readable SSE to
// the repair layer.
func TestServe_RepairsGzipStream(t *testing.T) {
	sse, err := os.ReadFile(testdataPath(t, "deepseek-broken.sse"))
	require.NoError(t, err)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, err = zw.Write(sse)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gz.Bytes())
			return
		}
		_, _ = w.Write(sse)
	}))
	t.Cleanup(up.Close)

	u, err := url.Parse(up.URL)
	require.NoError(t, err)
	trace := &bytes.Buffer{}
	srv := &proxy.Server{
		Upstream:    u,
		ListenAddr:  "127.0.0.1:0",
		ResolveSpec: func(model string) repair.Spec { s, _ := spec.Match(model); return s },
		LogOutput:   trace,
	}
	front := httptest.NewServer(srv.Handler())
	t.Cleanup(front.Close)

	req, err := http.NewRequest(http.MethodPost, front.URL+"/v1/chat/completions",
		bytes.NewBufferString(`{"model":"deepseek-v4","stream":true}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// An agent that advertises gzip: Node/undici and curl --compressed do.
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	var raw bytes.Buffer
	_, err = raw.ReadFrom(resp.Body)
	require.NoError(t, err)

	body := raw.Bytes()
	if resp.Header.Get("Content-Encoding") == "gzip" {
		// The proxy may legally re-compress as a transport detail; the agent
		// contract is about the SSE payload, not its wire encoding.
		zr, err := gzip.NewReader(bytes.NewReader(body))
		require.NoError(t, err)
		var plain bytes.Buffer
		_, err = plain.ReadFrom(zr)
		require.NoError(t, err)
		body = plain.Bytes()
	}

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, string(body), "｜", "sentinel bytes must not survive the proxy")
	assert.NotEmpty(t, trace.String(), "the repair must be traced")

	cs, err := proxy.ReassembleChatStream(bytes.NewReader(body))
	require.NoError(t, err)
	require.Len(t, cs.Calls, 1)
	assert.JSONEq(t, `{"query":"vllm 工具调用","top_k":3}`, cs.Calls[0].Arguments)
}
