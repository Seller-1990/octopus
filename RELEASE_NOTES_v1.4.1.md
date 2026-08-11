# Octopus v1.4.1 Release Notes

> English first, 中文见下方. — v1.4.0 → v1.4.1, 4 commits.

## 🐛 Fixes

- **Site sync UNIQUE constraint failure (2067)**: `rewriteManagedGroupItemsForAccount` previously did a bare `UPDATE channel_id` per row; when the same `(group_id, model_name)` legitimately spans multiple split-route channels and rows were redirected to the same target channel that already held the tuple, the update collided with the `idx_group_channel_model` unique index — causing sync to fail with `UNIQUE constraint failed` and failed accounts to re-collide on every retry. Now: delete stale rows before moving, check target occupancy by the exact index triple `(group_id, target_channel_id, model_name)`, and wrap in a transaction covering residual state and same-batch collisions.

## ✨ Features

- **Model capability badges**: canonical models (and groups/discovered models) now carry a capability bitmap from models.dev — multimodal (image/video input), reasoning, voice (input/output), image generation, video generation. Shown as read-only badges on model catalog cards, discovery rows, and group card names. Tools capability stays instance-level (`supports_tools`), not from models.dev.
- **Makefile build wrapper**: `make build` / `make deploy` force the order frontend build → sync `static/out` → Go build, so binaries always embed the fresh frontend (prevents stale-UI after frontend changes).

## 🧹 Housekeeping

- Review reports archived to `docs/reviews/`; removed obsolete one-off docs.

---

## 🐙 v1.4.1 发布说明（中文）

> 自 v1.4.0 起，4 个提交。English above.

## 🐛 修复

- **站点同步 UNIQUE 约束冲突（2067）**：`rewriteManagedGroupItemsForAccount` 原先对 group_items 逐条裸 `UPDATE channel_id`；当同一 `(group_id, model_name)` 合法分布在多个拆分路由渠道、且条目被重定向到已持有该组合的目标渠道时，更新撞 `idx_group_channel_model` 唯一索引——导致同步报 `UNIQUE constraint failed`，失败账号每次重试都复现。现已改为：先删过期条目再搬运、按索引三元组 `(group_id, target_channel_id, model_name)` 检查目标占用、并包进事务（覆盖残留态与同批互相撞）。

## ✨ 新功能

- **模型能力徽标**：CanonicalModel（以及分组/发现模型）新增 models.dev 能力位图——多模态（图像/视频输入）、推理、语音（输入/输出）、生图、视频生成。以只读徽标展示在模型目录卡片、模型发现行、分组卡片名称后。工具调用能力保持实例级（`supports_tools`），不来自 models.dev。
- **Makefile 构建封装**：`make build` / `make deploy` 强制按序执行前端构建 → 同步 `static/out` → Go 编译，保证二进制始终内嵌最新前端（防止前端改动后 UI 陈旧）。

## 🧹 维护

- 审查报告归档至 `docs/reviews/`；清理过时的一次性文档。
