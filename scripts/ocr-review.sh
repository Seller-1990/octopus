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

# 模型策略（2026-10-04，用户拍板：成本优先）：
# 主模型 deepseek-v4.1-flash @ nas-hy4（8787，0 倍率免费；实测工具调用 ✅、1M 上下文）。
# 不用 kimi-k3：两个 NAS 网关计费都贵（用户明示）；不用 wzw：渠道已全下架
# （glm-5.2-200k / gpt-4o 均 No available channel，2026-10-04 实测）；
# 不用 grok-4.7 @ x666：已不输出结构化 tool_calls（2026-10-03 实测）；
# 不用 grok-4.6：仅 CUN.ai 渠道承载（CF 1010 封 UA 且曾集体超时）；
# 不用 qwen3.8-max：近 7 天综合成功率仅 43%（K API 60% + 334 次无可用渠道）。
# 内网四棒全免费；云端五棒仅因公开仓可出境才挂在尾部。
AGENT_MODEL="$(head -1 "${HOME}/.opencodereview/agent-model" 2>/dev/null | tr -d " \r" || true)"; AGENT_MODEL="${AGENT_MODEL:-glm-5.3-flash}"
# 候选链：内网免费链在前，同族避让（代理是 glm/deepseek 时该族放备用末位），
# 云端链固定垫底（x666 0 倍率 → lucky 两 key → 呆瓜）
MODEL_CHAIN=("nas-hy4|deepseek-v4.1-flash")
case "$AGENT_MODEL" in
  *deepseek*) MODEL_CHAIN+=("nas-octopus|glm-5.3-flash" "nas-hy4|hy3" "nas-hy4|hy4-preview-f") ;;
  *glm*)      MODEL_CHAIN+=("nas-hy4|hy3" "nas-hy4|hy4-preview-f" "nas-octopus|glm-5.3-flash") ;;
  *)          MODEL_CHAIN+=("nas-octopus|glm-5.3-flash" "nas-hy4|hy3" "nas-hy4|hy4-preview-f") ;;
esac
MODEL_CHAIN+=("x666|ministral-14b-latest" "lucky-gem|gemini-3.6-flash" "lucky|stealth/space-bunny-alpha" "lucky|step-5-preview" "daigua|gpt-6-sol")

# 速率限制处理（2026-09-30，x666 有每分钟限额）：一轮评审含多次 LLM 调用，
# 限额可能只打死部分评审组（ocr 以 status 标记：complete=全部成功 /
# partial=部分组失败 / failed=全失败）。处理顺序 = 用户拍板的两条路径：
#   ① 同模型续跑：等 OCR_RATE_WAIT 秒（限额窗口重置）后 --resume 该会话，
#      已成功的评审组走缓存不重跑，只补失败的组；
#   ② 换下一个候选模型全新跑（按避让排序）。
OCR_RATE_WAIT="${OCR_RATE_WAIT:-70}"
RUN_LOG="$(mktemp "${TMPDIR:-/tmp}/ocr-review-log-XXXXXX")"

run_ocr() {  # $1=provider $2=model $3=可选 "--resume <sid>"
  # shellcheck disable=SC2086  # $3 需按空格拆成两个参数
  ocr review --from "$BASE" --to HEAD --provider "$1" --model "$2" ${3:-} --format json --output "$OUT" 2>&1 | tee -a "$RUN_LOG"
}

ocr_complete() {  # $OUT 存在且 status=complete 才算成功
  python3 - "$OUT" <<'PYI'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    sys.exit(1)
sys.exit(0 if d.get("status") == "complete" else 1)
PYI
}

session_id() {
  python3 - "$OUT" <<'PYI'
import json, sys
try:
    print(json.load(open(sys.argv[1])).get("session_id") or "")
except Exception:
    pass
PYI
}

try_model() {  # $1=provider $2=model → 返回 0 当且仅当 status=complete
  echo "== ocr review: ${BASE}..HEAD（模型 ${2} @ ${1}）=="
  : > "$RUN_LOG"
  if ! run_ocr "$1" "$2"; then
    echo "!! 模型 ${2} 本轮调用失败（退出码非 0，详见 ${RUN_LOG}）"
  fi
  if ocr_complete; then
    echo "== 模型 ${2} 评审完整完成 =="
    return 0
  fi
  local sid
  sid="$(session_id)"
  if [ -n "$sid" ]; then
    echo "!! 评审不完整（partial/failed），${OCR_RATE_WAIT}s 后同模型续跑（--resume ${sid}，已成功组走缓存）"
    sleep "$OCR_RATE_WAIT"
    run_ocr "$1" "$2" "--resume ${sid}" >/dev/null 2>&1 || true
    if ocr_complete; then
      echo "== 续跑后评审完整完成 =="
      return 0
    fi
  fi
  echo "!! 模型 ${2} 仍无法完整完成，切换下一个候选"
  return 1
}

SUCCESS=0
for entry in "${MODEL_CHAIN[@]}"; do
  PROVIDER="${entry%%|*}"; MODEL="${entry#*|}"
  if try_model "$PROVIDER" "$MODEL"; then
    SUCCESS=1
    break
  fi
done
if [ "$SUCCESS" != "1" ]; then
  echo "!! 全部 ${#MODEL_CHAIN[@]} 个候选模型都无法完整完成评审——advisory 停摆不阻断交付，但必须向主人报告此情况"
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
