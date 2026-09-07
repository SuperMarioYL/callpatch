#!/usr/bin/env bash
set -euo pipefail
go run ./cmd/callpatch repair --trace testdata/qwen3-broken.sse
go run ./cmd/callpatch repair --trace --model deepseek-v4 testdata/deepseek-broken.sse
