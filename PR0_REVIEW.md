# PR-0 Code Review

> 审阅日期：2026-08-06  
> 分支：`feature/slim-pr0` → `dev` (PR #2, merged at `2b66078`)  
> 审阅范围：`37178b8..2b66078`，63 files changed, +3458 / -750

---

## 一、设计方向：正确且有价值

| 主题 | 评价 |
|------|------|
| **Credential Revision CAS** | 核心设计：所有凭据写入使用 `WHERE credential_revision = ?` 乐观锁，杜绝并发 token refresh 互踩。设计完整、测试充分。 |
| **Cookie 凭据类型隔离** | 新增 `SiteCredentialTypeCookie`，session cookie 加密后存入 `session_cookie_encrypted`，access_token 列不再混存 cookie。向前兼容处理旧数据。 |
| **Sub2API 定时 refresh** | 新增 `sub2api_schedule.go`，带 spread window 打散、worker pool 并发限制、failure class 记录。架构干净。 |
| **请求重试** | `requestJSON` / `anyRouterRequestJSONWithCookies` 对 GET 加入有限重试（仅 5xx/429，尊重 Retry-After），POST 不重试。策略合理。 |
| **死代码清理** | 删除 `mergeBetaHeader`、`collectOpenAIResponsesPassthroughMetrics`、`streamReachedTerminalEvent`、`injectWSPreviousResponseID`、`ClashSwitchLease`、`xslice` 包、`GetRouterCount`、`sanitizeCacheControlPair` 等。收窄攻击面，减少维护负担。 |
| **备份脱敏** | `sanitizeSiteAccountsForBackup` 清除遗留明文 cookie；import 时重置 `SessionCookieEncrypted`。 |

---

## 二、确认通过的要点（无 bug）

- CAS 失败后 `reloadSub2APICredentials` 从 DB 重新加载，调用方拿到最新 token——逻辑闭环。
- `coordinateSub2APIRefresh` 的 singleflight 模式：leader 执行后 close(done)，joiner 用加载的最新 DB 凭据。
- `VerificationSession` 携带 `CredentialRevision`，completion 时用 `WHERE credential_revision = ?` 保护，revision 变更后自动 supersede。
- `updateAccountCheckinState` 和 `persistSyncSnapshot` 统一走 transaction + CAS，snapshot 整体回滚不会留下 partial group/token/model 数据。
- 前端 `AccountEditDialog` 在 cookie 型凭据未变更时不发送 `access_token` 字段（`preservesStoredSessionCookie`），避免覆盖服务端加密值。
- `anyRouterRequestJSONWithCookieScope` 用 `cookiejar.New` + `publicsuffix.List` 正确隔离 cookie scope。
- Relay 层 `MaxEventSize` 配置统一传入 stream processor，消除硬编码不一致。
- `outlierwindow.Report` 增加 `reportEnabled` atomic 开关，POR 未启用时跳过热路径写入。
- `helper/fetch.go` 修复被忽略的 `NewRequestWithContext` 错误返回。

---

## 三、发现的问题与建议

### 3.1 [Low] `sub2APIRefreshCalls` 缺少 panic-safe defer

**文件**：`internal/sitesync/sub2api_auth.go:95-103`

```go
call.result = refresh()           // 如果这里 panic...
sub2APIRefreshCalls.Lock()
delete(sub2APIRefreshCalls.calls, key)
close(call.done)                  // ...这里永远不会执行
sub2APIRefreshCalls.Unlock()
```

如果 `refresh()` panic，`call.done` 永远不会被 close，所有 joiner 会永久阻塞。

**建议修复**：
```go
defer func() {
    sub2APIRefreshCalls.Lock()
    delete(sub2APIRefreshCalls.calls, key)
    close(call.done)
    sub2APIRefreshCalls.Unlock()
}()
call.result = refresh()
```

**影响**：Low——`refresh()` 内部不会 panic（无 index/nil 操作），但作为防御性编码建议补上。

---

### 3.2 [Low] `sub2APIRefreshDueAt` spread 哈希对非 2^n 窗口分布不严格均匀

**文件**：`internal/sitesync/sub2api_auth.go:168`

```go
offsetMillis := int64((uint64(account.ID) * 11400714819323198485) % uint64(spreadMillis))
```

Fibonacci hash 在 modulus = 120000 (2min) 时分布不是完美均匀。测试已验证 100 个连续 ID 可分散到 4+ bucket，实际效果足够好。如果后续 ID 密集连续且需要更严格分散，可改为 `crc32(id) % spreadMillis`。

**影响**：Low——当前行为可接受，无需立即修改。

---

### 3.3 [Info] `IsSiteCookieCredential` 的 known-name 列表有限

**文件**：`internal/model/site.go:298-301`

单 pair cookie（如 `PHPSESSID=xxx`）不在已知列表中时不会被识别为 cookie。但只有 `CredentialRevision == 0` 的遗留数据才走此判断（新创建的会有明确 `CredentialType`），影响有限。

**建议**：后续如遇到遗留数据误判，再扩展列表即可。

---

### 3.4 [Info] `SLIM_PLAN.md` 和 `SLIM_PR0_AUDIT.md` 存在于源码目录

这两个文件是规划和审计记录（共 ~490 行），不是运行时依赖。如果不希望长期保留在 repo 中，可在后续 PR 移到 wiki 或 `.docs/` 目录。

---

## 四、验证结果

| 检查项 | 结果 |
|--------|------|
| `go test ./internal/...` | **全部通过** (18 packages, 0 failures) |
| `go vet ./...` | **无警告** |
| `pnpm lint` (web/) | **无报错** |

---

## 五、结论

**变更质量很高**。核心逻辑正确，测试覆盖充分（新增 ~1500 行测试），所有测试和静态检查通过。

唯一建议修复的是 3.1 中 `coordinateSub2APIRefresh` 缺少 panic-safe 的 defer，属于防御性编码，优先级 Low——当前不影响正常运行，可以在后续 PR 补上。

---

**Reviewer**: Claude (AI-assisted review)  
**Verdict**: ✅ Approved
