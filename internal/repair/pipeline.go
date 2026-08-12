package repair

import "bytes"

// Spec is the repair contract a per-model RepairSpec satisfies. Defining it
// here (rather than importing the spec package) keeps the dependency one-way:
// spec imports repair to compose primitives; repair never imports spec, so the
// two can evolve independently and the test layer can wire either side.
type Spec interface {
	ModelName() string
	StrategyNames() []string
	DetectAt(buf []byte) bool
	RepairAt(buf []byte) (clean []byte, consumed int, err error)
}

// Report summarises one repair run for tracing (the --trace flag and the
// stderr lines an operator reads during a live coding-agent session).
type Report struct {
	Model      string
	Repaired   bool
	Strategies []string // declared strategies for this model
	Input      int      // input buffer length in bytes
	Output     int      // output buffer length in bytes
	Err        error
}

// Pipeline drives a model's RepairSpec against an incremental SSE delta buffer.
// In v0.1 (m1) it runs repair over a fully reassembled arguments buffer; in m2
// the same pipeline runs in-stream on each forwarded SSE chunk.
type Pipeline struct {
	S Spec
}

// NewPipeline wraps a Spec.
func NewPipeline(s Spec) *Pipeline { return &Pipeline{S: s} }

// Spec returns the underlying Spec (so callers can read its model name).
func (p *Pipeline) Spec() Spec { return p.S }

// Repair runs Detect then Repair and reports what happened. A buffer that
// already parses as valid JSON is returned untouched (Repaired=false), which is
// the fast path for clean tool-call envelopes that need no patching.
func (p *Pipeline) Repair(buf []byte) (clean []byte, rpt *Report) {
	rpt = &Report{
		Model:      p.S.ModelName(),
		Strategies: p.S.StrategyNames(),
		Input:      len(buf),
	}
	if !p.S.DetectAt(buf) {
		rpt.Output = len(buf)
		return bytes.Clone(buf), rpt
	}
	clean, _, err := p.S.RepairAt(buf)
	if err != nil {
		rpt.Err = err
		rpt.Output = len(buf)
		return bytes.Clone(buf), rpt
	}
	rpt.Repaired = true
	rpt.Output = len(clean)
	return clean, rpt
}
