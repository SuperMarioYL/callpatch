// Package repair holds the deterministic, model-agnostic primitives a RepairSpec
// composes to fix malformed tool-call JSON emitted by local CN model servers.
//
// The primitives are deliberately leaf-level: they operate on a raw byte buffer
// and never import a model spec, so the spec layer can compose them freely.
package repair

// BalanceBraces closes unbalanced JSON object/array delimiters and terminates
// unterminated string literals in buf.
//
// It is a single left-to-right scan that is string-state aware (escapes
// honoured), counting opened but never-closed '{' / '[' and appending the
// matching closers in reverse order. Inside an open string it also normalises
// the raw control characters local CN models emit inside string values: a raw
// LF or TAB is rewritten to its two-byte JSON escape and a bare CR (SSE
// framing noise) is dropped — raw control characters are illegal inside JSON
// strings, so the rewrite can never alter a valid stream. Outside strings the
// bytes pass through untouched. A dangling trailing backslash inside an open
// string (an escape the stream never completed) is dropped before the closing
// quote is added; a complete escaped backslash (even run) is kept.
//
// The number of repairs (control-char rewrites, closers, added quotes) is
// returned as n (0 means the buffer was already balanced).
//
// BalanceBraces does not itself guarantee JSON validity — a buffer with stray
// tokens or a broken escape sequence may still fail json.Unmarshal — but it
// covers the dominant local-CNN failure: a tool-call envelope truncated
// mid-value because the SSE stream ended (length / disconnect) before the model
// finished emitting the arguments JSON.
func BalanceBraces(buf []byte) (clean []byte, n int) {
	out := make([]byte, 0, len(buf)+8)
	var stack []byte
	inString := false
	escaped := false

	for _, c := range buf {
		if escaped {
			escaped = false
			out = append(out, c)
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
				out = append(out, c)
			case '"':
				inString = false
				out = append(out, c)
			case '\n':
				out = append(out, '\\', 'n')
				n++
			case '\t':
				out = append(out, '\\', 't')
				n++
			case '\r':
				// Bare CR inside a string is SSE framing noise; drop it.
				n++
			default:
				out = append(out, c)
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '{')
		case '[':
			stack = append(stack, '[')
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		case '\\':
			// A backslash outside a string is invalid JSON; mark it consumed so a
			// dangling trailing escape does not corrupt the post-loop string fixup.
			escaped = true
		}
		out = append(out, c)
	}

	// Unterminated string: drop a dangling trailing backslash — the escaped
	// flag distinguishes an escape the stream never completed (odd run) from a
	// complete escaped backslash (even run, which must survive) — then close
	// the quote.
	if inString {
		if escaped {
			out = out[:len(out)-1]
		}
		out = append(out, '"')
		n++
	}

	// Close every unclosed container, innermost first.
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i] {
		case '{':
			out = append(out, '}')
		case '[':
			out = append(out, ']')
		}
		n++
	}
	return out, n
}
