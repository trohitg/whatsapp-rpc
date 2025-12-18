# WhatsApp RPC

WebSocket JSON-RPC 2.0 API for WhatsApp.

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Your App      │     │    Web UI       │     │  Python Client  │
│  (Any Language) │     │   (Flask)       │     │    Library      │
└────────┬────────┘     └────────┬────────┘     └────────┬────────┘
         │                       │                       │
         │      WebSocket        │      HTTP             │
         │      JSON-RPC 2.0     │      :5000            │
         │                       │                       │
         └───────────┬───────────┴───────────────────────┘
                     │
                     ▼
         ┌───────────────────────┐
         │   Go WebSocket API    │
         │       :9400           │
         │  ┌─────────────────┐  │
         │  │  RPC Handler    │  │
         │  │  Rate Limiter   │  │
         │  │  Event Emitter  │  │
         │  └────────┬────────┘  │
         └───────────┼───────────┘
                     │
                     ▼
         ┌───────────────────────┐
         │   WhatsApp Service    │
         │  ┌─────────────────┐  │
         │  │   whatsmeow     │  │
         │  │   (Go Library)  │  │
         │  └────────┬────────┘  │
         └───────────┼───────────┘
                     │
                     ▼
         ┌───────────────────────┐
         │   WhatsApp Servers    │
         └───────────────────────┘

Data Storage:
┌─────────────────────────────────┐
│  data/                          │
│  ├── whatsapp.db   (SQLite)     │
│  ├── qr/*.png      (QR codes)   │
│  └── groups.json   (Group cache)│
└─────────────────────────────────┘
```

## Setup

### Native
```bash
npm start           # Start API + Web UI
npm stop            # Stop all
npm run restart     # Restart all
npm run status      # Check if running
npm run api         # Start API only
npm run web         # Start Web UI only
npm run build       # Build Go binary
npm run clean       # Full cleanup (bin, data, node_modules)
```

All commands auto-install dependencies if missing.

### Docker
```bash
docker-compose up -d      # Start
docker-compose down       # Stop
docker-compose logs -f    # View logs
```

## Endpoints

| Port | Service |
|------|---------|
| 9400 | WebSocket API - `ws://localhost:9400/ws/rpc` |
| 5000 | Web UI - `http://localhost:5000` |

## API

Connect via WebSocket and send JSON-RPC 2.0 requests:

```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {"phone": "1234567890", "type": "text", "message": "Hello"}}
```

### Methods

| Method | Params | Description |
|--------|--------|-------------|
| `status` | - | Connection status |
| `start` | - | Start WhatsApp |
| `stop` | - | Stop WhatsApp |
| `restart` | - | Full reset (logout, delete DB, new QR) |
| `qr` | - | Get QR code (base64 PNG) |
| `send` | `phone/group_id`, `type`, `message/media_data` | Send message |
| `media` | `message_id` | Download received media |
| `groups` | - | List all groups |
| `group_info` | `group_id` | Get group details |
| `contact_check` | `phones[]` | Check if numbers are on WhatsApp |

Full schema: [schema.json](schema.json)

## Events

Server pushes events as JSON-RPC notifications (no `id`):

```json
{"jsonrpc": "2.0", "method": "event.message_received", "params": {...}}
```

Events: `connected`, `disconnected`, `qr_code`, `message_received`, `message_sent`

## Data Files

- `data/whatsapp.db` - SQLite database (session, contacts)
- `data/qr/*.png` - QR code images
- `data/groups.json` - Auto-generated on connection (indexed group data)

## Structure

```
src/go/          # Go backend
src/python/      # Python client library
web/             # Flask web UI
scripts/         # CLI tools
configs/         # YAML config
```

## Requirements

- Go 1.21+
- Python 3.8+
- Node.js 16+
