#!/usr/bin/env bash
# 本地语义评审（advisory，不阻断）：ocr review + 大白话摘要。
# 端点 = NAS Octopus 网关（配置在 ~/.opencodereview/，key 不进仓库/CI）。
# 依据 AGENTS.md 第一节第 6 条：合并 PR 前必须运行，并把评论整理成表格给主人。
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

BASE="${1:-origin/dev}"
# mktemp 避免可预测路径（多用户机器上他人可预建同名文件劫持结果）；
# 文件保留在临时目录供追溯，由系统定期清理临时区兜底。
OUT="$(mktemp "${TMPDIR:-/tmp}/ocr-review-XXXXXX").json"

echo "== ocr review: $BASE..HEAD（评审模型 qwen3.8-max @ NAS 网关）=="
if ! ocr review --from "$BASE" --to HEAD --format json --output "$OUT"; then
  echo "!! ocr 运行失败（网络/网关/配置问题）——advisory 停摆不阻断交付，但必须向主人报告此情况"
  exit 1
fi

echo
echo "== 结果（advisory：仅供主人参考，不与 lint/测试抢阻断权）=="
python3 - "$OUT" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except (json.JSONDecodeError, OSError) as e:
    print(f"!! 评审结果文件解析失败：{e}——请主人让 AI 直接贴 ocr 的终端输出")
    sys.exit(0)  # 解析失败不升级为阻断（advisory 定位）
if not isinstance(d, dict):
    print("!! 结果 JSON 顶层结构异常（不是对象），跳过摘要")
    sys.exit(0)
cs = d.get("comments") or []
if not cs:
    print("ocr 未发现问题（0 条评论）")
else:
    print(f"共 {len(cs)} 条评论：")
    for c in cs:
        if not isinstance(c, dict):
            continue
        sev = c.get("severity", "?")
        path = c.get("path", "?")
        line = c.get("start_line", "?")
        body = (c.get("content") or "").replace("\n", " ")[:140]
        print(f"- [{sev}] {path}:{line} — {body}")
print(f"\n原始 JSON: {sys.argv[1]}")
PY
