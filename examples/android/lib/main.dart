import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:wakelock_plus/wakelock_plus.dart';

import 'app_state.dart';
import 'dashboard_tab.dart';
import 'messages_tab.dart';
import 'send_tab.dart';

const _defaultPort = 9400;
const _channel = MethodChannel('com.edgymeow/backend');

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const EdgyMeowApp());
}

class EdgyMeowApp extends StatelessWidget {
  const EdgyMeowApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'EdgyMeow',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: Colors.teal,
          brightness: Brightness.dark,
        ),
        useMaterial3: true,
      ),
      home: const HomePage(),
    );
  }
}

class HomePage extends StatefulWidget {
  const HomePage({super.key});

  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  String _status = 'Starting backend...';
  AppState? _appState;
  bool _ready = false;
  int _currentTab = 0;

  @override
  void initState() {
    super.initState();
    _init();
  }

  @override
  void dispose() {
    WakelockPlus.disable();
    _appState?.dispose();
    _stopBackend();
    super.dispose();
  }

  Future<void> _init() async {
    try {
      setState(() => _status = 'Starting backend...');
      int port = _defaultPort;
      final result =
          await _channel.invokeMethod<Map>('startBackend', {'port': port});
      if (result != null && result['port'] != null) {
        port = result['port'] as int;
      }

      await Future.delayed(const Duration(seconds: 3));

      WakelockPlus.enable();

      final appState = AppState();
      appState.addListener(() {
        if (mounted) setState(() {});
      });
      appState.init(port);

      setState(() {
        _appState = appState;
        _ready = true;
      });
    } on PlatformException catch (e) {
      setState(() => _status = 'Backend failed: ${e.message}');
    } catch (e) {
      setState(() => _status = 'Error: $e');
    }
  }

  Future<void> _stopBackend() async {
    try {
      await _channel.invokeMethod('stopBackend');
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    if (!_ready || _appState == null) {
      return Scaffold(
        backgroundColor: Colors.grey.shade900,
        body: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const CircularProgressIndicator(),
              const SizedBox(height: 16),
              Text(_status, style: const TextStyle(color: Colors.white70)),
            ],
          ),
        ),
      );
    }

    final appState = _appState!;

    return Scaffold(
      appBar: AppBar(
        title: const Text('EdgyMeow'),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 16),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Container(
                  width: 10,
                  height: 10,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: _statusColor(appState),
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  _statusLabel(appState),
                  style: const TextStyle(fontSize: 13),
                ),
              ],
            ),
          ),
        ],
      ),
      body: _buildTab(appState),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _currentTab,
        onDestinationSelected: (index) {
          setState(() => _currentTab = index);
          if (index == 2) appState.clearUnread();
        },
        destinations: [
          const NavigationDestination(
            icon: Icon(Icons.dashboard),
            label: 'Dashboard',
          ),
          const NavigationDestination(
            icon: Icon(Icons.send),
            label: 'Send',
          ),
          NavigationDestination(
            icon: appState.unreadCount > 0
                ? Badge(
                    label: Text('${appState.unreadCount}'),
                    child: const Icon(Icons.message),
                  )
                : const Icon(Icons.message),
            label: 'Messages',
          ),
        ],
      ),
    );
  }

  Widget _buildTab(AppState appState) {
    switch (_currentTab) {
      case 1:
        return SendTab(appState: appState);
      case 2:
        return MessagesTab(appState: appState);
      default:
        return DashboardTab(appState: appState);
    }
  }

  Color _statusColor(AppState appState) {
    if (appState.connectionState == 'connected' && appState.waConnected) {
      return Colors.green;
    }
    if (appState.connectionState == 'connecting' ||
        (appState.connectionState == 'connected' && !appState.waConnected)) {
      return Colors.amber;
    }
    return Colors.red;
  }

  String _statusLabel(AppState appState) {
    if (appState.connectionState == 'connected' && appState.waConnected) {
      return 'Connected';
    }
    if (appState.connectionState == 'connected' && appState.waHasSession) {
      return 'Connecting';
    }
    if (appState.connectionState == 'connecting') {
      return 'Connecting...';
    }
    return 'Offline';
  }
}
