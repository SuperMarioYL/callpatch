package repair

import "strings"

// RestitchFence strips an incomplete code-fence envelope from buf, leaving the
// inner content intact.
//
// Qwen3's chat template wraps tool-call arguments in a ```json fenced block.
// Under lossy SSE streaming the closing ``` is frequently split across deltas
// or dropped entirely, so the reassembled arguments buffer arrives as
// "```json\n{...}\n``" or "```json\n{..." — not valid JSON because of the fence
// markers. RestitchFence removes the opening fence line and any trailing
// backticks (full or truncated closing fence), returning the inner text and
// ok=true when a fence was present. When no fence is present it returns buf
// unchanged with ok=false.
//
// RestitchFence is idempotent: applying it to already-fence-free output is a
// no-op, so it is safe to run unconditionally before BalanceBraces.
func RestitchFence(buf []byte) (inner []byte, ok bool) {
	s := string(buf)
	idx := strings.Index(s, "```")
	if idx < 0 {
		return buf, false
	}
	rest := s[idx:]
	// The opening fence runs to the end of its line (```json or plain ```).
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		// Single-line fence with no inner content — just strip the token.
		trimmed := strings.ReplaceAll(rest, "```", "")
		return []byte(strings.TrimSpace(trimmed)), true
	}
	innerStr := rest[nl+1:]
	// Drop a trailing closing fence, full or truncated, plus surrounding noise.
	innerStr = strings.TrimRight(innerStr, " \t\r\n")
	innerStr = strings.TrimRight(innerStr, "`")
	innerStr = strings.TrimRight(innerStr, " \t\r\n")
	return []byte(innerStr), true
}

// StripBetween removes every [open ... close] sentinel run from buf, where the
// sentinels are model-specific control tokens that leaked into the arguments
// JSON. DeepSeek-V4 emits tool-call wrappers such as
// "<｜tool▁call▁arguments▁begin｜>{...}<｜tool▁call▁end｜>"; when the local
// server's OpenAI-compatible shim fails to strip them, the arguments buffer
// carries the sentinels and is not parseable. StripBetween excises every
// "<｜...｜>" run, leaving the inner JSON for BalanceBraces to finish.
//
// n is the number of sentinel runs removed.
func StripBetween(buf []byte, open, close string) (clean []byte, n int) {
	s := string(buf)
	for {
		start := strings.Index(s, open)
		if start < 0 {
			break
		}
		tail := s[start:]
		end := strings.Index(tail, close)
		if end < 0 {
			// No closing sentinel — drop the dangling open token and stop.
			s = s[:start] + strings.TrimPrefix(tail, open)
			n++
			break
		}
		s = s[:start] + s[start+end+len(close):]
		n++
	}
	return []byte(s), n
}

// UnescapeQuotes repairs the one safe case of broken quote escapes produced
// when a streaming chunk boundary lands inside a JSON escape: a dangling
// trailing backslash (an escape sequence begun but never completed because the
// SSE stream ended before the escaped character arrived). It drops the dangling
// backslash so the following pass can cleanly terminate the string.
//
// Other broken-escape shapes (invalid \X sequences, over-escaped \\") are
// intentionally left untouched: they are ambiguous to repair deterministically
// and are surfaced by Detect for a human, or a future learned pass. n is the
// number of dangling backslashes removed (0 or 1).
func UnescapeQuotes(buf []byte) (clean []byte, n int) {
	s := string(buf)
	if strings.HasSuffix(s, "\\") {
		return []byte(s[:len(s)-1]), 1
	}
	return buf, 0
}
