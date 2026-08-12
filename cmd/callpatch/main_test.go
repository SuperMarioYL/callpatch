package main

import (
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
