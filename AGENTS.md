# AGENTS.md — Octopus 交付纪律（评审门禁 · 对 AI 代理的硬约束）

本文件的读者是在本仓库工作的 **AI 编码代理**（ZCode / Claude Code / Codex 等）。
仓库主人**不会读代码**，她能做的只有三件事：核对 AI 是否贴出了可验证的命令输出、
在 GitHub PR 页看绿灯/红灯、以及在 AI 请求时决定是否同意修改"门禁文件"。
本文件的所有条款按这个能力边界设计，没有"可商量的精神"——字面执行。

## 一、每次交付前必做的自检（每条都要在回复里贴出命令与原始输出）

1. `gofmt -l .` —— 输出必须为空；不为空则先 `gofmt -w <文件>` 再重贴。
2. `go vet ./...` —— exit 0。
3. `golangci-lint run` —— exit 0。
4. `go test ./...` —— exit 0；粘贴结尾统计行（ok/FAIL 行），不得截断失败内容。
5. 若改了 `web/`：`cd web && pnpm lint && pnpm exec tsc --noEmit` —— 均 exit 0。
6. 合并 PR 前运行 `./scripts/ocr-review.sh`，并把结果整理成大白话表格：
   | # | 大白话说的是什么问题 | 位置 | 严重度 | 确定性工具是否也报了同一处？ | 建议动作 |
   处理规则：
   - 与自检 1-5 **同时出现**的问题 → 必修。
   - ocr **单独**报的问题 → 不自动大改；列给主人决策（AI 评审约有 2/3 误报率，这是官方数据）。
7. 交付说明的最后一行必须是"门禁自检"小结：列出 1-6 各项的 exit 码。
   **列不出来 = 没有完成**。禁止只说"做完了 ✅"。

## 二、绝对禁止（任何一条都视为严重违规）

1. 用 `--no-verify`、`git commit -n`、注释断言、加 `//nolint` / `eslint-disable` /
   `#nosec` / `NOSONAR` 之类豁免来让红灯变绿。
2. 修改下列**门禁文件**。任务确实需要改动时：先用中文大白话向主人解释
   "改哪个文件、为什么、影响是什么"，得到主人明确回复"同意"后方可，
   且必须知晓 CI 的 `gate-guard` 检查会亮红灯（红灯是设计，主人批准才继续合并）：
   - `.github/**`（workflows、CODEOWNERS、dependabot 等全部）
   - `.pre-commit-config.yaml`、`.golangci.yml`
   - `AGENTS.md`、`CLAUDE.md`、`Makefile`、`.gitignore`
   - `web/eslint.config.mjs`、`web/tsconfig.json`
3. 删除或弱化现有测试来让测试通过。测试只能加、只能修，不能减。
4. 提交任何密钥/token/密码到仓库，或在聊天里粘贴 key 的完整值；
   也不要把 NAS 网关 key 加进 GitHub Secrets（内网地址对 CI 无意义，见第四节）。
5. 直接 `git push` 到 `dev`。唯一流程：
   `git checkout -b <type>/<name>` → commit → push → `gh pr create` →
   等 CI 全绿 → 主人同意 → `gh pr merge`。
6. 把未跟踪的 AI 工作产物（review 报告、计划文档）提交进仓库——它们在
   `.gitignore` 里是故意的。

## 三、主人视角（不会代码的人只需要做这些）

1. AI 汇报时**没有贴原始输出和 exit 码** → 回复"重新贴检查输出"。
2. PR 页 checks 有红 → 不点 merge；让 AI 用大白话解释红在哪、为什么。
   特别地：`gate-guard` 红 = AI 在修改门禁本身，要格外谨慎。
3. AI 问"是否同意修改门禁" → 默认回答不同意，让它换不改门禁的做法。
4. ocr 的评审评论**永远只是参考、不拦门**；拦门的是测试/lint/vet 这些
   机器能给出确定答案的检查。这是有意设计，不要试图反过来。

## 四、当前门禁体系登记信息

- **本地 ocr**：`ocr` v1.12.6（npm 全局钉版）；LLM 端点 =  NAS Octopus 网关
  `http://192.168.50.139:8088/v1`，模型 `qwen3.8-max`（已验证支持 tool-calling）。
  配置与密钥存于本机 `~/.opencodereview/`，**不进仓库、不进 CI**。
  OCR 不进 CI 的原因：网关是内网地址，GitHub 的公共 runner 访问不到。
- **本地 git hook**：pre-commit 只做 gofmt 自动修复，不做阻断（防"跳过"心理）。
- **GitHub CI 阻断检查**（分支保护绑定这三个名字，禁止改名）：
  `backend` / `frontend` / `verification-bridge`（原有测试层，见 ci.yml）、
  `go-quality`（gofmt + go vet + golangci-lint）、`gate-guard`（门禁文件变更告警）。
- **CI 第三方 Action 全部钉 commit SHA**（供应链要求），升级需走 PR 并过 gate-guard。
- NAS 上的 Octopus 生产实例（:8088）同时承担两个职责：给 ZCode 供模型、
  给本地 ocr 供模型。它挂了评审就停，但**确定性门禁（CI 全层）不受影响**——
  这是"评审工具不能成为单点故障"的落实。

## 五、已知信任边界（诚实声明，2026-09-19 审查委员会结论）

1. **AI 与主人共用同一份 admin 凭据**：分支保护的 `enforce_admins: false`
   是刻意保留的主人紧急通道，但这也意味着持有 gh/git 凭据的 AI **技术上**
   具备 override merge、直推 dev、修改分支保护的能力。此类行为即使工具放行，
   也是对主人的背叛级违规，永久禁止。
2. **根治路径（待主人同意后实施）**：为 AI 的 git 操作单独签发细粒度
   GitHub PAT（仅 `contents:read/write` + `pull_requests:write`，不含
   administration 与删除保护权限），与主人浏览器登录态隔离。实施前，
   防线退化为：gate-guard 红灯 + AGENTS.md 禁令 + 主人每周抽查一次
   PR 列表和分支保护设置（`gh api repos/Seller-1990/octopus/branches/dev/protection`）。
3. **gate-guard 的检测原理限制（2026-09-19 本地 ocr 评审发现后加固）**：
   `pull_request` 事件执行的是 **PR head 分支里的 workflow 版本**——把某个
   检查"掏空成永绿"理论可行。对策是双独立检测点：`gate-guard` 与
   `go-quality` 内各自跑同一判据，绕掉单点无效，必须同时改两个 workflow
   （diff 留下"AI 在同时改两个门禁文件"的显著痕迹，主人看到即为红线）；
   防"删检查"靠 required checks 按名字绑定——删掉/改名 = 永久 pending = 无法合并。
   对"共用 admin 凭据的单人场景"，这是免费方案的上限；再往上加固 = 第 2 条。
4. **push-to-dev 事件的 lint 只覆盖最新 commit**（`--new-from-rev=HEAD~1`）：
   正常流程（一律走 PR）无影响；仅当用 admin 权限直推多个 commit 时覆盖面
   收窄，已知并接受。
5. **ocr 是开源 4 个月的新项目**：钉版本+钉 SHA 只防供应链漂移，不防评审
   质量回退。每半年对照官方 CHANGELOG 评估一次，评审信号连续两周与
   确定性工具矛盾时，降级为"仅作存档不展示"。
