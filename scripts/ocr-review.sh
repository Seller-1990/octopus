#!/usr/bin/env bash
# 本地语义评审（advisory，不阻断）：ocr review + 大白话摘要。
# 端点 = NAS Octopus 网关（配置在 ~/.opencodereview/，key 不进仓库/CI）。
# 依据 AGENTS.md 第一节第 6 条：合并 PR 前必须运行，并把评论整理成表格给主人。
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

BASE="${1:-origin/dev}"
OUT="/tmp/ocr-review-$(date +%Y%m%d-%H%M%S).json"

echo "== ocr review: $BASE..HEAD（评审模型 qwen3.8-max @ NAS 网关）=="
ocr review --from "$BASE" --to HEAD --format json --output "$OUT"

echo
echo "== 结果（advisory：仅供主人参考，不与 lint/测试抢阻断权）=="
python3 - "$OUT" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
cs = d.get("comments", [])
if not cs:
    print("ocr 未发现问题（0 条评论）")
else:
    print(f"共 {len(cs)} 条评论：")
    for c in cs:
        sev = c.get("severity", "?")
        path = c.get("path", "?")
        line = c.get("start_line", "?")
        body = (c.get("content") or "").replace("\n", " ")[:140]
        print(f"- [{sev}] {path}:{line} — {body}")
print(f"\n原始 JSON: {sys.argv[1]}")
PY
