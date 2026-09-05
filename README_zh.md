<div align="center">

<img src="web/public/logo.svg" alt="Octopus Logo" width="120" height="120">

### Octopus

**为个人打造的简单、美观、优雅的 LLM API 聚合与负载均衡服务**

简体中文 | [English](README.md) | [使用指南](USAGE_zh.md)

</div>

## ✨ 特性

- 🔀 **多渠道聚合** — 接入多个 LLM 供应商渠道，统一管理
- ⚖️ **负载均衡** — 轮训 / 随机 / 故障转移 / 加权 四种策略
- 🔄 **协议互转** — OpenAI Chat / Responses / Anthropic / Gemini 格式自动转换
- 💰 **价格同步** — 自动从上游站点和 models.dev 同步模型价格
- 🔃 **模型同步** — 自动与渠道同步可用模型列表
- 📊 **数据统计** — 实时 SSE 日志流、Token 消耗、费用追踪
- 🏷️ **厂商筛选** — 自动检测模型厂商，filter chips 快速定位
- 🔄 **一键更新** — 应用内自动检测新版本，SHA-256 校验 + 回滚备份
- 🐳 **Docker 热更新** — 容器内无需重启即可更新
- 📦 **原生安装包** — Linux `.deb` / `.rpm`（含 systemd）、Windows NSIS 安装器、macOS zip
- 🖥️ **Windows 桌面版** — 系统托盘、开机自启、NSIS 安装包
- 🗄️ **多数据库** — SQLite（默认）/ MySQL / PostgreSQL

> 📖 详细文档请参阅 **[使用指南](USAGE_zh.md)**

## 🚀 快速开始

### 🐳 Docker

```bash
# F01：容器内需绑定 0.0.0.0（否则端口映射失效），首启需设置 bootstrap 密码
docker run -d --name octopus -v octopus-data:/app/data -p 8080:8080 \
  -e OCTOPUS_SERVER_HOST=0.0.0.0 \
  -e OCTOPUS_BOOTSTRAP_PASSWORD='your-strong-password' \
  ghcr.io/seller-1990/octopus:latest
```

或者使用 docker compose：

```bash
wget https://raw.githubusercontent.com/Seller-1990/octopus/dev/docker-compose.yml
docker compose up -d
```

### 📦 Linux

```bash
# Debian/Ubuntu
sudo dpkg -i octopus_*.deb
# Fedora/CentOS
sudo rpm -i octopus_*.rpm
# 管理服务
systemctl start octopus
```

### 🖥️ Windows

