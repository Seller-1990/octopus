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
docker run -d --name octopus -v octopus-data:/app/data -p 8080:8080 ghcr.io/seller-1990/octopus:latest
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

访问 `http://localhost:8080` — 用户名：`admin` / 密码：`admin`

> ⚠️ 请在首次登录后立即修改默认密码。

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
