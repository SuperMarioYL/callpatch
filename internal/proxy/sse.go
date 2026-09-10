// Package proxy implements the OpenAI-compatible streaming surface: an SSE
// delta reassembler (m1, used by the `callpatch repair` CLI to replay a
// recorded failure trace) and a reverse proxy (m2, forwarding /v1/chat/
// completions to a local upstream while applying the repair pipeline
// in-stream).
package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// Chunk is a minimal subset of the OpenAI chat.completion.chunk SSE payload —
// just enough to reassemble tool-call arguments across a stream. Unknown
// fields decode into the ignored struct tail, so partial / vendor-extended
// chunks from llama.cpp and vLLM parse without error.
type Chunk struct {
	ID      string `json:"id,omitempty"`
	Created int64  `json:"created,omitempty"`
	Model   string `json:"model,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

// ToolCall is the reassembled view of one tool call across an SSE stream.
type ToolCall struct {
	Index     int
	ID        string
	Type      string
	Name      string
	Arguments string
}

// ChatStream is the full reassembled view of one chat-completions SSE body:
// the tool calls (first-seen order, keyed by tool-call index — the
// single-choice shape coding agents use), plus the per-choice accumulated
// content and the final finish_reason, so the serve proxy can re-emit a
// semantically equivalent stream when a repair rewrites the arguments.
type ChatStream struct {
	ID      string
	Created int64
	Model   string
	Calls   []ToolCall
	// Content maps choice index to the concatenated content text.
	Content map[int]string
	// Finish maps choice index to the final finish_reason ("" when the
	// stream never carried one).
	Finish map[int]string
}

// Reassemble parses an OpenAI-compatible SSE stream and accumulates, per tool
// call index, the function name and the concatenated arguments fragments. It
// returns the model name (if any chunk carried one) and the tool calls in
// first-seen order. Chunks that fail to parse as JSON are skipped (lossy
// servers occasionally emit a malformed framing line), and a `data: [DONE]`
// sentinel terminates the stream.
func Reassemble(r io.Reader) (model string, calls []ToolCall, err error) {
	cs, err := ReassembleChatStream(r)
	if err != nil {
		return "", nil, err
	}
	return cs.Model, cs.Calls, nil
}

// ReassembleChatStream parses an OpenAI-compatible SSE body into the full
// ChatStream view. It shares the framing and accumulation rules of Reassemble
// (single scan, unparseable data lines skipped, `data: [DONE]` sentinel ends
// the stream) and additionally captures the stream id, the created timestamp,
// the per-choice content text and the final finish_reason per choice.
func ReassembleChatStream(r io.Reader) (*ChatStream, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	cs := &ChatStream{
		Content: map[int]string{},
		Finish:  map[int]string{},
	}
	byIndex := map[int]*ToolCall{}
	var order []int

	for sc.Scan() {
		line := sc.Bytes()
		// SSE event framing: only `data:` lines carry JSON for chat completions.
		data, ok := dataPayload(line)
		if !ok {
			continue
		}
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		var ch Chunk
		if e := json.Unmarshal(data, &ch); e != nil {
			// Skip unparseable deltas rather than aborting the whole reassembly.
			continue
		}
		if ch.Model != "" {
			cs.Model = ch.Model
		}
		if ch.ID != "" {
			cs.ID = ch.ID
		}
		if ch.Created != 0 {
			cs.Created = ch.Created
		}
		for _, c := range ch.Choices {
			if c.Delta.Content != "" {
				cs.Content[c.Index] += c.Delta.Content
			}
			if c.FinishReason != "" {
				cs.Finish[c.Index] = c.FinishReason
			}
			for _, tc := range c.Delta.ToolCalls {
				call, exists := byIndex[tc.Index]
				if !exists {
					call = &ToolCall{Index: tc.Index}
					byIndex[tc.Index] = call
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				if tc.Type != "" {
					call.Type = tc.Type
				}
				if tc.Function.Name != "" {
					call.Name = tc.Function.Name
				}
				call.Arguments += tc.Function.Arguments
			}
		}
	}
	if e := sc.Err(); e != nil {
		return nil, e
	}
	for _, i := range order {
		cs.Calls = append(cs.Calls, *byIndex[i])
	}
	return cs, nil
}

// dataPayload returns the JSON payload of an SSE `data:` line (with or without
// a leading space), and ok=false for any non-data line.
func dataPayload(line []byte) ([]byte, bool) {
	trimmed := bytes.TrimRight(line, "\r")
	switch {
	case bytes.HasPrefix(trimmed, []byte("data:")):
		return bytes.TrimSpace(trimmed[5:]), true
	case bytes.HasPrefix(trimmed, []byte("data ")):
		return bytes.TrimSpace(trimmed[5:]), true
	default:
		return nil, false
	}
}
