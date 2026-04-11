import 'package:flutter/material.dart';

import 'app_state.dart';

class MessagesTab extends StatelessWidget {
  final AppState appState;
  const MessagesTab({super.key, required this.appState});

  @override
  Widget build(BuildContext context) {
    if (appState.messages.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.inbox, size: 64, color: Colors.grey.shade600),
            const SizedBox(height: 16),
            Text(
              'No messages yet',
              style: TextStyle(color: Colors.grey.shade500),
            ),
          ],
        ),
      );
    }

    return ListView.builder(
      itemCount: appState.messages.length,
      itemBuilder: (context, index) {
        final msg = appState.messages[index];
        final sender = msg['sender_name'] as String? ??
            msg['sender_phone'] as String? ??
            msg['sender'] as String? ??
            'Unknown';
        final text = msg['text'] as String? ??
            msg['message'] as String? ??
            msg['type'] as String? ??
            '[Media]';
        final timestamp = msg['timestamp'] as String? ?? '';

        return ListTile(
          leading: CircleAvatar(
            backgroundColor: Colors.teal.shade700,
            child: Text(
              sender.isNotEmpty ? sender[0].toUpperCase() : '?',
              style: const TextStyle(color: Colors.white),
            ),
          ),
          title: Text(
            sender,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
          subtitle: Text(
            text,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
          trailing: timestamp.isNotEmpty
              ? Text(
                  _formatTime(timestamp),
                  style: TextStyle(fontSize: 11, color: Colors.grey.shade500),
                )
              : null,
        );
      },
    );
  }

  String _formatTime(String timestamp) {
    try {
      final dt = DateTime.parse(timestamp);
      return '${dt.hour.toString().padLeft(2, '0')}:${dt.minute.toString().padLeft(2, '0')}';
    } catch (_) {
      return timestamp;
    }
  }
}
