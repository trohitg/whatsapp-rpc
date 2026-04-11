import 'dart:convert';

import 'package:flutter/material.dart';

import 'app_state.dart';

class DashboardTab extends StatelessWidget {
  final AppState appState;
  const DashboardTab({super.key, required this.appState});

  @override
  Widget build(BuildContext context) {
    if (appState.connectionState != 'connected') {
      return _buildOffline(context);
    }

    if (appState.waConnected) {
      return _buildConnected(context);
    }

    if (appState.qrImageData != null) {
      return _buildQrPairing(context);
    }

    return _buildDisconnected(context);
  }

  Widget _buildOffline(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.cloud_off, size: 64, color: Colors.grey.shade600),
          const SizedBox(height: 16),
          Text(
            'Backend offline',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          Text(
            'WebSocket: ${appState.connectionState}',
            style: TextStyle(color: Colors.grey.shade500),
          ),
        ],
      ),
    );
  }

  Widget _buildDisconnected(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.phone_android, size: 64, color: Colors.grey.shade600),
          const SizedBox(height: 16),
          Text(
            'WhatsApp not connected',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          if (appState.lastError != null) ...[
            const SizedBox(height: 16),
            Card(
              color: Colors.red.shade900,
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Row(
                  children: [
                    const Icon(Icons.error_outline, color: Colors.red, size: 20),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        appState.lastError!,
                        style: const TextStyle(fontSize: 13),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ],
          const SizedBox(height: 24),
          FilledButton.icon(
            onPressed: () async {
              try {
                appState.lastError = null;
                await appState.start();
              } catch (e) {
                if (context.mounted) {
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(content: Text('Failed to start: $e')),
                  );
                }
              }
            },
            icon: const Icon(Icons.play_arrow),
            label: const Text('Start WhatsApp'),
          ),
        ],
      ),
    );
  }

  Widget _buildQrPairing(BuildContext context) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              'Link WhatsApp',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: 24),
            ClipRRect(
              borderRadius: BorderRadius.circular(12),
              child: Image.memory(
                base64Decode(appState.qrImageData!),
                width: 280,
                height: 280,
                fit: BoxFit.contain,
              ),
            ),
            const SizedBox(height: 20),
            FilledButton.icon(
              onPressed: () async {
                final saved = await appState.saveQrToGallery();
                if (!context.mounted) return;
                if (saved) {
                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(content: Text('QR saved to gallery')),
                  );
                  await appState.openWhatsApp();
                } else {
                  ScaffoldMessenger.of(context).showSnackBar(
                    const SnackBar(content: Text('Failed to save QR')),
                  );
                }
              },
              icon: const Icon(Icons.save_alt),
              label: const Text('Save & Open WhatsApp'),
            ),
            const SizedBox(height: 8),
            Text(
              'Tap the gallery icon in WhatsApp\'s scanner\nto select the saved QR code',
              textAlign: TextAlign.center,
              style: TextStyle(color: Colors.grey.shade400, fontSize: 13),
            ),
            const SizedBox(height: 16),
            OutlinedButton.icon(
              onPressed: () => appState.getQR().catchError((_) {}),
              icon: const Icon(Icons.refresh),
              label: const Text('Refresh QR'),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildConnected(BuildContext context) {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Row(
                children: [
                  const Icon(Icons.check_circle, color: Colors.green, size: 32),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'WhatsApp Connected',
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        if (appState.deviceId != null)
                          Text(
                            'Device: ${appState.deviceId}',
                            style: TextStyle(color: Colors.grey.shade400, fontSize: 12),
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
          const SizedBox(height: 12),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Quick Actions', style: Theme.of(context).textTheme.titleSmall),
                  const SizedBox(height: 12),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      OutlinedButton.icon(
                        onPressed: () async {
                          try {
                            await appState.stop();
                          } catch (e) {
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                SnackBar(content: Text('Error: $e')),
                              );
                            }
                          }
                        },
                        icon: const Icon(Icons.stop),
                        label: const Text('Stop'),
                      ),
                      OutlinedButton.icon(
                        onPressed: () async {
                          try {
                            await appState.restart();
                          } catch (e) {
                            if (context.mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                SnackBar(content: Text('Error: $e')),
                              );
                            }
                          }
                        },
                        icon: const Icon(Icons.restart_alt),
                        label: const Text('Restart'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
