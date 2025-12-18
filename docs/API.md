# WhatsApp RPC API

## Connection

```
ws://localhost:9400/ws/rpc
```

All requests use JSON-RPC 2.0 format:
```json
{"jsonrpc": "2.0", "id": 1, "method": "METHOD_NAME", "params": {...}}
```

## Quick Examples

### Send Text Message
```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {
  "phone": "919876543210",
  "type": "text",
  "message": "Hello!"
}}
```

### Send to Group
```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {
  "group_id": "123456789@g.us",
  "type": "text",
  "message": "Hello group!"
}}
```

### Send Image
```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {
  "phone": "919876543210",
  "type": "image",
  "media_data": {
    "data": "BASE64_IMAGE_DATA",
    "mime_type": "image/jpeg",
    "caption": "Check this out"
  }
}}
```

### Send Document
```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {
  "phone": "919876543210",
  "type": "document",
  "media_data": {
    "data": "BASE64_FILE_DATA",
    "mime_type": "application/pdf",
    "filename": "report.pdf"
  }
}}
```

### Send Location
```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {
  "phone": "919876543210",
  "type": "location",
  "location": {
    "latitude": 37.7749,
    "longitude": -122.4194,
    "name": "San Francisco",
    "address": "California, USA"
  }
}}
```

### Reply to Message
```json
{"jsonrpc": "2.0", "id": 1, "method": "send", "params": {
  "phone": "919876543210",
  "type": "text",
  "message": "This is a reply",
  "reply": {
    "message_id": "ABC123",
    "sender": "919876543210@s.whatsapp.net",
    "content": "Original message text"
  }
}}
```

---

## All Methods

### Connection

#### `status`
Get connection status.
```json
{"jsonrpc": "2.0", "id": 1, "method": "status", "params": {}}
```
Response:
```json
{
  "connected": true,
  "has_session": true,
  "running": true,
  "pairing": false,
  "device_id": "919876543210@s.whatsapp.net"
}
```

#### `start`
Start WhatsApp service.
```json
{"jsonrpc": "2.0", "id": 1, "method": "start", "params": {}}
```

#### `stop`
Stop WhatsApp service.
```json
{"jsonrpc": "2.0", "id": 1, "method": "stop", "params": {}}
```

#### `restart`
Full reset: logout, delete database, generate new QR.
```json
{"jsonrpc": "2.0", "id": 1, "method": "restart", "params": {}}
```

#### `qr`
Get QR code for pairing.
```json
{"jsonrpc": "2.0", "id": 1, "method": "qr", "params": {}}
```
Response:
```json
{
  "has_qr": true,
  "code": "QR_STRING",
  "filename": "data/qr/qr_123.png",
  "image_data": "BASE64_PNG"
}
```

---

### Messaging

#### `send`
Send message (text, image, video, audio, document, sticker, location, contact).

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `phone` | string | * | Phone number (without +) |
| `group_id` | string | * | Group JID (e.g., `123@g.us`) |
| `type` | string | Yes | `text`, `image`, `video`, `audio`, `document`, `sticker`, `location`, `contact` |
| `message` | string | For text | Text content |
| `media_data` | object | For media | `{data, mime_type, filename?, caption?, thumbnail?}` |
| `location` | object | For location | `{latitude, longitude, name?, address?}` |
| `contact` | object | For contact | `{display_name, vcard}` |
| `reply` | object | Optional | `{message_id, sender, content}` |

*Either `phone` or `group_id` required.

#### `media`
Download media from received message.
```json
{"jsonrpc": "2.0", "id": 1, "method": "media", "params": {
  "message_id": "ABC123DEF456"
}}
```
Response:
```json
{
  "data": "BASE64_MEDIA_DATA",
  "mime_type": "image/jpeg"
}
```

---

### Groups

#### `groups`
List all groups.
```json
{"jsonrpc": "2.0", "id": 1, "method": "groups", "params": {}}
```

#### `group_info`
Get group details.
```json
{"jsonrpc": "2.0", "id": 1, "method": "group_info", "params": {
  "group_id": "123456789@g.us"
}}
```
Response:
```json
{
  "jid": "123456789@g.us",
  "name": "Group Name",
  "topic": "Description",
  "owner": "919876543210@s.whatsapp.net",
  "size": 25,
  "participants": [
    {"jid": "...", "phone": "919876543210", "is_admin": true, "is_super_admin": true}
  ]
}
```

#### `group_participants_add`
Add members to group.
```json
{"jsonrpc": "2.0", "id": 1, "method": "group_participants_add", "params": {
  "group_id": "123456789@g.us",
  "participants": ["919876543210", "918765432109"]
}}
```

