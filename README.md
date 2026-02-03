# WhatsApp RPC

[![npm version](https://img.shields.io/npm/v/whatsapp-rpc.svg)](https://www.npmjs.com/package/whatsapp-rpc)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

WebSocket JSON-RPC 2.0 API for QR Based WhatsApp Web.

<img width="1280" height="676" alt="image" src="https://github.com/user-attachments/assets/ddc7324a-8c4d-4557-ac6f-cb091a8ce31f" />

## Installation

```bash
npm install whatsapp-rpc
```

Pre-built binaries are automatically downloaded for your platform (Linux, macOS, Windows).

## Quick Start

```bash
# Start the API server
npx whatsapp-rpc start

# Check status
npx whatsapp-rpc status

# Stop the server
npx whatsapp-rpc stop
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     Your Application                     │
│              (Node.js, Python, Go, etc.)                │
└─────────────────────────┬───────────────────────────────┘
                          │
                          │ WebSocket (JSON-RPC 2.0)
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│              WhatsApp RPC Server (:9400)                 │
│  ┌─────────────────────────────────────────────────┐    │
│  │  • JSON-RPC 2.0 Handler                         │    │
│  │  • Rate Limiter (anti-ban protection)           │    │
│  │  • Event Emitter (real-time notifications)      │    │
│  │  • Message History Store (SQLite)               │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                 whatsmeow (Go Library)                   │
│                WhatsApp Web Protocol                     │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                   WhatsApp Servers                       │
└─────────────────────────────────────────────────────────┘

Data Storage:
┌─────────────────────────────────────────────────────────┐
│  data/                                                   │
│  ├── whatsapp.db    (SQLite: session, contacts, history)│
│  ├── qr/*.png       (QR code images)                    │
│  └── groups.json    (Group cache)                       │
└─────────────────────────────────────────────────────────┘
```

## CLI Commands

```bash
npx whatsapp-rpc start           # Start API server (background)
npx whatsapp-rpc stop            # Stop API server
npx whatsapp-rpc restart         # Restart API server
npx whatsapp-rpc status          # Check if running
npx whatsapp-rpc api --foreground  # Run in foreground (for Docker)
npx whatsapp-rpc build           # Build from source (requires Go)
```

## Docker

```dockerfile
FROM node:20-alpine
WORKDIR /app
RUN npm init -y && npm install whatsapp-rpc
RUN mkdir -p /app/data
ENV PORT=9400
EXPOSE 9400
CMD ["npx", "whatsapp-rpc", "api", "--foreground"]
```

## API Reference

Connect via WebSocket to `ws://localhost:9400/ws/rpc` and send JSON-RPC 2.0 requests.

### Connection & Status

| Method | Description |
|--------|-------------|
| `status` | Get connection status (connected, has_session, running, pairing, device_id) |
| `start` | Start WhatsApp service |
| `stop` | Stop WhatsApp service |
| `restart` | Full reset: logout, delete DB, cleanup, start fresh |
| `reset` | Reset session (logout and delete credentials) |
| `diagnostics` | Get detailed diagnostics information |
| `qr` | Get QR code for pairing (code, filename, image_data as base64 PNG) |

### Messaging

| Method | Parameters | Description |
|--------|------------|-------------|
| `send` | `phone` or `group_id`, `type`, `message`/`media_data`/`location`/`contact` | Send message |
| `media` | `message_id` | Download media from received message (returns base64) |
| `typing` | `jid`, `state` (composing/paused) | Send typing indicator |
| `presence` | `status` (available/unavailable) | Set online/offline status |
| `mark_read` | `message_ids[]`, `chat_jid`, `sender_jid` | Mark messages as read |

**Message Types:** `text`, `image`, `video`, `audio`, `document`, `sticker`, `location`, `contact`

### Contacts

| Method | Parameters | Description |
|--------|------------|-------------|
| `contacts` | `query` (optional) | List all contacts with saved names |
| `contact_info` | `phone` | Get full contact info (name, business status, profile pic) |
| `contact_check` | `phones[]` | Check if numbers are on WhatsApp |
| `contact_profile_pic` | `jid`, `preview` | Get profile picture (returns URL and base64) |

### Groups

| Method | Parameters | Description |
|--------|------------|-------------|
| `groups` | - | List all groups |
| `group_info` | `group_id` | Get group details (name, topic, participants, admins) |
| `group_update` | `group_id`, `name`, `topic` | Update group name/description |
| `group_participants_add` | `group_id`, `participants[]` | Add members to group |
| `group_participants_remove` | `group_id`, `participants[]` | Remove members from group |
| `group_invite_link` | `group_id` | Get invite link |
| `group_revoke_invite` | `group_id` | Revoke and regenerate invite link |

### Chat History

| Method | Parameters | Description |
|--------|------------|-------------|
| `chat_history` | `chat_id`/`phone`/`group_id`, `limit`, `offset`, `sender_phone`, `text_only` | Get stored message history |

### Rate Limiting (Anti-Ban Protection)

| Method | Parameters | Description |
|--------|------------|-------------|
| `rate_limit_get` | - | Get config and statistics |
| `rate_limit_set` | `enabled`, `min_delay_ms`, `max_delay_ms`, `max_messages_per_hour`, etc. | Update configuration |
| `rate_limit_stats` | - | Get current statistics only |
| `rate_limit_unpause` | - | Resume after automatic pause |

## Events

The server pushes events as JSON-RPC notifications (no `id` field):

```json
{"jsonrpc": "2.0", "method": "event.message_received", "params": {...}}
```

| Event | Description |
|-------|-------------|
| `event.status` | Initial status on WebSocket connection |
| `event.connected` | WhatsApp connected successfully |
| `event.disconnected` | WhatsApp disconnected |
| `event.connection_failure` | Connection failed |
| `event.logged_out` | User logged out |
| `event.temporary_ban` | Temporary ban received |
| `event.qr_code` | New QR code available (code, filename, image_data) |
| `event.message_sent` | Message sent successfully |
| `event.message_received` | New message received (includes forwarding info) |
| `event.history_sync_complete` | History sync completed after first login |

### Message Received Event Fields

```json
{
  "message_id": "ABC123",
  "sender": "1234567890@s.whatsapp.net",
  "chat_id": "1234567890@s.whatsapp.net",
  "timestamp": "2025-01-15T10:30:00Z",
  "is_from_me": false,
  "is_group": false,
  "is_forwarded": false,
  "forwarding_score": 0,
  "message_type": "text",
  "text": "Hello world!",
  "group_info": { "group_jid": "...", "sender_jid": "...", "sender_name": "..." }
}
```

## Examples

### Send Text Message

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "send",
  "params": {
    "phone": "1234567890",
    "type": "text",
    "message": "Hello from API!"
  }
}
```

### Send Image with Caption

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "send",
  "params": {
    "phone": "1234567890",
    "type": "image",
    "media_data": {
      "data": "base64_encoded_image_data",
      "mime_type": "image/jpeg",
      "caption": "Check this out!"
    }
  }
}
```

### Send to Group

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "send",
  "params": {
    "group_id": "123456789@g.us",
    "type": "text",
    "message": "Hello group!"
  }
}
```

### Send Location

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "send",
  "params": {
    "phone": "1234567890",
    "type": "location",
    "location": {
      "latitude": 37.7749,
      "longitude": -122.4194,
      "name": "San Francisco",
      "address": "California, USA"
    }
  }
}
```

