**English** | [简体中文](./README.md)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/hero-light.svg">
  <img src="./assets/hero-light.svg" width="880" alt="CallPatch — runtime tool-call JSON repair for local CN models">
</picture>

<p align="center"><sub>A runtime tool-call JSON repair proxy for local CN models, so Claude Code on Qwen3/DeepSeek stops dying mid-session</sub></p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue"></a>
  <a href="https://github.com/SuperMarioYL/callpatch/releases"><img src="https://img.shields.io/github/v/release/SuperMarioYL/callpatch?label=release"></a>
  <a href="https://github.com/SuperMarioYL/callpatch/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/callpatch/ci.yml?label=ci"></a>
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white">
  <img src="https://img.shields.io/badge/Coding%20Agent-ready-5E5CE6">
  <img src="https://img.shields.io/badge/Show%20Hn-launch-FF6B35">
</p>

> **The malformed tool-call envelope in your local Qwen3/DeepSeek stream is repaired in flight — a Coding Agent pointed at a local backend stops dying silently.**

<h2><img src="https://api.iconify.design/tabler:topology-star-3.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Architecture</h2>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
  <img src="./assets/atlas-light.svg" width="880" alt="Architecture: Coding Agent → CallPatch repair proxy → local CN model">
</picture>

One process, two layers, no microservices: `internal/proxy` is a `net/http` reverse proxy for `/v1/chat/completions` (in-stream repair from m2 on); `internal/repair` + `internal/spec` is a per-model RepairSpec pipeline that runs brace/fence/sentinel repair on each request's SSE delta buffer and re-emits a clean OpenAI `tool_calls` delta stream. No DB, no auth, no TLS — one binary, cross-platform.

## Table of Contents