#### `group_participants_remove`
Remove members from group.
```json
{"jsonrpc": "2.0", "id": 1, "method": "group_participants_remove", "params": {
  "group_id": "123456789@g.us",
  "participants": ["919876543210"]
}}
```

#### `group_invite_link`
Get group invite link.
```json
{"jsonrpc": "2.0", "id": 1, "method": "group_invite_link", "params": {
  "group_id": "123456789@g.us"
}}
```

---

### Contacts

#### `contact_check`
Check if numbers are on WhatsApp.
```json
{"jsonrpc": "2.0", "id": 1, "method": "contact_check", "params": {
  "phones": ["919876543210", "918765432109"]
}}
```
Response:
```json
[
  {"query": "919876543210", "jid": "919876543210@s.whatsapp.net", "is_registered": true, "is_business": false},
  {"query": "918765432109", "is_registered": false}
]
```

#### `contact_profile_pic`
Get profile picture.
```json
{"jsonrpc": "2.0", "id": 1, "method": "contact_profile_pic", "params": {
  "jid": "919876543210@s.whatsapp.net"
}}
```

---

### Presence

#### `typing`
Send typing indicator.
```json
{"jsonrpc": "2.0", "id": 1, "method": "typing", "params": {
  "jid": "919876543210@s.whatsapp.net",
  "state": "composing"
}}
```
States: `composing`, `paused`

#### `presence`
Set online status.
```json
{"jsonrpc": "2.0", "id": 1, "method": "presence", "params": {
  "status": "available"
}}
```
Status: `available`, `unavailable`

#### `mark_read`
Mark messages as read.
```json
{"jsonrpc": "2.0", "id": 1, "method": "mark_read", "params": {
  "message_ids": ["ABC123", "DEF456"],
  "chat_jid": "919876543210@s.whatsapp.net"
}}
```

---

### Rate Limiting

#### `rate_limit_get`
Get rate limit config and stats.
```json
{"jsonrpc": "2.0", "id": 1, "method": "rate_limit_get", "params": {}}
```

#### `rate_limit_set`
Update rate limit config.
```json
{"jsonrpc": "2.0", "id": 1, "method": "rate_limit_set", "params": {
  "enabled": true,
  "min_delay_ms": 3000,
  "max_messages_per_hour": 60,
  "simulate_typing": true
}}
```

---

## Events

Server pushes events as JSON-RPC notifications (no `id` field):

### `event.connected`
```json
{"jsonrpc": "2.0", "method": "event.connected", "params": {
  "status": "connected",
  "device_id": "919876543210@s.whatsapp.net"
}}
```

### `event.disconnected`
```json
{"jsonrpc": "2.0", "method": "event.disconnected", "params": {
  "status": "disconnected",
  "reason": "..."
}}
```

### `event.qr_code`
```json
{"jsonrpc": "2.0", "method": "event.qr_code", "params": {
  "code": "QR_STRING",
  "filename": "data/qr/qr_123.png",
  "image_data": "BASE64_PNG"
}}
```

### `event.message_received`
```json
{"jsonrpc": "2.0", "method": "event.message_received", "params": {
  "message_id": "ABC123",
  "sender": "919876543210@s.whatsapp.net",
  "chat_id": "919876543210@s.whatsapp.net",
  "is_from_me": false,
  "is_group": false,
  "message_type": "text",
  "text": "Hello!",
  "timestamp": "2025-01-15T10:30:00Z"
}}
```

### `event.message_sent`
```json
{"jsonrpc": "2.0", "method": "event.message_sent", "params": {
  "message_id": "XYZ789",
  "to": "919876543210@s.whatsapp.net",
  "type": "text",
  "timestamp": "2025-01-15T10:30:00Z"
}}
```

---

## Error Codes

| Code | Description |
|------|-------------|
| -32700 | Parse error - Invalid JSON |
| -32600 | Invalid Request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32000 | Server error (WhatsApp operation failed) |
| -32001 | No QR available |

---

## Data Files

| File | Description |
|------|-------------|
| `data/whatsapp.db` | SQLite database (session, messages) |
| `data/qr/*.png` | QR code images |
| `data/groups.json` | Auto-generated group data (on connection) |

The `groups.json` file is indexed for fast lookups:
```json
{
  "generated_at": "2025-01-15T10:30:00Z",
  "device_id": "919876543210@s.whatsapp.net",
  "groups": {
    "123456789@g.us": {
      "name": "Group Name",
      "participants": {
        "919876543210": {"phone": "919876543210", "is_admin": true}
      }
    }
  },
  "phone_to_groups": {
    "919876543210": ["123456789@g.us", "987654321@g.us"]
  }
}
```
