#!/bin/bash
set -euo pipefail

# Go 为每个含 Objective-C 的 CGO 包追加 -lobjc（golang/go#67799）。
# 仅删除重复的这一项，保留其他参数、警告策略和 clang 退出码。
args=(clang)
objc_seen=false
for arg in "$@"; do
  if [[ "$arg" == "-lobjc" ]]; then
    if [[ "$objc_seen" == true ]]; then
      continue
    fi
    objc_seen=true
  fi
  args+=("$arg")
done
exec "${args[@]}"
