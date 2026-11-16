// Package repair holds the deterministic, model-agnostic primitives a RepairSpec
// composes to fix malformed tool-call JSON emitted by local CN model servers.
//
// The primitives are deliberately leaf-level: they operate on a raw byte buffer
// and never import a model spec, so the spec layer can compose them freely.
package repair

// BalanceBraces closes unbalanced JSON object/array delimiters, terminates
// unterminated string literals and drops a truncated trailing member in buf.
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
// A stream that ends mid-member leaves a partial token no amount of closing can
// make parseable — a dangling comma or colon, or an object key that never got
// its value. Before closing, the trailing member of the innermost open container
// is dropped when it is incomplete; complete members are never touched and no
// value is ever invented.
//
// The number of repairs (control-char rewrites, dropped tokens, closers, added
// quotes) is returned as n (0 means the buffer was already balanced).
//
// BalanceBraces does not itself guarantee JSON validity — a buffer with stray
// tokens or a broken escape sequence may still fail json.Unmarshal — but it
// covers the dominant local-CNN failure: a tool-call envelope truncated
// mid-value because the SSE stream ended (length / disconnect) before the model
// finished emitting the arguments JSON.
func BalanceBraces(buf []byte) (clean []byte, n int) {
	out := make([]byte, 0, len(buf)+8)
	var stack []container
	inString := false
	escaped := false

	for _, c := range buf {
		pos := len(out)
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
		case '{', '[':
			stack = append(stack, container{kind: c, memberStart: pos + 1})
		case ',':
			// Remember where the next member begins so a truncation inside it can
			// be removed without guessing at the value.
			if len(stack) > 0 {
				stack[len(stack)-1].memberStart = pos + 1
			}
		case '}':
			if len(stack) > 0 && stack[len(stack)-1].kind == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1].kind == '[' {
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

	// A truncated trailing member cannot be closed into valid JSON; drop it.
	if len(stack) > 0 {
		out, n = dropIncompleteTail(out, stack[len(stack)-1], n)
	}

	// Close every unclosed container, innermost first.
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].kind {
		case '{':
			out = append(out, '}')
		case '[':
			out = append(out, ']')
		}
		n++
	}
	return out, n
}

// container records one open '{' or '[' during the scan: its kind and the index
// in the output where its current (last) member begins.
type container struct {
	kind        byte
	memberStart int
}

// dropIncompleteTail removes the trailing member of the innermost open container
// when truncation left it incomplete. An object member is complete only as a
// key/value pair whose value has begun; an array member is complete once any
// non-space byte follows the last comma.
func dropIncompleteTail(out []byte, f container, n int) ([]byte, int) {
	start := f.memberStart
	if start > len(out) {
		start = len(out)
	}
	end := len(out)
	for end > start && isJSONSpace(out[end-1]) {
		end--
	}
	if end == start {
		// Nothing follows the last comma: that member never began, so the
		// separator is now dangling.
		if start > 0 && out[start-1] == ',' {
			return out[:start-1], n + 1
		}
		return out, n
	}
	if f.kind == '{' {
		if colon, value := scanMember(out[start:end]); !colon || !value {
			return trimIncompleteMember(out, start, n)
		}
	}
	return out, n
}

// trimIncompleteMember cuts the trailing member (and the separator before it)
// off the output.
func trimIncompleteMember(out []byte, memberStart, n int) ([]byte, int) {
	if memberStart > 0 && out[memberStart-1] == ',' {
		return out[:memberStart-1], n + 1
	}
	return out[:memberStart], n + 1
}

// scanMember reports whether a single container member carries a top-level ':'
// and whether its value has begun — the difference between a complete
// key/value pair and a key (or partial key) cut off by truncation.
func scanMember(tail []byte) (colon, value bool) {
	depth := 0
	inString := false
	escaped := false
	for _, c := range tail {
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
			if depth == 0 && colon {
				value = true // a string starting at member level is the value
			}
			inString = true
		case '{', '[':
			if depth == 0 && colon {
				value = true // a container starting at member level is the value
			}
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				colon = true
			}
		default:
			if depth == 0 && colon && !isJSONSpace(c) {
				value = true
			}
		}
	}
	return colon, value
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
