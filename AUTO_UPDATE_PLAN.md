# 自动更新方案审查报告

> 审查日期：2026-08-06
> 状态：已有实现，需要补强

---

## 一、现有实现（已可用）

项目已内置完整的自动更新机制，**不需要从零开发**：

| 组件 | 文件 | 状态 |
|------|------|------|
| 更新检查 API | `internal/update/update.go` | 已实现 |
| 下载+替换+重启 | `internal/update/core.go` | 已实现 |
| 容器检测（Docker/K8s 禁用自更新） | `internal/update/container.go` | 已实现 |
| HTTP API 接口 (3个) | `internal/server/handlers/update.go` | 已实现 |
| 前端 UI（设置页）| `web/src/components/modules/setting/Info.tsx` | 已实现 |
| React Query hooks | `web/src/api/endpoints/update.ts` | 已实现 |
| 版本号编译注入 | `internal/conf/version.go` + `scripts/build.sh` | 已实现 |
| CI/CD 发布 | `.github/workflows/release.yaml` | 已实现 |
| 多平台构建 | `scripts/build.sh release` | 已实现 |
| Docker 多架构镜像 | `release.yaml` → GHCR | 已实现 |

---

## 二、工作流程（已可用）

```
你本地: git tag v1.3.0 && git push --tags
                    |
                    v
GitHub Action 自动触发:
  1. pnpm build (前端)
  2. scripts/build.sh release (7 平台交叉编译)
  3. 上传 zip + md5.txt 到 GitHub Releases
  4. 构建+推送 Docker 镜像到 ghcr.io/seller-1990/octopus
                    |
                    v
部署设备 (NAS/Linux/macOS):
  前端每小时轮询 GitHub Releases API
  发现新版本 → 设置页显示 "有新版本 vX.Y.Z"
  用户点击 "更新" → 下载 zip → 解压替换二进制 → syscall.Exec 重启
                    |
                    v
Docker 部署:
  Watchtower 自动拉取新镜像并重启容器
  或用户手动 docker compose pull && up -d
```

**不需要重新编译、不需要重构、不需要手动替换文件**。

---

## 三、当前发布操作（你只需要做的）

```bash
# 1. 确保 dev 分支是最新的
git checkout dev

# 2. 打 tag
git tag v1.3.0
git push origin v1.3.0

# 3. 等 GitHub Action 完成（约 5-10 分钟）
# 4. 所有部署设备在最多 1 小时内会发现新版本
```

---

## 四、需要补强的 Gap（安全性+可靠性）

| # | Gap | 风险 | 修复方案 | 优先级 |
|---|-----|------|----------|--------|
| 1 | **无校验和验证** | 下载被篡改或不完整时会替换为损坏文件 | 下载后验证 SHA-256 checksum（release 已生成 md5.txt，升级为 sha256） | 高 |
| 2 | **无回滚机制** | 新版本启动失败后无法自动恢复 | 替换前备份旧二进制为 `.old`，启动失败时自动恢复 | 中 |
| 3 | **无服务端定期检查** | 前端不打开就永远不知道有更新 | 启动时 + 每 6 小时后台 goroutine 检查，结果存内存供 API 读取 | 中 |
| 4 | **Windows 自更新禁用** | Windows 用户只能手动下载 | 可改为：关闭服务 → 替换 → 重启服务（或保持手动，影响小） | 低 |
| 5 | **GitHub API 限流** | 未设 PAT 时 60 次/小时 | 单实例足够（只有服务端请求），可忽略 | 低 |

---

## 五、建议的改进实施（可选）

### 5.1 SHA-256 校验（优先）

```go
// core.go: UpdateCore() 中下载 zip 后
checksumURL := releaseDownloadURL("sha256sums.txt")
expected := fetchExpectedChecksum(checksumURL, filename)
actual := sha256.Sum256(zipBytes)
if actual != expected {
    return fmt.Errorf("checksum mismatch: download may be corrupted")
}
```

Release workflow 增加 `sha256sum` 生成步骤。

### 5.2 旧版本备份

```go
// core.go: 替换前
backupPath := execPath + ".old"
os.Rename(execPath, backupPath)
// 解压新版本...
// 如果解压失败:
os.Rename(backupPath, execPath)
```

### 5.3 服务端定期检查

```go
// internal/update/background.go
func StartBackgroundCheck() {
    go func() {
        check() // 启动时立即检查一次
        ticker := time.NewTicker(6 * time.Hour)
        for range ticker.C {
            check()
        }
    }()
}
```

---

## 六、Docker/NAS 用户指南

### Docker Compose + Watchtower 自动更新

```yaml
services:
  octopus:
    image: ghcr.io/seller-1990/octopus:latest
    # ... 其他配置

  watchtower:
    image: containrrr/watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - WATCHTOWER_POLL_INTERVAL=3600    # 每小时检查
      - WATCHTOWER_CLEANUP=true          # 清理旧镜像
      - WATCHTOWER_INCLUDE_STOPPED=false
    restart: unless-stopped
```

添加 Watchtower 后，每次你 push tag 触发新镜像发布，Watchtower 会在 1 小时内自动拉取新镜像并重启容器。

---

## 七、结论

**你的核心需求已经被满足**：
- 合并分支 + 打 tag → GitHub Action 自动构建发布
- 裸机部署：设置页自动检测并一键更新（下载+替换+重启）
- Docker 部署：Watchtower 自动拉取新镜像
- 无需重构、无需手动替换文件、NAS 友好

唯一需要补强的是 SHA-256 校验和旧版本备份，属于安全加固，不影响基本功能。
