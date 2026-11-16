package repair

import "strings"

// RestitchFence strips an incomplete code-fence envelope from buf, leaving the
// inner content intact.
//
// Qwen3's chat template wraps tool-call arguments in a ```json fenced block.
// Under lossy SSE streaming the closing ``` is frequently split across deltas
// or dropped entirely, so the reassembled arguments buffer arrives as
// "```json\n{...}\n“" or "```json\n{..." — not valid JSON because of the fence
// markers. RestitchFence removes the opening fence line and any trailing
// backticks (full or truncated closing fence), returning the inner text and
// ok=true when a fence was present. When no fence is present it returns buf
// unchanged with ok=false.
//
// A fence only counts as the envelope when it sits on a buffer boundary: a
// leading ``` (the opener) or a trailing backtick run (the closing fence, with
// its opener already stripped by the local server). A fence *inside* an
// argument's string value is data — coding agents write markdown files through
// tool calls — so it is never promoted to the whole arguments value.
//
// RestitchFence is idempotent: applying it to already-fence-free output is a
// no-op, so it is safe to run unconditionally before BalanceBraces.
func RestitchFence(buf []byte) (inner []byte, ok bool) {
	s := string(buf)
	trimmed := strings.TrimRight(s, " \t\r\n")
	trailingBackticks := len(trimmed) - len(strings.TrimRight(trimmed, "`"))
	hasOpener := strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "```")
	// A closing fence is at least "```", truncated to two backticks at the end of
	// the stream; a single backtick is more likely a value byte than a fence.
	hasCloser := trailingBackticks >= 2
	if !hasOpener && !hasCloser {
		return buf, false
	}

	if hasOpener {
		rest := strings.TrimLeft(s, " \t\r\n")
		// The opening fence runs to the end of its line (```json or plain ```).
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			// Single-line fence: the whole buffer was the marker and its body.
			rest = strings.TrimSpace(strings.ReplaceAll(rest, "```", ""))
		} else {
			rest = rest[nl+1:]
		}
		s = rest
	}
	// Drop a trailing closing fence, full or truncated, plus surrounding noise.
	s = strings.TrimRight(s, " \t\r\n")
	s = strings.TrimRight(s, "`")
	s = strings.TrimRight(s, " \t\r\n")
	if strings.TrimSpace(s) == "" {
		// The fence carried no inner content; leave the buffer for honest failure.
		return buf, false
	}
	return []byte(s), true
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
// Only an ODD trailing backslash run is dangling: a complete escaped backslash
// (an even run, e.g. a value ending in \\) is valid JSON and must survive.
// Other broken-escape shapes (invalid \X sequences, over-escaped \\") are
// intentionally left untouched: they are ambiguous to repair deterministically
// and are surfaced by Detect for a human, or a future learned pass. n is the
// number of dangling backslashes removed (0 or 1).
func UnescapeQuotes(buf []byte) (clean []byte, n int) {
	s := string(buf)
	run := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\\'; i-- {
		run++
	}
	if run%2 == 1 {
		return []byte(s[:len(s)-1]), 1
	}
	return buf, 0
}
