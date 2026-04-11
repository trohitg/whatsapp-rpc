# EdgyMeow - Development Guidelines

## Overview

WebSocket-only WhatsApp API using JSON-RPC 2.0 protocol. No REST endpoints.

## Structure

```
src/
  go/
    cmd/server/main.go     # Entry point
    config/config.go       # Viper configuration
    rpc/server.go          # WebSocket server
    rpc/rpc.go             # RPC method handlers
    whatsapp/service.go    # WhatsApp client, events
    whatsapp/messages.go   # Message sending
    whatsapp/types.go      # Data structures
  python/
    edgymeow/              # Pip installable client
scripts/
  cli.js                   # CLI (all commands)
  serve-client.js          # Static web server for web/client/
configs/
  config.yaml              # Configuration
data/                      # Runtime data (gitignored)
  whatsapp.db              # SQLite database
  qr/                      # QR code images
bin/                       # Build output (gitignored)
web/
  app.py                   # Flask UI (legacy, requires Python)
  templates/               # Flask/Jinja2 HTML templates (legacy)
  client/                  # Standalone static web client (no Python needed)
    js/rpc-client.js       # WebSocket JSON-RPC 2.0 client
    js/app.js              # Shared nav, status, RPC init
    index.html             # Dashboard
    send.html              # Simple send
    messaging.html         # Enhanced messaging
    messages.html          # Received messages
    groups.html            # Groups management
    contacts.html          # Contacts
    settings.html          # Settings & rate limiting
examples/
  android/                 # CrossMeow Flutter app (native UI + Go binary)
```

## Commands

### Native
```bash
npm start         # Start API + Web (auto-installs deps if needed)
npm stop          # Stop all
npm run build     # Build Go binary (auto-installs deps if needed)
npm run build-cross  # Cross-compile all 7 platform targets
npm run clean     # Full cleanup (stops processes, removes bin/, data/*.db, node_modules/)
npm run status    # Check status
npm run api       # Start API only
npm run web       # Start static web client (no Python needed)
npm run dev       # Start API + static web client together
```

### Docker
```bash
docker-compose up -d      # Start all containers
docker-compose down       # Stop all containers
docker-compose logs -f    # View logs
docker-compose build      # Rebuild images
```

## Ports

- **9400** - WebSocket API (`ws://localhost:9400/ws/rpc`)
- **5000** - Web UI (`http://localhost:5000`)

## JSON-RPC Methods

| Method | Description |
|--------|-------------|
| `status` | Get connection status |
| `start` | Start WhatsApp |
| `stop` | Stop WhatsApp |
| `restart` | Full reset (logout, clear all caches, new QR) |
| `qr` | Get QR code (base64 PNG) |
| `send` | Send message |
| `media` | Download received media |
| `groups` | List groups (cached locally, use `refresh: true` to fetch from API) |
| `group_info` | Get group details (cached locally, use `refresh: true` to fetch from API) |
| `contact_check` | Check if phones are on WhatsApp (cached 24h, use `refresh: true` to force) |
| `contact_profile_pic` | Get profile picture URL (cached 24h, use `refresh: true` to force) |
| `group_invite_link` | Get group invite link (cached 1h, use `refresh: true` to force) |
| `newsletters` | List subscribed channels (cached 24h, use `refresh: true` to fetch) |
| `newsletter_info` | Get channel details by JID or invite link |
| `newsletter_create` | Create a new channel |
| `newsletter_follow` | Subscribe to a channel |
| `newsletter_unfollow` | Unsubscribe from a channel |
| `newsletter_mute` | Mute/unmute a channel |
| `newsletter_messages` | Get channel messages (lazy cached in SQLite, use `refresh: true` to re-fetch). Returns media info for download via `media` RPC |
| `newsletter_send` | Send message to a channel (admin only) |
| `newsletter_mark_viewed` | Mark channel messages as viewed |
| `newsletter_react` | React to a channel message |
| `newsletter_live_updates` | Subscribe to live updates (views/reactions) |
| `newsletter_stats` | Get channel statistics (views, reactions, aggregates) |

## Local SQLite Cache

Read queries are cached in SQLite to prevent WhatsApp rate limits (429 errors):

