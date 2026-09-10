package repair

import (
	"bytes"
	"encoding/json"
	"fmt"
)

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

// TraceLine renders the one-line-per-call repair trace shared by the CLI
// (--trace) and the serve proxy, in the style an operator scans during a live
// coding-agent session:
//
//	repaired: qwen3 call[0] get_weather — strategies=[restitch_fence,brace_balance] in=39 out=34 ok
func (r *Report) TraceLine(index int, name string) string {
	if r.Err != nil {
		return fmt.Sprintf("repair failed: %s call[%d] %s — %v", r.Model, index, name, r.Err)
	}
	status := "ok"
	if !r.Repaired {
		status = "passthrough (already valid)"
	}
	return fmt.Sprintf("repaired: %s call[%d] %s — strategies=%v in=%d out=%d %s",
		r.Model, index, name, r.Strategies, r.Input, r.Output, status)
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
// the fast path for clean tool-call envelopes that need no patching. A buffer
// whose repair does not yield valid JSON is returned best-effort with
// Repaired=false and Err set — the pipeline never claims a repair it cannot
// verify, because consumers (the CLI, the serve proxy) present that claim to a
// coding agent that will parse the arguments downstream.
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
	if !json.Valid(clean) {
		rpt.Err = fmt.Errorf("still invalid JSON after repair %v", p.S.StrategyNames())
		rpt.Output = len(clean)
		return clean, rpt
	}
	rpt.Repaired = true
	rpt.Output = len(clean)
	return clean, rpt
}
