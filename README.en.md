**English** | [简体中文](README.md)

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/hero-dark.svg">
  <img src="assets/presentation/hero-light.svg" width="1000" alt="Reassemble tool-call arguments from saved SSE traces, repair JSON with model-specific rules, and inspect the repair trace.">
</picture>

**Reassemble tool-call arguments from saved SSE traces, repair JSON with model-specific rules, and inspect the repair trace.**

`v0.1.0` · `Go 1.24+` · [MIT](LICENSE)

[Website](https://callpatch.lei6393.com) · [Demo record](docs/demo-results.json)

## Why use it

Streamed tool arguments may contain fences or sentinels, or lack closing characters. CallPatch reads a saved SSE stream, reassembles calls by index, and applies bounded deterministic repairs. The result is inspectable JSON; callers still judge whether the repaired meaning is correct.

## Architecture

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/architecture-dark.svg">
  <img src="assets/presentation/architecture-light.svg" width="1000" alt="internal/proxy/sse.go reassembles deltas, internal/spec selects a RepairSpec, and internal/repair handles fences, quotes, sentinels and braces. The CLI writes repaired envelopes to stdout and optional traces to stderr. The serve subcommand is explicitly unimplemented.">
</picture>

internal/proxy/sse.go reassembles deltas, internal/spec selects a RepairSpec, and internal/repair handles fences, quotes, sentinels and braces. The CLI writes repaired envelopes to stdout and optional traces to stderr. The serve subcommand is explicitly unimplemented.

The rule registry is in [internal/spec](internal/spec/), with the pipeline in [pipeline.go](internal/repair/pipeline.go). Already valid JSON takes the unchanged-return path.

## Install

Requires Go 1.24+. The demo uses go run; initial building may fetch dependencies. The repair itself reads local files only.

```bash
git clone https://github.com/SuperMarioYL/callpatch.git
cd callpatch
go build -o callpatch ./cmd/callpatch
```

## Quickstart

The run repairs the supplied Qwen3 and DeepSeek SSE examples. It demonstrates argument JSON generation for these inputs, not exact recovery of original intent or live proxy operation.

```bash
go run ./cmd/callpatch repair --trace testdata/qwen3-broken.sse
go run ./cmd/callpatch repair --trace --model deepseek-v4 testdata/deepseek-broken.sse
```

Inputs are in [testdata](testdata/), with commands in [examples/presentation_demo.sh](examples/presentation_demo.sh).

## Usage

repair [--trace] [--model NAME] FILE.sse is the current main command. --trace is a boolean switch and the file is positional; --model overrides the stream model. function.arguments remains a JSON-containing string and needs parsing by consumers.

## Recorded demo

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/process-dark.svg">
  <img src="assets/presentation/process-light.svg" width="1000" alt="The run repairs the supplied Qwen3 and DeepSeek SSE examples. It demonstrates argument JSON generation for these inputs, not exact recovery of original intent or live proxy operation.">
</picture>

### Qwen3 fenced trace

Recover parseable arguments from the saved Qwen3 input.

```text
$ go run ./cmd/callpatch repair --trace testdata/qwen3-broken.sse
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

### DeepSeek sentinel trace

Apply the explicitly selected DeepSeek rules to the second input.

```text
$ go run ./cmd/callpatch repair --trace --model deepseek-v4 testdata/deepseek-broken.sse
{
  "_repaired": true,
  "function": {
    "arguments": "{\"query\":\"vllm 工具调用\",\"top_k\":3}",
    "name": "search_web"
  },
  "id": "call_04",
  "index": 0,
  "type": "function"
}
```

## Capabilities and integration

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/integrations-dark.svg">
  <img src="assets/presentation/integrations-light.svg" width="1000" alt="CallPatch consumes recorded failure traces and produces argument envelopes for inspection. It does not execute tools, complete an agent task or guarantee arbitrary backend compatibility.">
</picture>

CallPatch consumes recorded failure traces and produces argument envelopes for inspection. It does not execute tools, complete an agent task or guarantee arbitrary backend compatibility.



## Configuration

Rules follow the stream model by default. Empty or unknown names fall back to qwen3, with a stderr notice for unknown models. There is no authentication, TLS or runtime configuration file. --trace lists declared model strategies and input/output byte counts, not proof that every strategy changed bytes.

## Roadmap and scope

Offline repair, Qwen3/DeepSeek rules and fixtures are implemented. Live serve, in-stream repair and additional models remain future work; the current binary is not a completed reverse proxy.

- Reconstructed arguments need caller review; valid JSON does not imply a correct operation.
- serve is unimplemented, so this does not establish a working agent integration.
- Unknown models fall back to Qwen3 rules; inspect stderr.

[Terminal recording](assets/demo.gif) · [Recording script](docs/demo.tape)

## License

[MIT](LICENSE)
