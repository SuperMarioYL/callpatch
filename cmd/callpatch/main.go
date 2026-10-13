// Command callpatch is a runtime tool-call JSON repair proxy for local CN
// models (Qwen3 / DeepSeek-V4) served by llama.cpp / vLLM.
//
// `callpatch repair --trace <file.sse>` reassembles the OpenAI SSE deltas in a
// recorded failure trace, looks up the model's RepairSpec, runs the
// deterministic repair pipeline, and prints the cleaned tool-call JSON to
// stdout (with a repair trace on stderr).
//
// `callpatch serve --upstream URL --listen :8080` runs the same repair live:
// an HTTP/SSE reverse proxy in front of the local model server that repairs
// streamed tool-call envelopes in-flight.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/SuperMarioYL/callpatch/internal/proxy"
	"github.com/SuperMarioYL/callpatch/internal/repair"
	"github.com/SuperMarioYL/callpatch/internal/spec"
)

const version = "0.3.0"

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
		return serveCmd(args[1:])
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

// resolveSpec picks the RepairSpec for a model name: exact or fuzzy match
// (spec.Match, which tolerates ids like "deepseek-ai/DeepSeek-V4"), then the
// documented qwen3 fallback with a warning.
func resolveSpec(name string) spec.RepairSpec {
	if name == "" {
		name = "qwen3"
	}
	sp, ok := spec.Match(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "callpatch: no RepairSpec for model %q; falling back to qwen3\n", name)
		sp, _ = spec.Lookup("qwen3")
	}
	return sp
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
	if fs.NArg() > 1 {
		fs.Usage()
		return fmt.Errorf("unexpected extra arguments (got %d, want exactly one trace file): %s",
			fs.NArg(), fs.Arg(1))
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
	sp := resolveSpec(detected)

	pipe := repair.NewPipeline(sp)
	if len(calls) == 0 {
		fmt.Fprintln(os.Stderr, "callpatch: no tool_calls found in stream")
		return nil
	}

	// A call whose arguments are still invalid JSON after repair is emitted
	// best-effort with "_repaired": false and a stderr warning, and fails the
	// run — never claim a repair the pipeline cannot verify.
	var anyFailed bool
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for _, tc := range calls {
		clean, rpt := pipe.Repair([]byte(tc.Arguments))
		if *trace {
			fmt.Fprintln(os.Stderr, rpt.TraceLine(tc.Index, tc.Name))
		}
		if rpt.Err != nil {
			anyFailed = true
			fmt.Fprintf(os.Stderr, "callpatch: %s\n", rpt.TraceLine(tc.Index, tc.Name))
		}
		out := map[string]any{
			"index": tc.Index,
			"id":    tc.ID,
			"type":  tc.Type,
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
	if anyFailed {
		return errors.New("one or more tool calls could not be repaired (see stderr)")
	}
	return nil
}

func serveCmd(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	upstream := fs.String("upstream", "", "base URL of the local OpenAI-compatible server (required, e.g. http://127.0.0.1:8081)")
	listen := fs.String("listen", ":8080", "listen address for the proxy")
	model := fs.String("model", "", "force a RepairSpec model name (default: auto-detect from each stream; falls back to qwen3)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: callpatch serve --upstream URL [--listen :8080] [--model qwen3|deepseek-v4]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return fmt.Errorf("unexpected arguments: %s", fs.Arg(0))
	}
	if *upstream == "" {
		fs.Usage()
		return errors.New("--upstream is required (e.g. --upstream http://127.0.0.1:8081)")
	}
	u, err := url.Parse(*upstream)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid --upstream URL %q", *upstream)
	}

	forced := *model
	srv := &proxy.Server{
		Upstream:   u,
		ListenAddr: *listen,
		ResolveSpec: func(detected string) repair.Spec {
			if forced != "" {
				detected = forced
			}
			return resolveSpec(detected)
		},
		LogOutput: os.Stderr,
	}
	fmt.Fprintf(os.Stderr, "callpatch: serving on %s -> %s\n", *listen, u)
	return srv.ListenAndServe()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `callpatch — runtime tool-call JSON repair for local CN models

usage:
  callpatch repair [--trace] [--model qwen3|deepseek-v4] <file.sse>
                      reassemble an SSE failure trace and print repaired tool-call JSON
  callpatch serve --upstream URL [--listen :8080] [--model qwen3|deepseek-v4]
                      run a live HTTP/SSE proxy that repairs tool-call envelopes in-flight
  callpatch version`)
}
