<div align="center">

<img src="web/public/logo.svg" alt="Octopus Logo" width="120" height="120">

### Octopus

**A Simple, Beautiful, and Elegant LLM API Aggregation & Load Balancing Service for Individuals**

English | [简体中文](README_zh.md) | [Getting Started](USAGE.md)

</div>

## ✨ Features

- 🔀 **Multi-Channel Aggregation** — Connect multiple LLM providers under unified management
- ⚖️ **Load Balancing** — Round Robin / Random / Failover / Weighted strategies
- 🔄 **Protocol Conversion** — Seamless conversion between OpenAI Chat / Responses / Anthropic / Gemini
- 💰 **Price Sync** — Automatic pricing from upstream sites and models.dev
- 🔃 **Model Sync** — Auto-sync available model lists from channels
- 📊 **Analytics** — Real-time SSE logs, token consumption, cost tracking
- 🏷️ **Vendor Filtering** — Auto-detected vendor filter chips for quick navigation
- 🔄 **Auto Update** — In-app one-click update with SHA-256 verification and rollback
- 🐳 **Docker Live Update** — Update inside containers without restart
- 📦 **Native Packages** — `.deb` / `.rpm` with systemd, Windows NSIS installer, macOS zip
- 🖥️ **Windows Desktop** — System tray, autostart, NSIS installer
- 🗄️ **Multi-Database** — SQLite (default), MySQL, PostgreSQL

> 📖 For detailed documentation see **[Usage Guide](USAGE.md)**

## 🚀 Quick Start

### 🐳 Docker

```bash
# F01: 容器内需绑定 0.0.0.0（否则端口映射失效），首启需设置 bootstrap 密码
docker run -d --name octopus -v octopus-data:/app/data -p 8080:8080 \
  -e OCTOPUS_SERVER_HOST=0.0.0.0 \
  -e OCTOPUS_BOOTSTRAP_PASSWORD='your-strong-password' \
  ghcr.io/seller-1990/octopus:latest
```

Or with docker compose:

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
# Manage service
systemctl start octopus
```

### 🖥️ Windows

Download **octopus-setup-*.exe** from [Releases](https://github.com/Seller-1990/octopus/releases) and install.

### 🍎 macOS

Download the `.zip` for your architecture from [Releases](https://github.com/Seller-1990/octopus/releases), extract, and run:

```bash
./octopus start
```

### 🔐 Default Credentials

Visit `http://localhost:8080` — Username: `admin`, password = the `OCTOPUS_BOOTSTRAP_PASSWORD` you set on first boot (change it after login). No fixed default password exists.

> ⚠️ **Bare-metal/desktop first boot**: you must provide a bootstrap password or startup will refuse. Set `OCTOPUS_BOOTSTRAP_PASSWORD` env before `./octopus start`, or add `bootstrap.password` to `data/config.json`.

> ⚠️ Change the default password immediately after first login.

## 🔌 Client Integration

### OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8080/v1",
    api_key="sk-octopus-your-api-key",
)
completion = client.chat.completions.create(
    model="octopus-openai",  # Use your group name
    messages=[{"role": "user", "content": "Hello"}],
)
print(completion.choices[0].message.content)
```

### Claude Code

Edit `~/.claude/settings.json`:

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

Edit `~/.codex/config.toml`:

```toml
model = "octopus-codex"
model_provider = "octopus"

[model_providers.octopus]
name = "octopus"
base_url = "http://127.0.0.1:8080/v1"
```

Edit `~/.codex/auth.json`:

```json
{
  "OPENAI_API_KEY": "sk-octopus-your-api-key"
}
```

---

## 🤝 Acknowledgments

- 🐙 [Hureru/octopus](https://github.com/Hureru/octopus) - This project is based on this upstream fork
- 🙏 [looplj/axonhub](https://github.com/looplj/axonhub) - LLM API adaptation module derived from this project
- 📊 [sst/models.dev](https://github.com/sst/models.dev) - AI model database providing pricing data

## 🔗 Friend Links

- 🐧 [LinuxDO](https://linux.do) - A community for tech enthusiasts

## 💰 Multiplier Cap — Known Behaviors

The `default_multiplier_cap` setting limits the **group (key) multiplier** only; model-level multipliers are intentionally excluded (per-token/per-request pricing varies too widely to normalize).

- **After upgrade, the cap may stay inactive for groups whose true multiplier can no longer be confirmed.** The migration conservatively marks unverifiable values as "tentative", which bypasses the cap until the upstream reports a confirmable (non-1) multiplier again. Groups that never report a real multiplier stay unblocked indefinitely (shown as "tentative").
- **"Tentative" badges have two meanings** (tooltip distinguishes them): *retained old value* (display-only; actual billing follows the site quote) vs *site never set* (billed at 1x). Tentative state persists until upstream reports a confirmable multiplier.
- **Sorting behavior changed**: entries without a confirmed multiplier now sort as 1x (previously sorted last). The card order and the route-plan order may differ (route planning still uses candidate pricing).
- **Block reasons are English** (e.g. "multiplier exceeds cap (5 > 4)") in a Chinese UI; not yet translated.

Full technical notes: see `docs/group-multiplier-policy-analysis.md`.

## 👁️ Vision Bridge — Usage Notes

When an image-bearing request is routed to a model **proven to lack vision**, a configured VLM first converts the images into text descriptions before forwarding. Text-only upstreams do not error on images — they silently degrade (measured 100%: empty content / hang / refusal); the bridge covers exactly that path.

**Activation requires all three**: the global toggle on the Vision Bridge settings page ∧ the per-key toggle on the API key card ∧ the channel model being proven non-vision (capability index; unknown models pass through untouched). Off by default — no existing key changes routing behavior.

- **Privacy**: once enabled, images hitting the fallback path are sent to the VLM endpoint you configured (`base_url`). A cloud VLM means images leave your machine — privacy-sensitive deployments should use a local VLM (e.g. LAN Ollama; `api_key` may be empty).
- **Latency**: the fallback path measures ~57s median end-to-end (VLM description stage ~38s of that) vs ~9s on native vision channels — roughly 6×. Vision-capable channels are **always routed first** (zero added latency); the VLM fallback only runs when no vision channel is available.
- **Test before use**: VLM availability varies wildly across gateways/deployments. The settings page has a Test button (probes the full model chain with a built-in image, reporting availability/latency/preview per model).
- **Billing**: VLM calls are billed by your configured endpoint; descriptions are cached (same image + same question: 15 minutes for inline images, 2 minutes for URL references).
- **Known boundaries**: WebSocket inbound requests bypass the bridge; the `/responses/compact` passthrough endpoint skips text-only channels for image inputs (best-effort detection); oversized base64 content (images etc.) in relay logs is replaced with a placeholder plus a 256KB hard cap — this log redaction applies to all requests, independent of the bridge toggles.
