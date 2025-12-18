"""
WhatsApp Controller Web UI - WebSocket RPC Version
Uses WebSocket RPC as the primary and only communication method with Go backend.
"""

import os
import asyncio
import json
import logging
import threading
import time
from flask import Flask, render_template, request, jsonify, redirect, url_for, flash, send_from_directory
from flask_socketio import SocketIO, emit
from flask_limiter import Limiter
from flask_limiter.util import get_remote_address

from rpc_client import WhatsAppRPCClient

# Setup logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

app = Flask(__name__)
app.config['SECRET_KEY'] = os.getenv('SECRET_KEY', 'dev-secret-key')
socketio_app = SocketIO(app, cors_allowed_origins="*")

# Configuration
GO_WS_RPC_URL = os.getenv('GO_WS_RPC_URL', 'ws://localhost:9400/ws/rpc')

# Rate Limiting Configuration
RATE_LIMIT_ENABLED = os.getenv('RATE_LIMIT_ENABLED', 'true').lower() == 'true'
RATE_LIMIT_GLOBAL = os.getenv('RATE_LIMIT_GLOBAL', '20 per minute')
RATE_LIMIT_PER_USER = os.getenv('RATE_LIMIT_PER_USER', '10 per minute')
MESSAGE_DELAY_SECONDS = float(os.getenv('MESSAGE_DELAY_SECONDS', '3'))

# Initialize rate limiter
limiter = Limiter(
    app=app,
    key_func=get_remote_address,
    default_limits=[] if not RATE_LIMIT_ENABLED else [RATE_LIMIT_GLOBAL],
    storage_uri="memory://",
    strategy="fixed-window"
)

# RPC Client globals
rpc_loop: asyncio.AbstractEventLoop = None
rpc_client: WhatsAppRPCClient = None


def get_recipient_key():
    """Extract recipient (phone or group_id) for per-user rate limiting"""
    try:
        if request.is_json:
            data = request.json
            return data.get('phone') or data.get('group_id') or get_remote_address()
        elif request.form:
            return request.form.get('phone') or request.form.get('group_id') or get_remote_address()
    except:
        pass
    return get_remote_address()


def apply_message_delay():
    """Add delay between messages to simulate human behavior"""
    if RATE_LIMIT_ENABLED and MESSAGE_DELAY_SECONDS > 0:
        time.sleep(MESSAGE_DELAY_SECONDS)


def run_async(coro, timeout=30):
    """Run async coroutine from sync Flask context."""
    if rpc_loop is None or rpc_client is None:
        raise Exception("RPC client not initialized")
    if not rpc_client.connected:
        raise Exception("RPC client not connected")
    return asyncio.run_coroutine_threadsafe(coro, rpc_loop).result(timeout=timeout)


def init_rpc_client():
    """Initialize RPC client in background thread."""
    global rpc_loop, rpc_client

    rpc_client = WhatsAppRPCClient(GO_WS_RPC_URL)

    # Set event callback to forward to Socket.IO clients
    def on_event(event):
        try:
            method = event.get("method", "")
            event_type = method.replace("event.", "") if method.startswith("event.") else method
            params = event.get("params", {})

            data = {"type": event_type, "data": params}
            socketio_app.emit('whatsapp_event', data, namespace='/')
            logger.debug(f"Event forwarded: {event_type}")
        except Exception as e:
            logger.error(f"Error forwarding event: {e}")

    rpc_client.event_callback = on_event

    def start_loop():
        global rpc_loop
        rpc_loop = asyncio.new_event_loop()
        asyncio.set_event_loop(rpc_loop)
        try:
            rpc_loop.run_until_complete(rpc_client.connect())
            logger.info(f"RPC client connected to {GO_WS_RPC_URL}")
            rpc_loop.run_forever()
        except Exception as e:
            logger.error(f"RPC client connection failed: {e}")

    thread = threading.Thread(target=start_loop, daemon=True)
    thread.start()
    logger.info("RPC client initialization started")


# ============================================================================
# Routes
# ============================================================================

