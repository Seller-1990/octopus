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

# 模型策略（2026-09-30，用户拍板）：主模型 grok-4.7 @ x666（薄荷 API 入口，
# 0 倍率免费；实测工具调用 2/2、约 64s/次）。两个备用按代理当前模型避让
# 排序（同名模型不并用）——主模型失败时依次自动降级：
#   代理=glm-5.3-flash        → 备用① nas-hy4/hy4-preview-f → 备用② nas-octopus/glm-5.3-flash
#   代理=deepseek-v4.1-flash  → 备用① nas-octopus/glm-5.3-flash → 备用② nas-hy4/hy4-preview-f
# 代理当前模型写入 ~/.opencodereview/agent-model（首行生效，容忍空白/CR）。
# 不用 grok-4.6：仅 CUN.ai 渠道承载（CF 1010 封 UA 且曾集体超时）；
# 不用 qwen3.8-max：近 7 天综合成功率仅 43%（K API 60% + 334 次无可用渠道）。
AGENT_MODEL="$(head -1 "${HOME}/.opencodereview/agent-model" 2>/dev/null | tr -d " \r" || true)"; AGENT_MODEL="${AGENT_MODEL:-glm-5.3-flash}"
PRIMARY_PROVIDER="x666";        PRIMARY_MODEL="grok-4.7"
case "$AGENT_MODEL" in
  *glm*)      B1="nas-hy4|hy4-preview-f";   B2="nas-octopus|glm-5.3-flash" ;;
  *deepseek*) B1="nas-octopus|glm-5.3-flash"; B2="nas-hy4|hy4-preview-f" ;;
  *)          B1="nas-hy4|hy4-preview-f";   B2="nas-octopus|glm-5.3-flash" ;;
esac

echo "== ocr review: ${BASE}..HEAD（主模型 ${PRIMARY_MODEL} @ ${PRIMARY_PROVIDER}；备用 ${B1} → ${B2}；代理模型 ${AGENT_MODEL}）=="

run_ocr() {  # $1=provider $2=model；输出统一写 $OUT（降级时后者覆盖前者）
  ocr review --from "$BASE" --to HEAD --provider "$1" --model "$2" --format json --output "$OUT"
}

if run_ocr "$PRIMARY_PROVIDER" "$PRIMARY_MODEL"; then
  :
else
  B1_PROVIDER="${B1%%|*}"; B1_MODEL="${B1#*|}"
  echo "!! 主模型失败，降级备用①：${B1_MODEL} @ ${B1_PROVIDER}"
  if run_ocr "$B1_PROVIDER" "$B1_MODEL"; then
    :
  else
    B2_PROVIDER="${B2%%|*}"; B2_MODEL="${B2#*|}"
    echo "!! 备用①失败，降级备用②：${B2_MODEL} @ ${B2_PROVIDER}"
    if ! run_ocr "$B2_PROVIDER" "$B2_MODEL"; then
      echo "!! 三级模型全部失败（网络/网关/配置）——advisory 停摆不阻断交付，但必须向主人报告此情况"
      exit 1
    fi
  fi
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
