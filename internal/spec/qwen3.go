package spec

import (
	"encoding/json"

	"github.com/SuperMarioYL/callpatch/internal/repair"
)

// Qwen3's chat template wraps tool-call arguments in a ```json fenced block.
// Under lossy SSE streaming the closing fence is split across deltas or
// dropped, and the inner JSON is frequently truncated when the stream ends
// (length cap / disconnect) before the model finishes emitting the arguments.
// The RepairSpec below mirrors that surface: restitch the fence, normalise
// broken quote escapes, then close unbalanced braces.

func init() {
	Register(RepairSpec{
		Model:        "qwen3",
		Envelope:     EnvelopeFenced,
		FailureModes: []FailureMode{FailureFenceSplitAcrossChunks, FailureTruncatedJSON, FailureUnbalancedBraces, FailureQuoteEscapeBreak},
		Strategies:   []RepairStrategy{StrategyRestitchFence, StrategyUnescapeQuotes, StrategyBraceBalance},
		Detect: func(buf []byte) bool {
			// Any malformed buffer fails json.Valid — fences, truncation and
			// sentinel leaks all surface here. A clean envelope is the fast path.
			return !json.Valid(buf)
		},
		Repair: qwen3Repair,
	})
}

// qwen3Repair composes the three Qwen3 strategies. Order matters: the fence
// must be restitched (so its backticks no longer shadow the JSON) before
// escapes are normalised and braces are balanced. The pipeline re-runs Detect
// to confirm validity; qwen3Repair is idempotent because each primitive is.
func qwen3Repair(buf []byte) ([]byte, int, error) {
	consumed := len(buf)
	if inner, ok := repair.RestitchFence(buf); ok {
		buf = inner
	}
	buf, _ = repair.UnescapeQuotes(buf)
	buf, _ = repair.BalanceBraces(buf)
	return buf, consumed, nil
}