@app.route('/')
def index():
    """Main dashboard"""
    try:
        status = run_async(rpc_client.status())
        return render_template('dashboard.html', status={"success": True, "data": status})
    except Exception as e:
        return render_template('dashboard.html', status={"success": False, "error": str(e)})


@app.route('/api/status')
def api_status():
    """Get WhatsApp connection status"""
    try:
        result = run_async(rpc_client.status())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/start', methods=['POST'])
def api_start():
    """Start WhatsApp service"""
    try:
        result = run_async(rpc_client.start())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/stop', methods=['POST'])
def api_stop():
    """Stop WhatsApp service"""
    try:
        result = run_async(rpc_client.stop())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/restart', methods=['POST'])
def api_restart():
    """Restart WhatsApp service"""
    try:
        result = run_async(rpc_client.restart())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/reset', methods=['POST'])
def api_reset():
    """Reset WhatsApp session"""
    try:
        result = run_async(rpc_client.reset())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/qr')
def api_qr():
    """Get QR code information"""
    try:
        result = run_async(rpc_client.qr())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 404


@app.route('/api/diagnostics')
def api_diagnostics():
    """Get diagnostics information"""
    try:
        result = run_async(rpc_client.diagnostics())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/send', methods=['POST'])
@limiter.limit(RATE_LIMIT_PER_USER, key_func=get_recipient_key)
def api_send():
    """Send WhatsApp message (simple)"""
    data = request.json
    if not data or 'phone' not in data or 'message' not in data:
        return jsonify({"success": False, "error": "Phone and message required"}), 400

    apply_message_delay()

    try:
        result = run_async(rpc_client.send(
            phone=data['phone'],
            message=data['message'],
            type='text'
        ))
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/send/enhanced', methods=['POST'])
@limiter.limit(RATE_LIMIT_PER_USER, key_func=get_recipient_key)
def api_send_enhanced():
    """Send enhanced WhatsApp message (all types)"""
    data = request.json
    if not data:
        return jsonify({"success": False, "error": "Message data required"}), 400

    apply_message_delay()

    try:
        result = run_async(rpc_client.send(**data))
        return jsonify({"success": True, "data": result})
    except Exception as e:
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/media/<message_id>')
def api_media(message_id):
    """Download media from a message"""
    try:
        # Use longer timeout for media downloads (videos can be large)
        result = run_async(rpc_client.media(message_id), timeout=120)
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Media download failed for {message_id}: {e}")
        return jsonify({"success": False, "error": str(e)}), 404


@app.route('/api/groups')
def api_groups():
    """Get all groups"""
    try:
        result = run_async(rpc_client.groups())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to get groups: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/groups/<path:group_id>')
def api_group_info(group_id):
    """Get group info"""
    try:
        result = run_async(rpc_client.group_info(group_id))
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to get group info for {group_id}: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/groups/update', methods=['POST'])
def api_group_update():
    """Update group name/topic"""
    data = request.json
    if not data or 'group_id' not in data:
        return jsonify({"success": False, "error": "group_id required"}), 400

    try:
        result = run_async(rpc_client.group_update(
            group_id=data['group_id'],
            name=data.get('name'),
            topic=data.get('topic')
        ))
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to update group {data.get('group_id')}: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


# ============================================================================
# Rate Limiting API Routes
# ============================================================================

@app.route('/api/rate-limit')
def api_rate_limit_get():
    """Get rate limit configuration and stats"""
    try:
        result = run_async(rpc_client.rate_limit_get())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to get rate limit config: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/rate-limit', methods=['POST'])
def api_rate_limit_set():
    """Update rate limit configuration"""
    data = request.json
    if not data:
        return jsonify({"success": False, "error": "Configuration data required"}), 400

    try:
        result = run_async(rpc_client.rate_limit_set(**data))
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to update rate limit config: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/rate-limit/stats')
def api_rate_limit_stats():
    """Get rate limiting statistics"""
    try:
        result = run_async(rpc_client.rate_limit_stats())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to get rate limit stats: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


