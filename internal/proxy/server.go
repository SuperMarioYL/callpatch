package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/SuperMarioYL/callpatch/internal/repair"
)

// Server is the HTTP/SSE reverse proxy that sits between a coding agent and a
// local CN model server. Non-chat requests are forwarded verbatim. A streamed
// /v1/chat/completions response is buffered and reassembled: when a tool-call
// arguments buffer needs repair, the response is re-emitted as a clean OpenAI
// chunk sequence; when nothing needed repair, the original bytes are replayed
// verbatim so clean sessions pass through undistorted.
//
// Buffering (rather than rewriting chunks in flight) is inherent to the
// problem: a truncated envelope is only recognizable at end-of-stream, so the
// proxy must see the whole stream before it can decide what to re-emit.
type Server struct {
	Upstream   *url.URL
	ListenAddr string
	// ResolveSpec maps the model name detected in a stream to the repair Spec
	// governing it. The wiring layer (cmd/callpatch) owns the fallback policy
	// (fuzzy match, then qwen3), keeping the proxy decoupled from the spec
	// registry.
	ResolveSpec func(model string) repair.Spec
	// LogOutput receives the `repaired: ...` / `repair failed: ...` trace
	// lines. Nil disables tracing.
	LogOutput io.Writer
}

// Handler returns the http.Handler for ListenAndServe.
func (s *Server) Handler() http.Handler {
	if s.Upstream == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "callpatch: upstream not configured", http.StatusBadGateway)
		})
	}
	rp := httputil.NewSingleHostReverseProxy(s.Upstream)
	rp.ModifyResponse = s.repairChatStream
	return rp
}

// ListenAndServe runs the proxy until interrupted.
func (s *Server) ListenAndServe() error {
	if s.Upstream == nil {
		return fmt.Errorf("upstream not set")
	}
	if s.ListenAddr == "" {
		s.ListenAddr = ":8080"
	}
	return http.ListenAndServe(s.ListenAddr, s.Handler())
}

// repairChatStream is the ReverseProxy ModifyResponse hook. It intercepts only
// streamed chat-completions responses (POST + text/event-stream); everything
// else — non-streaming completions, /v1/models, vendor endpoints — passes
// through untouched.
func (s *Server) repairChatStream(resp *http.Response) error {
	req := resp.Request
	if req == nil || req.Method != http.MethodPost || req.URL.Path != "/v1/chat/completions" {
		return nil
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		// The stream could not be read to completion; replay what arrived
		// rather than fabricating a response.
		return replaceBody(resp, body)
	}
	return s.repairSSEBody(resp, body)
}

func (s *Server) repairSSEBody(resp *http.Response, body []byte) error {
	cs, err := ReassembleChatStream(bytes.NewReader(body))
	if err != nil {
		return replaceBody(resp, body)
	}
	if len(cs.Calls) == 0 || s.ResolveSpec == nil {
		return replaceBody(resp, body)
	}
	pipe := repair.NewPipeline(s.ResolveSpec(cs.Model))

	// Fast path: a stream whose tool calls are all already valid is replayed
	// byte-identically — the proxy leaves clean sessions undistorted (and
	// quiet: only repairs and failures are traced).
	anyRepair := false
	for i, tc := range cs.Calls {
		clean, rpt := pipe.Repair([]byte(tc.Arguments))
		if rpt.Repaired || rpt.Err != nil {
			anyRepair = true
			s.logTrace(rpt.TraceLine(tc.Index, tc.Name))
		}
		cs.Calls[i].Arguments = string(clean)
	}
	if !anyRepair {
		return replaceBody(resp, body)
	}
	synthesized, err := synthesize(cs)
	if err != nil {
		return replaceBody(resp, body)
	}
	return replaceBody(resp, synthesized)
}

// replaceBody swaps the (already consumed) response body for new bytes,
// keeping the framing headers consistent.
func replaceBody(resp *http.Response, body []byte) error {
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

func (s *Server) logTrace(line string) {
	if s.LogOutput != nil && line != "" {
		fmt.Fprintln(s.LogOutput, line)
	}
}

// synthChunk mirrors the OpenAI chat.completion.chunk shape the proxy
// re-emits after a repair.
type synthChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []synthChoice `json:"choices"`
}

type synthChoice struct {
	Index        int        `json:"index"`
	Delta        synthDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type synthDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []synthToolCall `json:"tool_calls,omitempty"`
}

type synthToolCall struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function synthFunction `json:"function"`
}

type synthFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// synthesize renders a clean OpenAI SSE stream from a reassembled ChatStream
// whose tool calls have already been repaired: one role delta per choice, a
// content delta when the stream carried text, one delta per tool call with the
// full repaired arguments, the finish_reason chunk, and the [DONE] sentinel.
// It assumes the single-choice shape coding agents use (calls are keyed by
// tool-call index); multi-choice tool-calling streams are replayed verbatim by
// the caller when they need no repair.
func synthesize(cs *ChatStream) ([]byte, error) {
	choices := make([]int, 0, len(cs.Finish)+len(cs.Content))
	seen := map[int]bool{}
	for idx := range cs.Finish {
		if !seen[idx] {
			seen[idx] = true
			choices = append(choices, idx)
		}
	}
	for idx := range cs.Content {
		if !seen[idx] {
			seen[idx] = true
			choices = append(choices, idx)
		}
	}
	if len(choices) == 0 {
		choices = []int{0}
	}
	sort.Ints(choices)

	var buf bytes.Buffer
	meta := func(choiceIdx int, delta synthDelta, finish *string) synthChunk {
		return synthChunk{
			ID:      cs.ID,
			Object:  "chat.completion.chunk",
			Created: cs.Created,
			Model:   cs.Model,
			Choices: []synthChoice{{Index: choiceIdx, Delta: delta, FinishReason: finish}},
		}
	}
	emit := func(c synthChunk) error {
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		buf.WriteString("data: ")
		buf.Write(b)
		buf.WriteString("\n\n")
		return nil
	}

	for _, idx := range choices {
		if err := emit(meta(idx, synthDelta{Role: "assistant"}, nil)); err != nil {
			return nil, err
		}
		if content := cs.Content[idx]; content != "" {
			if err := emit(meta(idx, synthDelta{Content: content}, nil)); err != nil {
				return nil, err
			}
		}
	}
	for _, tc := range cs.Calls {
		delta := synthDelta{ToolCalls: []synthToolCall{{
			Index:    tc.Index,
			ID:       tc.ID,
			Type:     tc.Type,
			Function: synthFunction{Name: tc.Name, Arguments: tc.Arguments},
		}}}
		if err := emit(meta(0, delta, nil)); err != nil {
			return nil, err
		}
	}
	for _, idx := range choices {
		finish := cs.Finish[idx]
		if finish == "" {
			finish = "tool_calls"
		}
		if err := emit(meta(idx, synthDelta{}, &finish)); err != nil {
			return nil, err
		}
	}
	buf.WriteString("data: [DONE]\n\n")
	return buf.Bytes(), nil
}
