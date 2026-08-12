// Command callpatch is a runtime tool-call JSON repair proxy for local CN
// models (Qwen3 / DeepSeek-V4) served by llama.cpp / vLLM. In v0.1 (m1) it
// exposes `callpatch repair --trace <file.sse>`: it reassembles the OpenAI
// SSE deltas in a recorded failure trace, looks up the model's RepairSpec,
// runs the deterministic repair pipeline, and prints the cleaned tool-call
// JSON to stdout (with a repair trace on stderr).
//
// The m2 `callpatch serve` subcommand (live HTTP/SSE proxy) is a stub.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/SuperMarioYL/callpatch/internal/proxy"
	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "callpatch:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return errors.New("no subcommand")
	}
	switch args[0] {
	case "repair":
		return repairCmd(args[1:])
	case "serve":
		// m2 milestone — not implemented in v0.1.
		fmt.Fprintln(os.Stderr, "callpatch: 'serve' is the m2 milestone; v0.1 ships 'callpatch repair --trace' only")
		return errors.New("subcommand 'serve' not available in v0.1")
	case "version", "--version", "-v":
		fmt.Printf("callpatch v%s\n", version)
		return nil
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func repairCmd(args []string) error {
	fs := flag.NewFlagSet("repair", flag.ContinueOnError)
	trace := fs.Bool("trace", false, "log the repair trace to stderr (model, strategies, in/out bytes)")
	model := fs.String("model", "", "RepairSpec model name (default: auto-detect from the SSE stream; falls back to qwen3)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: callpatch repair [--trace] [--model qwen3|deepseek-v4] <file.sse>\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return errors.New("missing SSE trace file")
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()

	detected, calls, err := proxy.Reassemble(f)
	if err != nil {
		return fmt.Errorf("reassemble %s: %w", fs.Arg(0), err)
	}
	if *model != "" {
		detected = *model
	}
	if detected == "" {
		detected = "qwen3"
	}
	sp, ok := spec.Lookup(detected)
	if !ok {
		fmt.Fprintf(os.Stderr, "callpatch: no RepairSpec for model %q; falling back to qwen3\n", detected)
		sp, _ = spec.Lookup("qwen3")
	}

	pipe := repair.NewPipeline(sp)
	if len(calls) == 0 {
		fmt.Fprintln(os.Stderr, "callpatch: no tool_calls found in stream")
		return nil
	}

	var encErr error
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for _, tc := range calls {
		clean, rpt := pipe.Repair([]byte(tc.Arguments))
		if rpt.Err != nil {
			encErr = fmt.Errorf("repair %s call[%d]: %w", sp.Model, tc.Index, rpt.Err)
			break
		}
		if *trace {
			logTrace(os.Stderr, rpt, tc)
		}
		out := map[string]any{
			"index": tc.Index,
			"id":     tc.ID,
			"type":   tc.Type,
			"function": map[string]string{
				"name":      tc.Name,
				"arguments": string(clean),
			},
			"_repaired": rpt.Repaired,
		}
		if err := enc.Encode(out); err != nil {
			return err
		}
	}
	return encErr
}

// logTrace writes a one-line-per-call repair trace to stderr in the style an
// operator scans during a live coding-agent session:
//
//	repaired: qwen3 call[0] get_weather — strategies=[restitch_fence,brace_balance] in=39 out=34
func logTrace(w io.Writer, rpt *repair.Report, tc proxy.ToolCall) {
	status := "ok"
	if !rpt.Repaired {
		status = "passthrough (already valid)"
	}
	fmt.Fprintf(w, "repaired: %s call[%d] %s — strategies=%v in=%d out=%d %s\n",
		rpt.Model, tc.Index, tc.Name, rpt.Strategies, rpt.Input, rpt.Output, status)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `callpatch — runtime tool-call JSON repair for local CN models

usage:
  callpatch repair [--trace] [--model qwen3|deepseek-v4] <file.sse>
                      reassemble an SSE failure trace and print repaired tool-call JSON
  callpatch serve --upstream URL --listen :8080   (m2; not in v0.1)
  callpatch version`)
}