@app.route('/api/rate-limit/unpause', methods=['POST'])
def api_rate_limit_unpause():
    """Unpause rate limiting"""
    try:
        result = run_async(rpc_client.rate_limit_unpause())
        return jsonify({"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to unpause rate limiting: {e}")
        return jsonify({"success": False, "error": str(e)}), 500


# ============================================================================
# Pages
# ============================================================================

@app.route('/send')
def send_page():
    """Send message page"""
    return render_template('send.html')


@app.route('/send', methods=['POST'])
@limiter.limit(RATE_LIMIT_PER_USER, key_func=get_recipient_key)
def send_message():
    """Handle message sending from form"""
    phone = request.form.get('phone')
    message = request.form.get('message')

    if not phone or not message:
        flash('Phone number and message are required', 'error')
        return redirect(url_for('send_page'))

    apply_message_delay()

    try:
        run_async(rpc_client.send(phone=phone, message=message, type='text'))
        flash(f'Message sent to {phone}!', 'success')
    except Exception as e:
        flash(f'Failed to send message: {str(e)}', 'error')

    return redirect(url_for('send_page'))


@app.route('/messaging')
def messaging_page():
    """Enhanced messaging page"""
    return render_template('messaging.html')


@app.route('/messages')
def messages_page():
    """Messages viewing page"""
    return render_template('messages.html')


@app.route('/groups')
def groups_page():
    """Groups management page"""
    return render_template('groups.html')


@app.route('/contacts')
def contacts_page():
    """Contacts management page"""
    return render_template('contacts.html')


@app.route('/settings')
def settings_page():
    """Rate limiting and settings page"""
    return render_template('settings.html')


@app.route('/qr/<filename>')
def serve_qr_file(filename):
    """Serve QR PNG files from project root"""
    if filename.startswith('qr_') and filename.endswith('.png'):
        current_dir = os.path.dirname(os.path.abspath(__file__))
        project_root = os.path.dirname(current_dir)
        file_path = os.path.join(project_root, filename)

        if os.path.exists(file_path):
            return send_from_directory(project_root, filename, mimetype='image/png')
        else:
            return jsonify({"success": False, "error": "File not found"}), 404
    return jsonify({"success": False, "error": "Invalid filename"}), 400


# ============================================================================
# WebSocket Events (Flask-SocketIO for browser clients)
# ============================================================================

@socketio_app.on('connect')
def handle_connect():
    logger.info('Browser client connected')
    try:
        status = run_async(rpc_client.status())
        emit('status_update', {"success": True, "data": status})
    except Exception as e:
        emit('status_update', {"success": False, "error": str(e)})


@socketio_app.on('disconnect')
def handle_disconnect():
    logger.info('Browser client disconnected')


@socketio_app.on('request_status')
def handle_status_request():
    try:
        status = run_async(rpc_client.status())
        emit('status_update', {"success": True, "data": status})
    except Exception as e:
        emit('status_update', {"success": False, "error": str(e)})


@socketio_app.on('subscribe_messages')
def handle_subscribe_messages():
    """Client subscribes to receive all WhatsApp events"""
    logger.info('Client subscribed to messages')
    emit('subscribed', {'success': True})


@socketio_app.on('contact_check')
def handle_contact_check(data):
    """Check if phone numbers are registered on WhatsApp"""
    phones = data.get('phones', [])
    if not phones:
        emit('contact_check_result', {"success": False, "error": "phones array required"})
        return
    try:
        result = run_async(rpc_client.contact_check(phones))
        emit('contact_check_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to check contacts: {e}")
        emit('contact_check_result', {"success": False, "error": str(e)})


@socketio_app.on('contact_profile_pic')
def handle_contact_profile_pic(data):
    """Get profile picture for a user or group"""
    jid = data.get('jid')
    if not jid:
        emit('contact_profile_pic_result', {"success": False, "error": "jid required"})
        return
    try:
        result = run_async(rpc_client.contact_profile_pic(jid, data.get('preview', False)))
        emit('contact_profile_pic_result', {"success": True, "data": result, "jid": jid})
    except Exception as e:
        logger.error(f"Failed to get profile picture: {e}")
        emit('contact_profile_pic_result', {"success": False, "error": str(e), "jid": jid})


@socketio_app.on('typing')
def handle_typing(data):
    """Send typing indicator to a chat"""
    jid = data.get('jid')
    if not jid:
        emit('typing_result', {"success": False, "error": "jid required"})
        return
    try:
        result = run_async(rpc_client.typing(
            jid=jid,
            state=data.get('state', 'composing'),
            media=data.get('media', '')
        ))
        emit('typing_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to send typing indicator: {e}")
        emit('typing_result', {"success": False, "error": str(e)})


@socketio_app.on('presence')
def handle_presence(data):
    """Set online/offline presence status"""
    status = data.get('status')
    if not status:
        emit('presence_result', {"success": False, "error": "status required"})
        return
    try:
        result = run_async(rpc_client.presence(status))
        emit('presence_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to set presence: {e}")
        emit('presence_result', {"success": False, "error": str(e)})


@socketio_app.on('mark_read')
def handle_mark_read(data):
    """Mark messages as read"""
    message_ids = data.get('message_ids', [])
    chat_jid = data.get('chat_jid')
    if not message_ids or not chat_jid:
        emit('mark_read_result', {"success": False, "error": "message_ids and chat_jid required"})
        return
    try:
        result = run_async(rpc_client.mark_read(
            message_ids=message_ids,
            chat_jid=chat_jid,
            sender_jid=data.get('sender_jid')
        ))
        emit('mark_read_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to mark messages as read: {e}")
        emit('mark_read_result', {"success": False, "error": str(e)})


@socketio_app.on('rate_limit_get')
def handle_rate_limit_get():
    """Get rate limit configuration and stats"""
    try:
        result = run_async(rpc_client.rate_limit_get())
        emit('rate_limit_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to get rate limit config: {e}")
        emit('rate_limit_result', {"success": False, "error": str(e)})


@socketio_app.on('rate_limit_set')
def handle_rate_limit_set(data):
    """Update rate limit configuration"""
    try:
        result = run_async(rpc_client.rate_limit_set(**data))
        emit('rate_limit_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to update rate limit config: {e}")
        emit('rate_limit_result', {"success": False, "error": str(e)})


@socketio_app.on('rate_limit_stats')
def handle_rate_limit_stats():
    """Get rate limiting statistics"""
    try:
        result = run_async(rpc_client.rate_limit_stats())
        emit('rate_limit_stats_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to get rate limit stats: {e}")
        emit('rate_limit_stats_result', {"success": False, "error": str(e)})


@socketio_app.on('rate_limit_unpause')
def handle_rate_limit_unpause():
    """Unpause rate limiting"""
    try:
        result = run_async(rpc_client.rate_limit_unpause())
        emit('rate_limit_result', {"success": True, "data": result})
    except Exception as e:
        logger.error(f"Failed to unpause rate limiting: {e}")
        emit('rate_limit_result', {"success": False, "error": str(e)})


# ============================================================================
# Main
# ============================================================================

if __name__ == '__main__':
    port = int(os.getenv('PORT', 5000))
    debug = os.getenv('FLASK_ENV') == 'development'

    if debug:
        logging.getLogger().setLevel(logging.DEBUG)

    print(f"Starting WhatsApp Web UI (WebSocket RPC) on port {port}")
    print(f"Go WebSocket RPC URL: {GO_WS_RPC_URL}")

    # Initialize RPC client
    print("Connecting to Go backend via WebSocket RPC...")
    init_rpc_client()
    time.sleep(2)  # Give RPC client time to connect

    # Print routes
    print("Registered routes:")
    for rule in app.url_map.iter_rules():
        print(f"  {rule.rule} - {rule.methods}")

    socketio_app.run(app, host='0.0.0.0', port=port, debug=debug, allow_unsafe_werkzeug=True)
