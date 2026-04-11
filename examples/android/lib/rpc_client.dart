import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

/// WebSocket JSON-RPC 2.0 client for edgymeow.
/// Dart port of web/client/js/rpc-client.js.
class RpcClient {
  WebSocketChannel? _channel;
  StreamSubscription? _subscription;
  int _nextId = 0;
  final Map<int, _PendingRequest> _pending = {};
  final Map<String, Set<void Function(Map<String, dynamic>)>> _eventListeners =
      {};
  final Set<void Function(String)> _stateListeners = {};

  String _state = 'disconnected';
  String? _wsUrl;
  Timer? _reconnectTimer;
  double _reconnectDelay = 1000;
  static const double _maxReconnectDelay = 10000;

  String get state => _state;
  bool get connected => _state == 'connected';

  /// Connect to the WebSocket RPC server.
  void connect([String? wsUrl]) {
    if (wsUrl != null) _wsUrl = wsUrl;
    if (_wsUrl == null) throw StateError('No WebSocket URL provided');

    _clearReconnect();
    _setState('connecting');

    try {
      _channel = WebSocketChannel.connect(Uri.parse(_wsUrl!));
      // Wait for the WebSocket handshake to complete before marking connected
      _channel!.ready.then((_) {
        _setState('connected');
        _reconnectDelay = 1000;
        _subscription = _channel!.stream.listen(
          (data) {
            try {
              final msg = jsonDecode(data as String) as Map<String, dynamic>;
              _handleMessage(msg);
            } catch (_) {}
          },
          onError: (_) {},
          onDone: () {
            _setState('disconnected');
            _rejectAllPending('WebSocket closed');
            _scheduleReconnect();
          },
        );
      }).catchError((_) {
        _setState('disconnected');
        _scheduleReconnect();
      });
    } catch (_) {
      _setState('disconnected');
      _scheduleReconnect();
    }
  }

  /// Call an RPC method and return a Future with the result.
  Future<dynamic> call(String method, [Map<String, dynamic>? params]) {
    final completer = Completer<dynamic>();

    if (_channel == null || _state != 'connected') {
      completer.completeError(StateError('Not connected'));
      return completer.future;
    }

    final id = ++_nextId;
    final timer = Timer(const Duration(seconds: 30), () {
      _pending.remove(id);
      if (!completer.isCompleted) {
        completer.completeError(TimeoutException('Request timeout: $method'));
      }
    });

    _pending[id] = _PendingRequest(completer, timer);

    final request = <String, dynamic>{
      'jsonrpc': '2.0',
      'id': id,
      'method': method,
    };
    if (params != null) request['params'] = params;

    _channel!.sink.add(jsonEncode(request));
    return completer.future;
  }

  /// Subscribe to server-pushed events.
  void on(String event, void Function(Map<String, dynamic>) callback) {
    _eventListeners.putIfAbsent(event, () => {}).add(callback);
  }

  /// Unsubscribe from an event.
  void off(String event, void Function(Map<String, dynamic>) callback) {
    _eventListeners[event]?.remove(callback);
  }

  /// Listen for connection state changes.
  void onStateChange(void Function(String) callback) {
    _stateListeners.add(callback);
  }

  /// Close the connection (no reconnect).
  void close() {
    _clearReconnect();
    _subscription?.cancel();
    _subscription = null;
    _channel?.sink.close();
    _channel = null;
    _rejectAllPending('Connection closed');
    _setState('disconnected');
  }

  // --- Internal ---

  void _handleMessage(Map<String, dynamic> msg) {
    // Response to a request (has id)
    final id = msg['id'];
    if (id != null && _pending.containsKey(id)) {
      final pending = _pending.remove(id)!;
      pending.timer.cancel();
      if (msg.containsKey('error')) {
        final error = msg['error'] as Map<String, dynamic>;
        pending.completer
            .completeError(Exception(error['message'] ?? 'RPC error'));
      } else {
        pending.completer.complete(msg['result']);
      }
      return;
    }

    // Server notification/event (has method, no id)
    final method = msg['method'] as String?;
    if (method != null) {
      final params = (msg['params'] as Map<String, dynamic>?) ?? {};
      final listeners = _eventListeners[method];
      if (listeners != null) {
        for (final cb in listeners.toList()) {
          try {
            cb(params);
          } catch (_) {}
        }
      }
      // Wildcard listeners
      final wildcard = _eventListeners['*'];
      if (wildcard != null) {
        for (final cb in wildcard.toList()) {
          try {
            cb({'event': method, ...params});
          } catch (_) {}
        }
      }
    }
  }

  void _setState(String newState) {
    if (_state == newState) return;
    _state = newState;
    for (final cb in _stateListeners.toList()) {
      try {
        cb(newState);
      } catch (_) {}
    }
  }

  void _scheduleReconnect() {
    _clearReconnect();
    _reconnectTimer = Timer(
      Duration(milliseconds: _reconnectDelay.round()),
      () {
        _reconnectDelay =
            (_reconnectDelay * 1.5).clamp(1000, _maxReconnectDelay);
        connect();
      },
    );
  }

  void _clearReconnect() {
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
  }

  void _rejectAllPending(String reason) {
    for (final pending in _pending.values) {
      pending.timer.cancel();
      if (!pending.completer.isCompleted) {
        pending.completer.completeError(StateError(reason));
      }
    }
    _pending.clear();
  }
}

class _PendingRequest {
  final Completer<dynamic> completer;
  final Timer timer;
  _PendingRequest(this.completer, this.timer);
}
