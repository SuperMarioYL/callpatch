# Changelog

All notable changes to CallPatch are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[SemVer](https://semver.org/).

## [0.3.0] - 2026-10-14

### Fixed

- **Empty arguments no longer fail repair.** A zero-argument tool call legally
  carries `arguments: ""`, but `json.Valid` rejects the empty buffer, so the
  pipeline reported "still invalid JSON after repair", the CLI exited 1 on a
  valid stream, and the proxy re-synthesized it. Empty (or whitespace-only)
  arguments now pass through untouched.
- **Failed repairs replay the upstream stream verbatim.** When a tool call
  cannot be repaired to valid JSON, the proxy now replays the original bytes
  instead of synthesizing a new stream. Synthesis drops unparseable chunks,
  re-parents every tool call to choice 0 and can fabricate a `finish_reason`;
  an unrepairable stream must reach the client as the upstream sent it. This
  supersedes the v0.2.0 best-effort-synthesis behavior.
- **Escape parity in `BalanceBraces` / `UnescapeQuotes`.** A stream truncated
  right after a *complete* escaped backslash (even trailing run) kept its
  quote-less shape broken: the old byte-inspection fixup dropped one backslash
  of the pair, so the DeepSeek pipeline (which has no unescape step) failed the
  repair and the qwen3 pipeline mangled the value. Both primitives now use
  trailing-run parity: only a dangling (odd) escape is dropped.

### Added

- **Raw control characters inside string values are repaired.** Local CN models
  emit raw newlines/tabs inside string arguments (multiline shell commands,
  code snippets); these are illegal in JSON strings and were unrepairable.
  `BalanceBraces` is string-state aware, so it now escapes raw LF/TAB to
  `\n`/`\t` and drops bare CR framing noise while scanning — valid streams are
  untouched.
- **Release binaries.** A `release` workflow now cross-builds
  linux/darwin/windows amd64+arm64 binaries and attaches them to the GitHub
  release on every `v*` tag (the v0.2.0 release shipped with zero assets).

## [0.2.0] - 2026-09-10

### Fixed

- **Repair claims are now verified.** A tool-call arguments buffer whose repair
  still does not parse as valid JSON (e.g. truncation mid-key) is emitted with
  `"_repaired": false`, a `repair failed:` stderr line, and a non-zero exit —
  v0.1.0 claimed `"_repaired": true` and exit 0 for those buffers.
- **Realistic model IDs resolve the right RepairSpec.** Streams carrying ids
  like `deepseek-ai/DeepSeek-V4` or `Qwen/Qwen3-32B` (what llama.cpp / vLLM
  actually emit) now fuzzy-match the registry instead of silently falling back
  to the qwen3 spec, which left DeepSeek sentinel runs inside the output.
- `callpatch repair a.sse b.sse` now rejects extra positional arguments
  instead of silently ignoring every file after the first.

### Added

- **`callpatch serve`** (the m2 milestone): a live HTTP/SSE reverse proxy —
  `callpatch serve --upstream http://127.0.0.1:8081 --listen :8080` forwards
  requests to a local OpenAI-compatible server and repairs streamed
  tool-call envelopes in-flight, re-emitting clean OpenAI SSE. Streams that
  need no repair are replayed byte-identically; repairs and failures are
  traced to stderr in the `repaired: ...` format.
- CHANGELOG.md and a version-lockstep test (VERSION file, CLI `version`
  output, and `web/site.json` meta.content_version must agree).

## [0.1.0] - 2026-08-12

### Added

- Initial release: `callpatch repair --trace <file.sse>` reassembles recorded
  OpenAI-compatible SSE failure traces and repairs tool-call argument JSON.
- RepairSpec registry with the qwen3 (fence restitch, quote unescape, brace
  balance) and deepseek-v4 (sentinel strip, brace balance) rules.
- Recorded failure-trace fixtures for fence-split, truncation, unbalanced
  braces, and DeepSeek sentinel leaks (`testdata/*.sse`).
