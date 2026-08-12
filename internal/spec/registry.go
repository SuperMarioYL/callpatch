// Package spec defines the RepairSpec — the per-model declarative rule that
// describes a local CN model's tool-call envelope shape, its known
// lossy-streaming failure modes, and the deterministic repair strategies the
// pipeline applies to an incremental SSE delta buffer.
//
// The registry is populated at init time by the per-model files (qwen3.go,
// deepseek.go). spec imports the repair package to compose primitives in the
// model Repair closures; repair never imports spec (it consumes a Spec
// interface), so the layering is strictly one-way.
package spec

// EnvelopeShape describes how a model's tool-call JSON is carried in the SSE
// stream produced by a local OpenAI-compatible server.
type EnvelopeShape string

const (
	// EnvelopeFenced: the model's chat template wraps tool-call arguments in a
	// ```json fenced block (e.g. Qwen3); lossy streaming splits the fence.
	EnvelopeFenced EnvelopeShape = "fenced"
	// EnvelopeDeltaAssembled: tool-call arguments arrive as incremental
	// delta fragments assembled by the client (e.g. DeepSeek-V4).
	EnvelopeDeltaAssembled EnvelopeShape = "delta_assembled"
)

// FailureMode is a known lossy-streaming failure a model exhibits.
type FailureMode string

const (
	FailureTruncatedJSON          FailureMode = "truncated_json"
	FailureUnbalancedBraces       FailureMode = "unbalanced_braces"
	FailureFenceSplitAcrossChunks FailureMode = "fence_split_across_chunks"
	FailureQuoteEscapeBreak       FailureMode = "quote_escape_break"
)

// RepairStrategy names a deterministic repair primitive.
type RepairStrategy string

const (
	StrategyRestitchFence  RepairStrategy = "restitch_fence"
	StrategyBraceBalance   RepairStrategy = "brace_balance"
	StrategyUnescapeQuotes RepairStrategy = "unescape_quotes"
	StrategyStripSentinels RepairStrategy = "strip_sentinels"
)

// RepairSpec is the core primitive of CallPatch: a per-model declarative rule
// describing (a) the model's tool-call envelope shape, (b) its known
// lossy-streaming failure modes, and (c) the Detect/Repair closures applied to
// an incremental SSE delta buffer. Detect decides whether a buffer needs
// repair; Repair returns the cleaned buffer (and the bytes consumed, for
// streaming pipelining in m2).
type RepairSpec struct {
	Model        string
	Envelope     EnvelopeShape
	FailureModes []FailureMode
	Strategies   []RepairStrategy
	Detect       func(buf []byte) bool
	Repair       func(buf []byte) (clean []byte, consumed int, err error)
}

// ModelName implements repair.Spec.
func (s RepairSpec) ModelName() string { return s.Model }

// EnvelopeName returns the envelope shape as a plain string (for tracing).
func (s RepairSpec) EnvelopeName() string { return string(s.Envelope) }

// StrategyNames implements repair.Spec — the declared strategy names. (Method
// named StrategyNames, not Strategies, to avoid clashing with the field.)
func (s RepairSpec) StrategyNames() []string {
	out := make([]string, len(s.Strategies))
	for i, st := range s.Strategies {
		out[i] = string(st)
	}
	return out
}

// DetectAt implements repair.Spec, delegating to the model's Detect closure.
func (s RepairSpec) DetectAt(buf []byte) bool {
	if s.Detect != nil {
		return s.Detect(buf)
	}
	return false
}

// RepairAt implements repair.Spec, delegating to the model's Repair closure.
func (s RepairSpec) RepairAt(buf []byte) ([]byte, int, error) {
	if s.Repair != nil {
		return s.Repair(buf)
	}
	return buf, 0, nil
}

var registry = map[string]RepairSpec{}

// Register adds (or replaces) a RepairSpec by model name. Called from init in
// the per-model files.
func Register(s RepairSpec) { registry[s.Model] = s }

// Lookup returns the RepairSpec for a model name and whether one is registered.
func Lookup(model string) (RepairSpec, bool) {
	s, ok := registry[model]
	return s, ok
}

// Models returns the registered model names in sorted order.
func Models() []string {
	out := make([]string, 0, len(registry))
	for m := range registry {
		out = append(out, m)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
