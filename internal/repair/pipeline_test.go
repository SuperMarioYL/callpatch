package repair_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/proxy"
	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQwen3RepairTraces replays the three recorded Qwen3 failure traces through
// the full reassemble -> repair pipeline and asserts each produces the expected
// clean tool-call JSON. This is the m1 milestone acceptance test: "three
// recorded failure traces pass unit tests".
func TestQwen3RepairTraces(t *testing.T) {
	sp, ok := spec.Lookup("qwen3")
	require.True(t, ok, "qwen3 spec must be registered")
	pipe := repair.NewPipeline(sp)

	cases := []struct {
		name string
		file string
		want string
	}{
		{"fence-split + truncation", "qwen3-broken.sse", `{"city":"北京","unit":"celsius"}`},
		{"truncated_json", "qwen3-truncated.sse", `{"city":"北京","unit":"celsius"}`},
		{"unbalanced_braces nested", "qwen3-unbalanced.sse", `{"name":"search","args":{"q":"vllm tool call","limit":3}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := reassemble(t, tc.file)
			require.Len(t, calls, 1, "%s: expected one tool call", tc.file)
			clean, rpt := pipe.Repair([]byte(calls[0].Arguments))
			require.NoError(t, rpt.Err)
			assert.True(t, rpt.Repaired, "expected repair to fire")
			assert.True(t, json.Valid(clean), "repaired output must be valid JSON: %s", clean)
			assert.JSONEq(t, tc.want, string(clean))
		})
	}
}

// TestDeepSeekRepairTrace replays the DeepSeek-V4 sentinel-leak + truncation
// trace through the deepseek-v4 RepairSpec.
func TestDeepSeekRepairTrace(t *testing.T) {
	sp, ok := spec.Lookup("deepseek-v4")
	require.True(t, ok, "deepseek-v4 spec must be registered")
	pipe := repair.NewPipeline(sp)

	calls := reassemble(t, "deepseek-broken.sse")
	require.Len(t, calls, 1)
	clean, rpt := pipe.Repair([]byte(calls[0].Arguments))
	require.NoError(t, rpt.Err)
	assert.True(t, rpt.Repaired)
	assert.True(t, json.Valid(clean))
	assert.JSONEq(t, `{"query":"vllm 工具调用","top_k":3}`, string(clean))
}

// TestPipeline_Passthrough verifies a clean, already-valid envelope is returned
// unchanged (Repaired=false) — the fast path when no repair is needed.
func TestPipeline_Passthrough(t *testing.T) {
	sp, _ := spec.Lookup("qwen3")
	pipe := repair.NewPipeline(sp)
	in := []byte(`{"city":"北京","unit":"celsius"}`)
	out, rpt := pipe.Repair(in)
	assert.False(t, rpt.Repaired)
	assert.Equal(t, in, out)
}

// reassemble opens a testdata SSE fixture and returns the reassembled tool
// calls. testdata/ lives at the repo root, two levels above internal/repair.
func reassemble(t *testing.T, name string) []proxy.ToolCall {
	t.Helper()
	f, err := os.Open(testdataPath(t, name))
	require.NoError(t, err)
	defer f.Close()
	_, calls, err := proxy.Reassemble(f)
	require.NoError(t, err)
	return calls
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "testdata", name), // from internal/repair
		filepath.Join("testdata", name),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatalf("testdata fixture %q not found relative to %s", name, mustWD(t))
	return ""
}

func mustWD(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return wd
}
