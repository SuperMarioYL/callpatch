[English](README.en.md) | **简体中文**

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/hero-dark.svg">
  <img src="assets/presentation/hero-light.svg" width="1000" alt="从已保存的 SSE 轨迹重组 tool-call 参数，按模型规则修复 JSON，并输出修复轨迹。">
</picture>

**从已保存的 SSE 轨迹重组 tool-call 参数，按模型规则修复 JSON，并输出修复轨迹。**

`v0.1.0` · `Go 1.24+` · [MIT](LICENSE)

[Website](https://callpatch.lei6393.com) · [Demo record](docs/demo-results.json)

## 为什么使用

流式工具参数可能夹着围栏、sentinel，或缺失闭合字符。CallPatch 读取完整保存的 SSE，先按 tool-call 索引拼接，再运行有限的确定性修复。它让参数可以被检查，不代替调用者判断修复后的含义是否正确。

## 架构

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/architecture-dark.svg">
  <img src="assets/presentation/architecture-light.svg" width="1000" alt="internal/proxy/sse.go 重组 delta；internal/spec 根据模型选择 RepairSpec；internal/repair 处理围栏、引号、sentinel 与括号。CLI 将修复后的工具 envelope 写到 stdout，可选轨迹写到 stderr。当前 serve 子命令明确未实现。">
</picture>

internal/proxy/sse.go 重组 delta；internal/spec 根据模型选择 RepairSpec；internal/repair 处理围栏、引号、sentinel 与括号。CLI 将修复后的工具 envelope 写到 stdout，可选轨迹写到 stderr。当前 serve 子命令明确未实现。

规则注册表见 [internal/spec](internal/spec/)，修复管线见 [pipeline.go](internal/repair/pipeline.go)。已有合法 JSON 走原样返回路径。

## 安装

需要 Go 1.24+。示例用 go run 执行源码，首次构建可能下载依赖；修复本身只读取本地文件。

```bash
git clone https://github.com/SuperMarioYL/callpatch.git
cd callpatch
go build -o callpatch ./cmd/callpatch
```

## 快速开始

实际修复仓库自带的 Qwen3 与 DeepSeek SSE 示例。输出证明这两条输入经过修复后能生成参数 JSON，不证明截断前的原始语义被完整恢复，也没有运行实时代理。

```bash
go run ./cmd/callpatch repair --trace testdata/qwen3-broken.sse
go run ./cmd/callpatch repair --trace --model deepseek-v4 testdata/deepseek-broken.sse
```

输入在 [testdata](testdata/)，重放命令保存在 [examples/presentation_demo.sh](examples/presentation_demo.sh)。

## 使用

`repair [--trace] [--model NAME] FILE.sse` 是当前主要命令。--trace 为布尔开关，文件是位置参数；--model 覆盖流中的模型名。输出 function.arguments 仍是包含 JSON 的字符串，消费前需要再解析。

## 实际 Demo

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/process-dark.svg">
  <img src="assets/presentation/process-light.svg" width="1000" alt="实际修复仓库自带的 Qwen3 与 DeepSeek SSE 示例。输出证明这两条输入经过修复后能生成参数 JSON，不证明截断前的原始语义被完整恢复，也没有运行实时代理。">
</picture>

### Qwen3 围栏轨迹

从保存的 Qwen3 输入恢复可解析参数。

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

### DeepSeek sentinel

显式选择 DeepSeek 规则处理第二条输入。

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

## 能力与接入

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/integrations-dark.svg">
  <img src="assets/presentation/integrations-light.svg" width="1000" alt="CallPatch 的输入是已记录的失败轨迹，输出是待检查的参数 envelope。它与 Agent 或推理服务分工不同，不负责执行工具、补全任务或保证任意后端兼容。">
</picture>

CallPatch 的输入是已记录的失败轨迹，输出是待检查的参数 envelope。它与 Agent 或推理服务分工不同，不负责执行工具、补全任务或保证任意后端兼容。



## 配置

默认按流中模型选择规则；空模型和未知模型会回退到 qwen3，未知模型还会在 stderr 提示。没有鉴权、TLS 或运行时配置文件。--trace 展示的是模型声明的策略列表及输入/输出字节数，不代表每个策略都实际改动了字节。

## 路线图与范围

已实现离线 repair、Qwen3 与 DeepSeek 规则和示例。实时 serve、流内修复、更多模型规则仍是后续方向；不能把当前二进制当作已完成的反向代理。

- 合成或补齐的参数需要人工或调用方校验，合法 JSON 不等于正确操作。
- 当前 serve 未实现，不能据此声称 Agent 接入已跑通。
- 未知模型会回退 Qwen3 规则，需留意 stderr。

[Terminal recording](assets/demo.gif) · [Recording script](docs/demo.tape)

## 许可证

[MIT](LICENSE)
