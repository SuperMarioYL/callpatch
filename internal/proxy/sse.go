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
	Model   string `json:"model,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index int `json:"index"`
				ID    string `json:"id,omitempty"`
				Type  string `json:"type,omitempty"`
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

// Reassemble parses an OpenAI-compatible SSE stream and accumulates, per tool
// call index, the function name and the concatenated arguments fragments. It
// returns the model name (if any chunk carried one) and the tool calls in
// first-seen order. Chunks that fail to parse as JSON are skipped (lossy
// servers occasionally emit a malformed framing line), and a `data: [DONE]`
// sentinel terminates the stream.
func Reassemble(r io.Reader) (model string, calls []ToolCall, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
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
			model = ch.Model
		}
		for _, c := range ch.Choices {
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
		return model, nil, e
	}
	for _, i := range order {
		calls = append(calls, *byIndex[i])
	}
	return model, calls, nil
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