- [Why this exists](#why-this-exists)
- [Install](#install)
- [Quickstart](#quickstart)
- [Usage](#usage)
- [Comparison](#comparison)
- [Demo](#demo)
- [Roadmap](#roadmap)
- [License](#license)
- [Share this](#share-this)

<h2><img src="https://api.iconify.design/tabler:bulb.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Why this exists</h2>

Point Claude Code or Codex at a local Qwen3 served by llama.cpp and the session often dies mid-tool-call — the model's chat template emits a tool-call envelope whose JSON shape the OpenAI-compatible harness was never written to tolerate, and the local server's SSE streaming emulation drops or truncates fragments relative to the cloud API. CallPatch changes none of the chat template, the inference server, or the Agent: it intercepts the envelope on the SSE stream, repairs it, and re-emits a clean OpenAI `tool_calls` delta.

This is the specific moment the Coding Agent wave meets the China surface: Claude Code / Codex became a mainstream dev workflow, Qwen3 / DeepSeek-V3 reached coding-competent, tool-calling-capable quality on a local backend, and llama.cpp / vLLM shipped OpenAI-compatible `/v1/chat/completions` streaming endpoints — the intersection only became a real, breakable workflow in the last 6–12 months. And it breaks exactly at the envelope boundary: the model's tool-call JSON shape and the local server's lossy SSE streaming are not what cloud-API-born Agent harnesses were built to tolerate. A Coding Agent like [headroomlabs-ai/headroom](https://github.com/headroomlabs-ai/headroom) is exactly the Agent that hits this wall; Unsloth already bundled a "self-healing tool calls" layer for Claude-Code/Codex on local models — but locked inside its Desktop runner. CallPatch unbundles that repair as agent-agnostic standalone middleware.

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Install</h2>

Requires Go 1.24+. Set a proxy if your network is slow to reach the default module proxy:

```bash
go env -w GOPROXY=https://goproxy.cn,direct
go install github.com/SuperMarioYL/callpatch/cmd/callpatch@latest
```

Or build from source:

```bash
git clone https://github.com/SuperMarioYL/callpatch.git
cd callpatch && go build -o callpatch ./cmd/callpatch
```

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Quickstart</h2>

From a cold clone to a first visible result, in three commands or fewer:

```bash
go install github.com/SuperMarioYL/callpatch/cmd/callpatch@latest
callpatch repair --trace testdata/qwen3-broken.sse
```

<details><summary>sample output</summary>

```
repaired: qwen3 call[0] get_weather — strategies=[restitch_fence unescape_quotes brace_balance] in=41 out=34 ok
{
  "_repaired": true,
  "function": {
    "arguments": "{\"city\":\"北京\",\"unit\":\"celsius\"}",
    "name": "get_weather"
  },
  "id": "call_01",
  "index": 0,
  "type": "function"
}
```

stderr is the repair trace (`repaired: …`); stdout is the repaired tool-call JSON. A Qwen3 trace whose ` ```json ` fence and closing brace were lost at stream end is repaired in place into clean `{"city":"北京","unit":"celsius"}`.

</details>

<h2><img src="https://api.iconify.design/tabler:terminal-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Usage</h2>

v0.1 is the repair core: `callpatch repair` reads an OpenAI-compatible SSE failure trace, reassembles the tool-call deltas, looks up the model's RepairSpec, runs deterministic repair, and prints clean tool-call JSON to stdout.

```bash
# Auto-pick the RepairSpec from the SSE stream's model field (defaults to qwen3)
callpatch repair --trace testdata/qwen3-broken.sse

# Force the deepseek-v4 spec
callpatch repair --model deepseek-v4 --trace testdata/deepseek-broken.sse
```

| flag | type | default | meaning |
|---|---|---|---|
| `<file.sse>` | path | — | OpenAI-compatible SSE failure trace (required) |
| `--trace` | bool | false | log the repair trace to stderr (model, strategies, in/out bytes) |
| `--model` | string | auto | `qwen3` or `deepseek-v4`, overriding the stream's detected model |

Three recorded failure modes (see `testdata/*.sse`):

- `qwen3-broken.sse` — `fence_split_across_chunks`: the ` ```json ` fence's closing fence + closing brace were lost at stream end.
- `qwen3-truncated.sse` — `truncated_json`: the delta-assembled arguments JSON was cut mid-value.
- `qwen3-unbalanced.sse` — `unbalanced_braces`: a nested object's two closing braces were dropped.
- `deepseek-broken.sse` — `strip_sentinels` + `truncated_json`: a `<｜tool▁call▁arguments▁begin｜>` sentinel leaked into the arguments + truncation.

> From m2, `callpatch serve --upstream http://127.0.0.1:8081 --listen :8080` runs the same repair in-stream (proxy mode); v0.1 ships offline `repair` only.

<h2><img src="https://api.iconify.design/tabler:arrows-exchange-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Comparison</h2>

CallPatch is runtime repair middleware, not a whole Agent. Versus a Coding Agent the positioning is complementary, not a replacement — and we mark the row where the competitor is actually better:

| axis | CallPatch | [headroomlabs-ai/headroom](https://github.com/headroomlabs-ai/headroom) |
|---|---|---|
| runtime tool-call JSON repair | ✓ | — |
| local CN model (Qwen3/DeepSeek) tuning | ✓ | partial |
| agent-agnostic (works with any OpenAI-compatible Agent) | ✓ | — |
| out-of-the-box complete Coding Agent | — | ✓ |
| single binary, zero-dependency deploy | ✓ | partial |

<h2><img src="https://api.iconify.design/tabler:photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Demo</h2>

Two recorded failure traces (Qwen3 fence-split + DeepSeek sentinel leak) repaired in place:

![demo](assets/demo.gif)

`docs/demo.tape` is the [vhs](https://github.com/charmbracelet/vhs) script for this demo; `.github/workflows/demo.yml` re-renders `assets/demo.gif` on manual dispatch.

<h2><img src="https://api.iconify.design/tabler:map-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Roadmap</h2>

- [x] **m1 repair core** — Qwen3 RepairSpec + SSE delta reassembler + brace/fence repair; `callpatch repair --trace` fixes 3 recorded failure traces.
- [ ] **m2 proxy serve** — `:8080` HTTP/SSE reverse proxy forwarding to `--upstream`, in-stream repair, re-emitting clean OpenAI SSE; Codex → CallPatch → llama.cpp completes a tool-using task that previously died.
- [ ] **m3 DeepSeek demo** — DeepSeek RepairSpec + a dual-model repair demo + README finalised; repo reaches Show-HN-ready.
- [ ] future — community RepairSpec PRs, more models (GLM/Intern), Claude-Code-native protocol, a repair-regression corpus.

<h2><img src="https://api.iconify.design/tabler:license.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> License</h2>

MIT, see [LICENSE](./LICENSE). Issues and PRs welcome — especially ones that attach the malformed envelope you hit as a `testdata/*.sse` fixture.

## Share this

Launch surfaces: Show Hn, r/LocalLLaMA, V2EX (掘金 / 少数派 cross-posts).

```
CallPatch — runtime tool-call JSON repair for local Qwen3/DeepSeek models. Fix the malformed SSE envelope in flight so your Coding Agent stops dying mid-session on a local backend. https://github.com/SuperMarioYL/callpatch
```

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
