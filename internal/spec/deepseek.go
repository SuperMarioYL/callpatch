package spec

import (
	"bytes"
	"encoding/json"

	"github.com/SuperMarioYL/callpatch/internal/repair"
)

// DeepSeek-V4 emits tool calls wrapped in <｜tool▁call▁arguments▁begin｜>…
// <｜tool▁call▁end｜> sentinel runs. The local server's OpenAI-compatible shim
// sometimes fails to strip them, so the reassembled arguments buffer carries
// the sentinels (invalid JSON) and is frequently truncated. The DeepSeek
// RepairSpec strips sentinel runs, then closes unbalanced braces.

// deepSeekSentinelOpen / Close are the DeepSeek tool-call control tokens that
// leak into the arguments buffer when the local shim under-strips them.
const (
	deepSeekSentinelOpen  = "<｜"
	deepSeekSentinelClose = "｜>"
)

func init() {
	Register(RepairSpec{
		Model:        "deepseek-v4",
		Envelope:     EnvelopeDeltaAssembled,
		FailureModes: []FailureMode{FailureTruncatedJSON, FailureUnbalancedBraces, FailureQuoteEscapeBreak},
		Strategies:   []RepairStrategy{StrategyStripSentinels, StrategyBraceBalance},
		Detect: func(buf []byte) bool {
			if !json.Valid(buf) {
				return true
			}
			return bytes.Contains(buf, []byte(deepSeekSentinelOpen))
		},
		Repair: deepseekRepair,
	})
}

func deepseekRepair(buf []byte) ([]byte, int, error) {
	consumed := len(buf)
	buf, _ = repair.StripBetween(buf, deepSeekSentinelOpen, deepSeekSentinelClose)
	buf, _ = repair.BalanceBraces(buf)
	return buf, consumed, nil
}
