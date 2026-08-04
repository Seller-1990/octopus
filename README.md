<div align="center">

<img src="web/public/logo.svg" alt="Octopus Logo" width="120" height="120">

### Octopus

**A Simple, Beautiful, and Elegant LLM API Aggregation & Load Balancing Service for Individuals**

 English | [简体中文](README_zh.md) | [Getting Started](USAGE.md)

</div>

> This is an independently maintained fork of [Hureru/octopus](https://github.com/Hureru/octopus), whose Go module remains compatible with [bestruirui/octopus](https://github.com/bestruirui/octopus). Releases, images, update checks, and deployment files come only from [Seller-1990/octopus](https://github.com/Seller-1990/octopus).
>
> The intended workflow imports site data from `all-api-hub`, which remains responsible for check-in. Octopus retains compatible check-in controls, but this fork does not use them as its primary workflow; avoid enabling check-in in both systems.


## ✨ Features

- 🔀 **Multi-Channel Aggregation** - Connect multiple LLM provider channels with unified management
- 🔑 **Multi-Key Support** - Support multiple API keys for a single channel
- ⚡ **Smart Selection** - Multiple endpoints per channel, smart selection of the endpoint with the shortest delay
- ⚖️ **Load Balancing** - Automatic request distribution for stable and efficient service
- 🔄 **Protocol Conversion** - Seamless conversion between OpenAI Chat / OpenAI Responses / Anthropic API formats
- 💰 **Price Sync** - Automatic model pricing updates
- 🔃 **Model Sync** - Automatic synchronization of available model lists with channels
- 📊 **Analytics** - Comprehensive request statistics, token consumption, and cost tracking
- 🎨 **Elegant UI** - Clean and beautiful web management panel
- 🗄️ **Multi-Database Support** - Support for SQLite, MySQL, PostgreSQL

> 📖 **First time using Octopus?** Check out the **[Getting Started Guide](USAGE.md)** for a complete walkthrough from deployment to client integration — get up and running in 5 minutes.


## 🚀 Quick Start

### 🐳 Docker

Run directly:

```bash
docker run -d --name octopus -v /path/to/data:/app/data -p 8080:8080 ghcr.io/seller-1990/octopus:v0.1.0
```

Or use docker compose:

```bash
wget https://raw.githubusercontent.com/Seller-1990/octopus/refs/tags/v0.1.0/docker-compose.yml
echo 'OCTOPUS_IMAGE_TAG=v0.1.0' > .env
docker compose up -d
```


Use only **ghcr.io/seller-1990/octopus** and the compose file from this repository. Replace `v0.1.0` with the release you intend to deploy; use `latest` only when you explicitly want a moving version. Parent-fork images and deployment files do not contain the complete feature set.

### 📦 Download from Release

Download the binary for your platform from [Releases](https://github.com/Seller-1990/octopus/releases), then run:

```bash
./octopus start
```

Windows users can download **octopus-setup-version-x86_64.exe** for an installer with desktop and Start Menu shortcuts, or **octopus-desktop-x86_64.exe** for the portable desktop executable.

### 🛠️ Build from Source

**Requirements:**
- Go 1.25.0
- Node.js 22
- pnpm 10

```bash
# Clone the repository
git clone https://github.com/Seller-1990/octopus.git
cd octopus
# Build frontend
cd web && pnpm install && pnpm run build && cd ..
# Move frontend assets to static directory
mv web/out static/
# Start the backend service
go run main.go start 
```

> 💡 **Tip**: The frontend build artifacts are embedded into the Go binary, so you must build the frontend before starting the backend.

**Development Mode**

```bash
cd web && pnpm install && NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:8080" pnpm run dev
## Open a new terminal, start the backend service
go run main.go start
## Access the frontend at
http://localhost:3000
```

### 🔐 Default Credentials

After first launch, visit http://localhost:8080 and log in to the management panel with:

- **Username**: `admin`
- **Password**: `admin`

> ⚠️ **Security Notice**: Please change the default password immediately after first login.

### 📝 Configuration File

The configuration file is located at `data/config.json` by default and is automatically generated on first startup.

**Complete Configuration Example:**

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "database": {
    "type": "sqlite",
    "path": "data/data.db"
  },
  "log": {
    "level": "info"
  }
}
```

**Configuration Options:**

| Option | Description | Default |
|--------|-------------|---------|
| `server.host` | Listen address | `0.0.0.0` |
| `server.port` | Server port | `8080` |
| `database.type` | Database type | `sqlite` |
| `database.path` | Database connection string | `data/data.db` |
| `log.level` | Log level | `info` |

**Database Configuration:**

Three database types are supported:

| Type | `database.type` | `database.path` Format |
|------|-----------------|-----------------------|
| SQLite | `sqlite` | `data/data.db` |
| MySQL | `mysql` | `user:password@tcp(host:port)/dbname` |
| PostgreSQL | `postgres` | `postgresql://user:password@host:port/dbname?sslmode=disable` |

**MySQL Configuration Example:**

```json
{
  "database": {
    "type": "mysql",
    "path": "root:password@tcp(127.0.0.1:3306)/octopus"
  }
}
```

**PostgreSQL Configuration Example:**

```json
{
  "database": {
    "type": "postgres",
    "path": "postgresql://user:password@localhost:5432/octopus?sslmode=disable"
  }
}
```

> 💡 **Tip**: MySQL and PostgreSQL require manual database creation. The application will automatically create the table structure.

### 🌐 Environment Variables

All configuration options can be overridden via environment variables using the format `OCTOPUS_` + configuration path (joined with `_`):

| Environment Variable | Configuration Option |
|---------------------|---------------------|
| `OCTOPUS_SERVER_PORT` | `server.port` |
| `OCTOPUS_SERVER_HOST` | `server.host` |
| `OCTOPUS_DATABASE_TYPE` | `database.type` |
| `OCTOPUS_DATABASE_PATH` | `database.path` |
| `OCTOPUS_LOG_LEVEL` | `log.level` |
| `OCTOPUS_GITHUB_PAT` | For rate limiting when getting the latest version (optional) |
| `OCTOPUS_RELAY_MAX_SSE_EVENT_SIZE` | Maximum SSE event size (optional) |
| `OCTOPUS_IMAGES_BODY_MEMORY_THRESHOLD_MB` | Images request body in-memory threshold. If exceeded, it will be spooled to a temporary file (optional, default 16) |
| `OCTOPUS_IMAGES_BODY_MAX_MB` | Images request body maximum size. Requests above this limit are rejected (optional, default 256) |
| `OCTOPUS_IMAGES_BODY_TMP_DIR` | Images request body temporary directory (optional, default `./cache`) |
| `OCTOPUS_IMAGES_BODY_TMP_CLEANUP_HOURS` | Startup cleanup threshold for temporary files (optional, default 24) |

## 📸 Screenshots

### 🖥️ Desktop

<div align="center">
<table>
<tr>
<td align="center"><b>Site Management</b></td>
<td align="center"><b>Key-aware Groups</b></td>
<td align="center"><b>Managed Channels</b></td>
</tr>
<tr>
<td><img src="web/public/screenshot/desktop-site.png" alt="Site management and failure filters" width="400"></td>
<td><img src="web/public/screenshot/desktop-group.png" alt="Groups with relay badges and Key multipliers" width="400"></td>
<td><img src="web/public/screenshot/desktop-channel.png" alt="Site-managed channels" width="400"></td>
</tr>
</table>
</div>

## 📖 Documentation

### 📡 Channel Management

Channels are the basic configuration units for connecting to LLM providers.

**Base URL Guide:**

The program automatically appends API paths based on channel type. You only need to provide the base URL:

| Channel Type | Auto-appended Path | Base URL | Full Request URL Example |
|--------------|-------------------|----------|--------------------------|
| OpenAI Chat | `/chat/completions` | `https://api.openai.com/v1` | `https://api.openai.com/v1/chat/completions` |
| OpenAI Responses | `/responses` | `https://api.openai.com/v1` | `https://api.openai.com/v1/responses` |
| OpenAI Images | `/images/generations`, `/images/edits`, `/images/variations` | `https://api.openai.com/v1` | `https://api.openai.com/v1/images/generations` |
| Anthropic | `/messages` | `https://api.anthropic.com/v1` | `https://api.anthropic.com/v1/messages` |
| Gemini | `/models/:model:generateContent` | `https://generativelanguage.googleapis.com/v1beta` | `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent` |

> 💡 **Tip**: No need to include specific API endpoint paths in the Base URL - the program handles this automatically.

---

### 📁 Group Management

Groups aggregate multiple channels into a unified external model name.

**Core Concepts:**

- **Group name** is the model name exposed by the program
- When calling the API, set the `model` parameter to the group name

**Load Balancing Modes:**

| Mode | Description |
|------|-------------|
| 🔄 **Round Robin** | Cycles through channels sequentially for each request |
| 🎲 **Random** | Randomly selects an available channel for each request |
| 🛡️ **Failover** | Prioritizes high-priority channels, switches to lower priority only on failure |
| ⚖️ **Weighted** | Distributes requests based on configured channel weights |

> 💡 **Example**: Create a group named `gpt-4o`, add multiple providers' GPT-4o channels to it, then access all channels via a unified `model: gpt-4o`.

---

### 💰 Price Management

Manage model pricing information in the system.

**Data Sources:**

- The system periodically syncs model pricing data from [models.dev](https://github.com/sst/models.dev)
- When creating a channel, if the channel contains models not in models.dev, the system automatically creates pricing information for those models on this page, so this page displays models that haven't had their prices fetched from upstream, allowing users to set prices manually
- Manual creation of models that exist in models.dev is also supported for custom pricing

**Price Priority:**

| Priority | Source | Description |
|:--------:|--------|-------------|
| 🥇 High | This Page | Prices set by user in price management page |
| 🥈 Low | models.dev | Auto-synced default prices |

> 💡 **Tip**: To override a model's default price, simply set a custom price for it in the price management page.

---

### ⚙️ Settings

Global system configuration.

**Statistics Save Interval (minutes):**

Since the program handles numerous statistics, writing to the database on every request would impact read/write performance. The program uses this strategy:

- Statistics are first stored in **memory**
- Periodically **batch-written** to the database at the configured interval

> ⚠️ **Important**: When exiting the program, use proper shutdown methods (like `Ctrl+C` or sending `SIGTERM` signal) to ensure in-memory statistics are correctly written to the database. **Do NOT use `kill -9` or other forced termination methods**, as this may result in statistics data loss.

---

## 🔌 Client Integration

### OpenAI SDK

```python
from openai import OpenAI
import os

client = OpenAI(   
    base_url="http://127.0.0.1:8080/v1",   
    api_key="sk-octopus-your-api-key",
)
completion = client.chat.completions.create(
    model="octopus-openai",  # Use the correct group name
    messages = [
        {"role": "user", "content": "Hello"},
    ],
)
print(completion.choices[0].message.content)
```

### Claude Code

Edit `~/.claude/settings.json`

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

Edit `~/.codex/config.toml`

```toml
model = "octopus-codex" # Use the correct group name

model_provider = "octopus"

[model_providers.octopus]
name = "octopus"
base_url = "http://127.0.0.1:8080/v1"
```

Edit `~/.codex/auth.json`

```json
{
  "OPENAI_API_KEY": "sk-octopus-your-api-key"
}
```

---

## 🔀 Differences from Upstream

This repository is forked directly from [Hureru/octopus](https://github.com/Hureru/octopus) and is now released independently as **Seller-1990/octopus**. It keeps the original Go module path for source compatibility, but never checks the parent repository for application updates.

### 🏗️ New subsystems

- **🌐 Site Management & Site Sync** — imports all-api-hub / Metapi data, syncs accounts, groups, Keys, models, balances and site pricing, classifies account-level failures, and projects usable groups into managed channels. Relay policy and proxy routing are managed in this repository's Sites workspace.
- **🔑 Group-aware Key projection** — persists the multiplier bound to each upstream API Key group, shows it on group members, supports quick Key creation/completion, and suspends only projections that truly lack a usable group Key.
- **🖥️ Windows desktop distribution** — no-console desktop executable, browser launch, system tray, autostart option, graceful shutdown, and NSIS installer.
- **📦 Independent releases** — version metadata, update checks, GHCR images, documentation, and Windows packages all use Seller-1990/octopus; Windows and container deployments link to Releases instead of replacing themselves in place.
- **🔌 WebSocket relay** — upstream WS connection pool with health backoff, client-facing WS, DB-backed response affinity, and opt-in OpenAI Responses passthrough for Codex tools.
- **🖼️ OpenAI Images API forwarding** with body cache.
- **🩹 Transformer overhaul** — native StreamEvent pipeline across all adapters, Anthropic patching layer, role-alternation normalization, plus a long tail of cross-format fidelity fixes.

### 🛠️ Reworked

- **Channel module** — tabbed Site/Manual layout; group editor preserves channel metadata.
- **Relay core** — route learning, retry, cancel propagation, Responses compact proxy, log filtering by channel ID.
- **Auth** — JWT secret persisted in DB (rotation-safe), no longer derived from credentials.
- **Backup**, **logs** (`Item.tsx` rewrite), and **home charts** redesigned.

### 🧬 Misc

- Claude Opus 4.7 adaptive thinking; DB migrations 003–012; site automation controls retained for compatibility.

### Check-in responsibility

The maintained deployment model uses **all-api-hub** for site check-in. Octopus focuses on importing site inventory, syncing and projecting models, routing requests, and exposing the downstream API gateway. Its check-in implementation is retained for compatibility but is not part of this actively used workflow.

> For a complete source diff, compare this repository with its GitHub parent **Hureru/octopus**.

---

## 🤝 Acknowledgments

- 🙏 [looplj/axonhub](https://github.com/looplj/axonhub) - The LLM API adaptation module in this project is directly derived from this repository
- 📊 [sst/models.dev](https://github.com/sst/models.dev) - AI model database providing model pricing data

## 🔗 Friend Links

- 🐧 [LinuxDO](https://linux.do) - A community for tech enthusiasts
