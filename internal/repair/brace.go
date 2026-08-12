// Package repair holds the deterministic, model-agnostic primitives a RepairSpec
// composes to fix malformed tool-call JSON emitted by local CN model servers.
//
// The primitives are deliberately leaf-level: they operate on a raw byte buffer
// and never import a model spec, so the spec layer can compose them freely.
package repair

import "bytes"

// BalanceBraces closes unbalanced JSON object/array delimiters and terminates
// unterminated string literals in buf.
//
// It is a single left-to-right scan that is string-state aware (escapes
// honoured), counting opened but never-closed '{' / '[' and appending the
// matching closers in reverse order. A dangling trailing backslash inside an
// open string is dropped before the closing quote is added. The number of
// closers appended is returned as n (0 means the buffer was already balanced,
// from a brace standpoint).
//
// BalanceBraces does not itself guarantee JSON validity — a buffer with stray
// tokens or a broken escape sequence may still fail json.Unmarshal — but it
// covers the dominant local-CNN failure: a tool-call envelope truncated
// mid-value because the SSE stream ended (length / disconnect) before the model
// finished emitting the arguments JSON.
func BalanceBraces(buf []byte) (clean []byte, n int) {
	out := bytes.Clone(buf)
	var stack []byte
	inString := false
	escaped := false

	for _, c := range buf {
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
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
	}

	// Unterminated string: drop a dangling trailing backslash, then close the quote.
	if inString {
		if outLen := len(out); outLen > 0 && out[outLen-1] == '\\' {
			out = out[:outLen-1]
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
