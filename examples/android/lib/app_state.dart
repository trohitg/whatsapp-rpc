import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';
import 'package:image_gallery_saver_plus/image_gallery_saver_plus.dart';
import 'package:url_launcher/url_launcher.dart';

import 'rpc_client.dart';

/// Application state holding WhatsApp connection info, messages, and the RPC client.
class AppState extends ChangeNotifier {
  final RpcClient rpc = RpcClient();

  String connectionState = 'disconnected';
  Map<String, dynamic> whatsappStatus = {};
  String? qrImageData;
  String? lastError;
  List<Map<String, dynamic>> messages = [];
  int unreadCount = 0;
  bool isSending = false;
  Timer? _statusTimer;

  bool get waConnected => whatsappStatus['connected'] == true;
  bool get waHasSession => whatsappStatus['has_session'] == true;
  bool get waPairing => whatsappStatus['pairing'] == true;
  String? get deviceId => whatsappStatus['device_id'] as String?;

  void init(int port) {
    rpc.onStateChange((state) {
      connectionState = state;
      notifyListeners();

      if (state == 'connected') {
        rpc.call('status').then((result) {
          if (result is Map<String, dynamic>) {
            whatsappStatus = result;
            notifyListeners();
          }
        }).catchError((_) {});

        rpc.call('start').catchError((_) {});
      }
    });

    rpc.on('event.status', (params) {
      whatsappStatus = params;
      notifyListeners();
    });

    rpc.on('event.connected', (params) {
      whatsappStatus['connected'] = true;
      qrImageData = null;
      lastError = null;
      notifyListeners();
    });

    rpc.on('event.disconnected', (params) {
      whatsappStatus['connected'] = false;
      notifyListeners();
    });

    rpc.on('event.logged_out', (params) {
      whatsappStatus['connected'] = false;
      whatsappStatus['has_session'] = false;
      qrImageData = null;
      notifyListeners();
    });

    rpc.on('event.connection_failure', (params) {
      lastError = params['error'] as String? ?? 'Connection failed';
      whatsappStatus['connected'] = false;
      notifyListeners();
    });

    rpc.on('event.temporary_ban', (params) {
      lastError = 'Temporary ban: ${params['code'] ?? 'unknown'}';
      notifyListeners();
    });

    rpc.on('event.qr_code', (params) {
      qrImageData = params['image_data'] as String?;
      notifyListeners();
    });

    rpc.on('event.message_received', (params) {
      messages.insert(0, params);
      unreadCount++;
      notifyListeners();
    });

    rpc.connect('ws://127.0.0.1:$port/ws/rpc');

    // Refresh status periodically
    _statusTimer = Timer.periodic(const Duration(seconds: 30), (_) {
      if (rpc.connected) {
        rpc.call('status').then((result) {
          if (result is Map<String, dynamic>) {
            whatsappStatus = result;
            notifyListeners();
          }
        }).catchError((_) {});
      }
    });
  }

  Future<void> start() => rpc.call('start');
  Future<void> stop() => rpc.call('stop');
  Future<void> restart() => rpc.call('restart');

  Future<bool> saveQrToGallery() async {
    if (qrImageData == null) return false;
    final bytes = base64Decode(qrImageData!);
    final result = await ImageGallerySaverPlus.saveImage(
      Uint8List.fromList(bytes),
      quality: 100,
      name: 'edgymeow_qr',
    );
    return result['isSuccess'] == true;
  }

  Future<void> openWhatsApp() async {
    final uri = Uri.parse('whatsapp://');
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    }
  }

  Future<void> getQR() async {
    final result = await rpc.call('qr');
    if (result is Map<String, dynamic>) {
      qrImageData = result['image_data'] as String?;
      notifyListeners();
    }
  }

  Future<void> sendMessage(String phone, String text) async {
    isSending = true;
    notifyListeners();
    try {
      await rpc.call('send', {
        'phone': phone,
        'type': 'text',
        'message': text,
      });
    } finally {
      isSending = false;
      notifyListeners();
    }
  }

  void clearUnread() {
    if (unreadCount > 0) {
      unreadCount = 0;
      notifyListeners();
    }
  }

  @override
  void dispose() {
    _statusTimer?.cancel();
    rpc.close();
    super.dispose();
  }
}
