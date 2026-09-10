package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepairCommand_Trace runs the real CLI against a recorded Qwen3 failure
// trace and asserts it prints the repaired tool-call JSON (stdout) plus a
// repair trace line (stderr). This is the m1 milestone's happy path:
// "callpatch repair --trace testdata/qwen3-broken.sse prints the repaired
// tool-call JSON to stdout".
func TestRepairCommand_Trace(t *testing.T) {
	root := repoRoot(t)
	sse := filepath.Join(root, "testdata", "qwen3-broken.sse")

	cmd := exec.Command("go", "run", "./cmd/callpatch", "repair", "--trace", sse)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "callpatch repair failed: %s", out)

	// The repaired tool-call JSON carries the restituted arguments.
	assert.Contains(t, string(out), "get_weather")
	assert.Contains(t, string(out), "北京")
	assert.Contains(t, string(out), "celsius")
	assert.Contains(t, string(out), `"_repaired": true`)
	// The --trace flag emits a stderr repair line.
	assert.Contains(t, string(out), "repaired:")
	assert.True(t, strings.Contains(string(out), "restitch_fence") || strings.Contains(string(out), "brace_balance"),
		"trace should name a strategy: %s", out)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// runCallpatch executes the real CLI and returns combined output plus exit err.
func runCallpatch(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "./cmd/callpatch"}, args...)...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestRepairCommand_AllTracesRepair is the v0.1 regression contract: every
// recorded failure trace still repairs cleanly with exit 0.
func TestRepairCommand_AllTracesRepair(t *testing.T) {
	for _, fixture := range []string{
		"qwen3-broken.sse", "qwen3-truncated.sse", "qwen3-unbalanced.sse", "deepseek-broken.sse",
	} {
		t.Run(fixture, func(t *testing.T) {
			out, err := runCallpatch(t, "repair", filepath.Join(repoRoot(t), "testdata", fixture))
			require.NoErrorf(t, err, "repair must succeed: %s", out)
			assert.Contains(t, out, `"_repaired": true`)
		})
	}
}

// TestRepairCommand_UnrepairableBufferNotClaimedRepaired guards the v0.2.0 fix:
// arguments truncated mid-key cannot be repaired into valid JSON, and the CLI
// must say so — "_repaired": false, a stderr warning, and a non-zero exit —
// instead of claiming a repair it cannot verify.
func TestRepairCommand_UnrepairableBufferNotClaimedRepaired(t *testing.T) {
	// Reassembled arguments: {"city":"Beijing","uni  (truncated mid-key)
	trace := "data: {\"id\":\"t\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen3\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_t\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"Beijing\\\",\\\"uni\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"
	path := filepath.Join(t.TempDir(), "midkey.sse")
	require.NoError(t, os.WriteFile(path, []byte(trace), 0o644))

	out, err := runCallpatch(t, "repair", path)
	require.Error(t, err, "an unrepairable call must fail the run")
	assert.Contains(t, out, `"_repaired": false`)
	assert.NotContains(t, out, `"_repaired": true`)
	assert.Contains(t, out, "still invalid JSON after repair")
}

// TestRepairCommand_ExtraArgsRejected guards the v0.2.0 fix: v0.1.0 silently
// ignored every trace file after the first.
func TestRepairCommand_ExtraArgsRejected(t *testing.T) {
	root := repoRoot(t)
	out, err := runCallpatch(t, "repair", filepath.Join(root, "testdata", "qwen3-broken.sse"), "second.sse")
	require.Error(t, err)
	assert.Contains(t, out, "unexpected extra arguments")
}

// TestRepairCommand_RealisticModelIDs guards the v0.2.0 fix: a stream carrying
// a real server model id ("deepseek-ai/DeepSeek-V4", not the bare spec key)
// must resolve the deepseek-v4 spec — v0.1.0 fell back to qwen3 and left the
// DeepSeek sentinel run inside the "repaired" arguments.
func TestRepairCommand_RealisticModelIDs(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "testdata", "deepseek-broken.sse"))
	require.NoError(t, err)
	renamed := bytes.ReplaceAll(raw, []byte(`"model":"deepseek-v4"`), []byte(`"model":"deepseek-ai/DeepSeek-V4"`))
	path := filepath.Join(t.TempDir(), "ds-realname.sse")
	require.NoError(t, os.WriteFile(path, renamed, 0o644))

	out, err := runCallpatch(t, "repair", path)
	require.NoErrorf(t, err, "repair must succeed: %s", out)
	assert.NotContains(t, out, "falling back to qwen3", "the realistic id must match the spec")
	assert.NotContains(t, out, "｜", "sentinel bytes must be stripped")
	// The arguments are embedded as a JSON string, so the inner quotes arrive
	// escaped — match the escaped form of {"query":"vllm 工具调用","top_k":3}.
	assert.Contains(t, out, `{\"query\":\"vllm 工具调用\",\"top_k\":3}`)
}

// TestServeCommand_MissingUpstream asserts the serve subcommand fails cleanly
// without --upstream.
func TestServeCommand_MissingUpstream(t *testing.T) {
	out, err := runCallpatch(t, "serve")
	require.Error(t, err)
	assert.Contains(t, out, "--upstream is required")
}