### Reply to Message

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "send",
  "params": {
    "phone": "1234567890",
    "type": "text",
    "message": "This is a reply!",
    "reply": {
      "message_id": "ABC123",
      "sender": "1234567890@s.whatsapp.net",
      "content": "Original message text"
    }
  }
}
```

### Get Chat History

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "chat_history",
  "params": {
    "phone": "1234567890",
    "limit": 50,
    "text_only": true
  }
}
```

### Configure Rate Limiting

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "rate_limit_set",
  "params": {
    "enabled": true,
    "min_delay_ms": 5000,
    "max_messages_per_hour": 30,
    "simulate_typing": true,
    "randomize_delays": true
  }
}
```

## Error Codes

| Code | Description |
|------|-------------|
| -32700 | Parse error - Invalid JSON |
| -32600 | Invalid Request - Not a valid JSON-RPC 2.0 request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32000 | Server error (WhatsApp operation failed) |
| -32001 | No QR available |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 9400 | WebSocket API port |
| `WHATSAPP_RPC_SKIP_BINARY_DOWNLOAD` | - | Set to `1` to skip binary download |
| `WHATSAPP_RPC_PREFER_SOURCE` | - | Set to `1` to build from source if Go is installed |

## Data Files

| Path | Description |
|------|-------------|
| `data/whatsapp.db` | SQLite database (session, contacts, message history) |
| `data/qr/*.png` | QR code images for pairing |
| `data/groups.json` | Group cache (auto-generated on connection) |

## Building from Source

Requires Go 1.21+:

```bash
npm run build
```

## Full Schema

See [schema.json](schema.json) for complete OpenRPC specification.

## Requirements

- Node.js 18+
- Pre-built binaries available for:
  - Linux (amd64, arm64)
  - macOS (amd64, arm64)
  - Windows (amd64)

## License

MIT
