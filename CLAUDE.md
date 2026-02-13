# WhatsApp RPC - Development Guidelines

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
    whatsapp_rpc/          # Pip installable client
scripts/
  cli.js                   # CLI (all commands)
configs/
  config.yaml              # Configuration
data/                      # Runtime data (gitignored)
  whatsapp.db              # SQLite database
  qr/                      # QR code images
bin/                       # Build output (gitignored)
web/
  app.py                   # Flask UI
  templates/               # HTML templates
```

## Commands

### Native
```bash
npm start         # Start API + Web (auto-installs deps if needed)
npm stop          # Stop all
npm run build     # Build Go binary (auto-installs deps if needed)
npm run clean     # Full cleanup (stops processes, removes bin/, data/*.db, node_modules/)
npm run status    # Check status
npm run api       # Start API only
npm run web       # Start Web only
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
| `restart` | Full reset (logout, delete DB, new QR) |
| `qr` | Get QR code (base64 PNG) |
| `send` | Send message |
| `media` | Download received media |
| `groups` | List groups (cached locally, use `refresh: true` to fetch from API) |
| `group_info` | Get group details (cached locally, use `refresh: true` to fetch from API) |
| `contact_check` | Check if phones are on WhatsApp (cached 24h, use `refresh: true` to force) |
| `contact_profile_pic` | Get profile picture URL (cached 24h, use `refresh: true` to force) |
| `group_invite_link` | Get group invite link (cached 1h, use `refresh: true` to force) |

## Local SQLite Cache

Read queries are cached in SQLite to prevent WhatsApp rate limits (429 errors):

| Data | Cache Strategy |
|------|----------------|
| Groups & participants | Cached on connect, refreshed with `refresh: true` |
| Contact registration | TTL 24 hours |
| Profile pictures | TTL 24 hours |
| Group invite links | TTL 1 hour |

Cache tables in `data/whatsapp.db_history`:
- `groups`, `group_participants` - Group data
- `contact_check_cache` - WhatsApp registration status
- `profile_pic_cache` - Profile picture URLs
- `group_invite_cache` - Invite links

## Configuration

```yaml
# configs/config.yaml
server:
  port: 9400
  host: "127.0.0.1"
database:
  path: "data/whatsapp.db"
```

## Key Files

| File | Purpose |
|------|---------|
| `src/go/whatsapp/service.go` | WhatsApp client, events, media cache |
| `src/go/whatsapp/messages.go` | Send all message types |
| `src/go/whatsapp/history.go` | SQLite cache (groups, contacts, profile pics) |
| `src/go/rpc/rpc.go` | JSON-RPC method routing |
| `web/app.py` | Flask routes, Socket.IO |
| `scripts/cli.js` | CLI for all commands (start/stop/build/clean) |

## Guidelines

- WebSocket only - no REST endpoints
- Keep code simple and flat
- No emojis in code/logs
- Rate limiting enabled by default
- Database stored in `data/whatsapp.db`
- QR codes stored in `data/qr/`
- All npm commands auto-install dependencies if missing