从 [Releases](https://github.com/Seller-1990/octopus/releases) 下载 **octopus-setup-*.exe** 安装包并安装。

### 🍎 macOS

从 [Releases](https://github.com/Seller-1990/octopus/releases) 下载对应架构的 `.zip`，解压后运行：

```bash
./octopus start
```

### 🔐 默认账户

访问 `http://localhost:8080` — 用户名：`admin`，密码 = 首启时设置的 `OCTOPUS_BOOTSTRAP_PASSWORD`（无固定默认密码，F01 安全修复）。请在首次登录后立即修改密码。

> ⚠️ **裸机/桌面首启**：需在启动前设置 bootstrap 密码，否则会拒绝启动。方式：环境变量 `OCTOPUS_BOOTSTRAP_PASSWORD`（`./octopus start` 前 export），或写入 `data/config.json` 的 `bootstrap.password` 字段。

### 安全升级注意事项

- **验证桥首次信任**：必须在扩展 popup 核对 Octopus 地址并手动提交配对令牌。网页 fragment 只能重新连接已信任的 origin。升级后请重新加载已安装的解压扩展，并删除不认识的历史配对；旧信任不会自动清空。
- **上游 TLS**：Chrome/Firefox 指纹客户端现在验证证书信任链及主机名。无效证书需要在上游修复；私有 CA 应配置到部署环境的可信证书库，不再静默接受自签名证书。
- **反向代理**：默认忽略转发来源头。使用反代时，须在 `server.trusted_proxies` 中以 JSON 数组指定实际代理地址/CIDR，或设置无空格、逗号分隔的环境变量：

  ```bash
  export OCTOPUS_SERVER_TRUSTED_PROXIES='127.0.0.1,::1'
  ```

  示例仅适用于本机代理；请按真实拓扑配置代理对端地址，不要用 `0.0.0.0/0`、`::/0` 信任所有来源。反代必须覆盖或安全追加来源头。非法地址/CIDR 会阻止 HTTP 启动。空环境变量会回退到配置文件；如要关闭代理信任，请移除环境覆盖并设置 `"trusted_proxies": []`。
- **登录预算**：每 IP 在十分钟内最多五次尝试，包含正在校验的请求；第五次准入开始十五分钟锁定，任何已准入的成功登录都会清空预算。状态为进程内存储，上限 10,000 个 IP；容量满时未知 IP 即使密码正确也会收到带 `Retry-After` 的 `429`，不会淘汰有效记录。登录流量触发全局过期回收，最多每分钟一次，空闲时不运行后台任务。未配置代理信任时，同一反代后的用户共享该代理的额度；多实例还需入口层限流。

## 🔌 客户端接入

### OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="sk-octopus-your-api-key",
)
completion = client.chat.completions.create(
    model="octopus-openai",  # 填写正确的分组名称
    messages=[{"role": "user", "content": "Hello"}],
)
print(completion.choices[0].message.content)
```

### Claude Code

编辑 `~/.claude/settings.json`：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8080",
    "ANTHROPIC_AUTH_TOKEN": "sk-octopus-your-api-key",
    "API_TIMEOUT_MS": "3000000",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "ANTHROPIC_MODEL": "octopus-sonnet-4-5",
    "ANTHROPIC_SMALL_FAST_MODEL": "octopus-haiku-4-5",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "octopus-sonnet-4-5",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "octopus-sonnet-4-5",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "octopus-haiku-4-5"
  }
}
```

### Codex

编辑 `~/.codex/config.toml`：

```toml
model = "octopus-codex"
model_provider = "octopus"

[model_providers.octopus]
name = "octopus"
base_url = "http://127.0.0.1:8080/v1"
```

编辑 `~/.codex/auth.json`：

```json
{
  "OPENAI_API_KEY": "sk-octopus-your-api-key"
}
```

---

## 🤝 鸣谢

- 🐙 [Hureru/octopus](https://github.com/Hureru/octopus) - 本项目基于此项目二开
- 🙏 [looplj/axonhub](https://github.com/looplj/axonhub) - LLM API 适配模块源自此项目
- 📊 [sst/models.dev](https://github.com/sst/models.dev) - AI 模型数据库，提供定价数据

## 🔗 友链

- 🐧 [LinuxDO](https://linux.do) - 真正的技术社区

## 💰 倍率上限 — 已知行为

`default_multiplier_cap`（默认倍率上限）**只限制分组（key）倍率**，不纳入模型倍率——模型按次/分项收费的口径差异过大，无法统一归一。

- **升级后上限可能长期不生效**：迁移保守地把无法确认的倍率标记为「暂定」，这类分组绕过上限，直到上游再次上报可确认（非 1）的倍率。永远不上报真实倍率的分组将一直不拦截（界面显示「暂定」）。
- **「暂定」徽标有两种含义**（tooltip 已区分）：*保留站点旧值*（仅展示，实际计费以站点报价为准）与 *站点从未设置*（按 1x 计费）。暂定状态保持到上游上报可确认倍率为止。
- **排序行为已变化**：无确认倍率的条目现在按 1x 参与排序（原先排末尾）。卡片顺序与路由选路顺序可能不同（路由选路仍按候选价格）。
- **阻断原因目前为英文**（如 "multiplier exceeds cap (5 > 4)"），尚未翻译。

完整技术说明见 `docs/group-multiplier-policy-analysis.md`。

## 👁️ 视觉桥（Vision Bridge）— 使用说明

含图请求被路由到**已证实无视觉能力**的纯文本模型时，先由配置的 VLM 把图片转成文字描述再转发——纯文本上游收到图片不会报错，而是静默降质（实测 100%：返回空内容/挂起/拒答），视觉桥用来兜住这条路径。

**生效条件（三者缺一不可）**：设置页「视觉桥」总开关 ∧ API 密钥卡片上的「视觉桥」开关 ∧ 通道模型被证实无视觉能力（模型能力索引；能力未知的模型保守直通，不做替换）。功能默认关闭，不改变任何现有 Key 的路由行为。

- **隐私**：开启后，命中兜底路径的图片会发送到你配置的 VLM 端点（`base_url`）。使用云端 VLM 意味着图片离开本机——对隐私敏感的部署请配置本地 VLM（如局域网 Ollama，`api_key` 可留空）。
- **延迟**：兜底链路端到端实测中位约 57s（其中 VLM 描述阶段约 38s），原生视觉通道约 9s——约 6 倍差距。视觉可用的通道**始终优先路由**（零额外延迟），只有全部视觉通道不可用时才走 VLM 兜底。
- **选型必须实测**：不同网关/部署下 VLM 可用性差异极大，设置页提供「测试」按钮（内置测试图真调完整模型链，逐模型返回可用性/延迟/描述预览），配置后先测再用。
- **计费**：VLM 调用按你配置的端点正常计费；描述结果有缓存（同图同问复用：内嵌图片 15 分钟、URL 引用图 2 分钟）。
- **已知边界**：WebSocket 入站请求不经过视觉桥；`/responses/compact` 直通入口对含图输入跳过纯文本通道（best-effort 检测）；relay 日志对超长 base64 内容（图片等）只保留占位符并施加 256KB 硬上限——该脱敏对所有请求生效，与视觉桥开关无关。
