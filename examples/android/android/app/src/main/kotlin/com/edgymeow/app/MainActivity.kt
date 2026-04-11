package com.edgymeow.app

import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

class MainActivity : FlutterActivity() {
    companion object {
        private const val CHANNEL = "com.edgymeow/backend"
        private const val DEFAULT_PORT = 9400
    }

    private var backendProcess: Process? = null
    private var activePort: Int = DEFAULT_PORT

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "startBackend" -> {
                    activePort = call.argument<Int>("port") ?: DEFAULT_PORT
                    try {
                        startBackend(activePort)
                        result.success(mapOf("port" to activePort))
                    } catch (e: Exception) {
                        result.error("START_FAILED", e.message, null)
                    }
                }
                "stopBackend" -> {
                    stopBackend()
                    result.success("stopped")
                }
                "isRunning" -> {
                    result.success(backendProcess?.isAlive == true)
                }
                else -> result.notImplemented()
            }
        }
    }

    private fun startBackend(port: Int) {
        if (backendProcess?.isAlive == true) return

        val binaryFile = File(applicationInfo.nativeLibraryDir, "libedgymeow.so")
        if (!binaryFile.exists()) {
            throw RuntimeException("Binary not found at ${binaryFile.absolutePath}")
        }

        val dataDir = File(filesDir, "data").apply { mkdirs() }

        File(filesDir, "config.yaml").writeText("""
            environment: production
            log_level: 4
            server:
              port: $port
              host: 0.0.0.0
            database:
              path: ${dataDir.absolutePath}/whatsapp.db
            qr_timeout_seconds: 300
        """.trimIndent())

        val pb = ProcessBuilder(binaryFile.absolutePath)
        pb.directory(filesDir)
        pb.environment()["SSL_CERT_DIR"] = "/system/etc/security/cacerts"
        pb.environment()["EDGYMEOW_ANDROID"] = "1"
        pb.redirectErrorStream(true)
        backendProcess = pb.start()

        Thread {
            backendProcess?.inputStream?.bufferedReader()?.forEachLine { line ->
                android.util.Log.d("EdgyMeow", line)
            }
        }.start()
    }

    private fun stopBackend() {
        backendProcess?.destroy()
        backendProcess = null
    }

    override fun onDestroy() {
        stopBackend()
        super.onDestroy()
    }
}