| Data | Cache Strategy |
|------|----------------|
| Groups & participants | Cached on connect, refreshed with `refresh: true` |
| Contact registration | TTL 24 hours |
| Profile pictures | TTL 24 hours |
| Group invite links | TTL 1 hour |
| Newsletter (channel) metadata | TTL 24 hours |
| Newsletter messages | Lazy per-channel (fetched on first access, use `refresh: true` to re-fetch) |

Cache tables in `data/whatsapp.db_history`:
- `groups`, `group_participants` - Group data
- `contact_check_cache` - WhatsApp registration status
- `profile_pic_cache` - Profile picture URLs
- `group_invite_cache` - Invite links
- `newsletter_cache` - Channel metadata
- `newsletter_message_cache` - Channel messages with media info

## Configuration

```yaml
# configs/config.yaml
environment: "development"
log_level: 4
server:
  port: 9400
  host: "127.0.0.1"
database:
  path: "data/whatsapp.db"
qr_timeout_seconds: 300
newsletter:
  fetch_page_size: 100
  fetch_delay_ms: 2000
  default_limit: 50
  max_limit: 500
  media_cache_size: 100
```

## Key Files

| File | Purpose |
|------|---------|
| `src/go/whatsapp/service.go` | WhatsApp client, events, media cache |
| `src/go/whatsapp/messages.go` | Send all message types |
| `src/go/whatsapp/history.go` | SQLite cache (groups, contacts, profile pics) |
| `src/go/rpc/rpc.go` | JSON-RPC method routing |
| `web/app.py` | Flask routes, Socket.IO (legacy) |
| `web/client/` | Standalone static web client (connects directly to Go WS) |
| `web/client/js/rpc-client.js` | WebSocket JSON-RPC 2.0 client class |
| `web/client/js/app.js` | Shared nav, status indicator, RPC init |
| `scripts/cli.js` | CLI for all commands (start/stop/build/clean/web/dev) |
| `scripts/serve-client.js` | Node.js static file server for web/client/ |
| `examples/android/` | CrossMeow Flutter app (native UI + embedded Go binary) |

## Cross-Platform / Android

### SQLite Driver
Uses `ncruces/go-sqlite3` (WASM via wazero) -- works on all platforms including Android. **Must import both packages:**
```go
_ "github.com/ncruces/go-sqlite3/driver"  // registers "sqlite3" driver
_ "github.com/ncruces/go-sqlite3/embed"   // embeds SQLite WASM binary
```
Dialect is `"sqlite3"` (not `"sqlite"`). Do NOT use `modernc.org/sqlite` -- it crashes on Android.

### Build Targets
```bash
npm run build-cross  # All 7 targets
```

| Target | GOOS | GOARCH | Output |
|--------|------|--------|--------|
| Linux x64 | linux | amd64 | `edgymeow-server-linux-amd64` |
| Linux arm64 | linux | arm64 | `edgymeow-server-linux-arm64` |
| macOS x64 | darwin | amd64 | `edgymeow-server-darwin-amd64` |
| macOS arm64 | darwin | arm64 | `edgymeow-server-darwin-arm64` |
| Windows x64 | windows | amd64 | `edgymeow-server-windows-amd64.exe` |
| Android arm64 | android | arm64 | `libedgymeow-android-arm64.so` |
| Android x86_64 | linux | amd64 | `libedgymeow-android-x86_64.so` |

Android emulator uses `GOOS=linux` (not `android`) because `android/amd64` requires CGO.

### Android Integration
- Binary must be named `lib*.so` and placed in `jniLibs/{abi}/` for SELinux execute permission
- Set env var `SSL_CERT_DIR=/system/etc/security/cacerts` when launching
- Set env var `EDGYMEOW_ANDROID=1` to activate Android DNS resolver (for emulator builds)
- Gradle: `useLegacyPackaging = true` and `abiFilters += listOf("x86_64", "arm64-v8a")`

## Versioning

- `package.json` - npm version (single source of truth for local dev)
- Python version is derived dynamically from git tags via `setuptools-scm` (no hardcoded version in `pyproject.toml`)
- Release workflow syncs both npm and PyPI versions from the git tag automatically

## Guidelines

- WebSocket only - no REST endpoints
- Keep code simple and flat
- No emojis in code/logs
- Rate limiting enabled by default
- Database stored in `data/whatsapp.db`
- QR codes stored in `data/qr/`
- All npm commands auto-install dependencies if missing
