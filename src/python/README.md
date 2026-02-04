# WhatsApp RPC - Python Client

Async Python client for the [WhatsApp RPC](https://github.com/trohitg/whatsapp-rpc) WebSocket API.

## Installation

```bash
pip install whatsapp-rpc
```

Requires the WhatsApp RPC server running separately:

```bash
npm install -g whatsapp-rpc
npx whatsapp-rpc start
```

## Usage

```python
import asyncio
from whatsapp_rpc import WhatsAppRPCClient

async def main():
    client = WhatsAppRPCClient("ws://localhost:9400/ws/rpc")
    await client.connect()

    # Check status
    status = await client.status()
    print(status)

    # Send a text message
    await client.send(phone="1234567890", type="text", message="Hello!")

    # Send an image
    await client.send(
        phone="1234567890",
        type="image",
        media_data={
            "data": "<base64>",
            "mime_type": "image/jpeg",
            "caption": "Check this out!"
        }
    )

    # List groups
    groups = await client.groups()

    # Get chat history
    history = await client.chat_history(phone="1234567890", limit=50)

    await client.close()

asyncio.run(main())
```

## Events

```python
async def main():
    client = WhatsAppRPCClient("ws://localhost:9400/ws/rpc")

    def on_event(event):
        if event["method"] == "event.message_received":
            print(f"New message: {event['params']['text']}")

    client.event_callback = on_event
    await client.connect()

    # Keep running to receive events
    await asyncio.sleep(3600)
    await client.close()
```

## API Methods

| Method | Description |
|--------|-------------|
| `status()` | Get connection status |
| `start()` / `stop()` / `restart()` | Control WhatsApp service |
| `qr()` | Get QR code for pairing |
| `send(**kwargs)` | Send message (text, image, video, audio, document, location, contact) |
| `media(message_id)` | Download media from message |
| `groups()` | List all groups |
| `group_info(group_id)` | Get group details |
| `contacts(query)` | List contacts |
| `contact_check(phones)` | Check WhatsApp registration |
| `chat_history(**kwargs)` | Get message history |
| `typing(jid, state)` | Send typing indicator |
| `presence(status)` | Set online/offline |
| `mark_read(message_ids, chat_jid)` | Mark messages as read |
| `rate_limit_get()` / `rate_limit_set(**config)` | Rate limiting config |

## License

MIT
