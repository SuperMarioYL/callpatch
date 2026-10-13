package repair_test

import (
	"testing"

	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPipeline_EmptyArgumentsPassthrough guards the v0.3.0 fix: an empty
// arguments buffer is valid OpenAI (zero-arg tools emit arguments:"") but
// json.Valid rejects it, so Detect fired and repair reported a false failure —
// the CLI exited 1 on a valid stream and the proxy re-synthesized it. An empty
// (or whitespace-only) buffer must pass through untouched, for every spec.
func TestPipeline_EmptyArgumentsPassthrough(t *testing.T) {
	for _, model := range []string{"qwen3", "deepseek-v4"} {
		sp, ok := spec.Lookup(model)
		require.True(t, ok, "%s spec must be registered", model)
		pipe := repair.NewPipeline(sp)

		clean, rpt := pipe.Repair(nil)
		assert.NoError(t, rpt.Err, "%s: empty arguments must not fail", model)
		assert.False(t, rpt.Repaired, "%s: nothing was repaired", model)
		assert.Empty(t, clean)

		clean, rpt = pipe.Repair([]byte("  \n"))
		assert.NoError(t, rpt.Err, "%s: whitespace-only arguments must not fail", model)
		assert.False(t, rpt.Repaired)
		assert.Equal(t, []byte("  \n"), clean)
	}
}

// TestPipeline_EmptyArgsReportShape pins the Report contract for the
// passthrough: Output mirrors Input so trace lines read honestly.
func TestPipeline_EmptyArgsReportShape(t *testing.T) {
	sp, ok := spec.Lookup("deepseek-v4")
	require.True(t, ok)
	clean, rpt := repair.NewPipeline(sp).Repair([]byte(""))
	require.NoError(t, rpt.Err)
	assert.False(t, rpt.Repaired)
	assert.Equal(t, 0, rpt.Input)
	assert.Equal(t, 0, rpt.Output)
	assert.Empty(t, clean)
}

// TestPipeline_NonEmptyInvalidStillFails pins the boundary: only buffers that
// are empty after trimming pass through; a non-empty invalid buffer still
// surfaces a repair failure.
func TestPipeline_NonEmptyInvalidStillFails(t *testing.T) {
	sp, ok := spec.Lookup("qwen3")
	require.True(t, ok)
	_, rpt := repair.NewPipeline(sp).Repair([]byte(`{"a": b`))
	assert.Error(t, rpt.Err, "a non-empty invalid buffer must still fail repair")
}
