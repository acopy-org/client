package org.acopy.android

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat

class AcopyService : Service() {

    private var syncClient: SyncClient? = null
    private var clipboardBridge: ClipboardBridge? = null
    private var networkMonitor: NetworkMonitor? = null
    private val mainHandler = Handler(Looper.getMainLooper())

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_PUSH_CLIPBOARD) {
            val content = intent.getByteArrayExtra(EXTRA_CLIPBOARD) ?: return START_STICKY
            val contentType = intent.getStringExtra(EXTRA_CONTENT_TYPE) ?: "text/plain"
            val config = ConfigStore(this)
            clipboardBridge?.lastContentHash = if (contentType == "text/plain") {
                String(content, Charsets.UTF_8).hashCode()
            } else {
                content.contentHashCode()
            }
            val client = syncClient
            if (client != null) {
                client.pushClipboard(content, config.deviceName, contentType)
            } else {
                Log.w(TAG, "syncClient not ready — clipboard push dropped")
            }
            return START_STICKY
        }

        val config = ConfigStore(this)
        if (!config.isLoggedIn) {
            stopSelf()
            return START_NOT_STICKY
        }

        // Already initialized — don't create duplicate listeners/connections
        if (syncClient != null) return START_STICKY

        currentStatus = "Connecting..."
        sendBroadcast(Intent(ACTION_STATUS).setPackage(packageName).putExtra(EXTRA_STATUS, currentStatus))

        ServiceCompat.startForeground(
            this,
            NOTIFICATION_ID,
            buildNotification("Connecting..."),
            ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
        )

        val cb = ClipboardBridge(this) { content, contentType ->
            syncClient?.sendCopyIntent(config.deviceName)
            syncClient?.pushClipboard(content, config.deviceName, contentType)
        }
        cb.register()
        clipboardBridge = cb

        val client = SyncClient(
            serverUrl = config.serverUrl,
            token = config.token,
            deviceName = config.deviceName,
            onClipboard = { content, device, contentType ->
                mainHandler.post {
                    clipboardBridge?.writeClipboard(content, contentType)
                }
                Log.d(TAG, "Clipboard updated from $device")
            },
            onConnectionState = { connected ->
                val text = if (connected) "Connected" else "Reconnecting..."
                currentStatus = text
                updateNotification(text)
                sendBroadcast(Intent(ACTION_STATUS).setPackage(packageName).putExtra(EXTRA_STATUS, text))
                Log.d(TAG, "Connection state: $text")
            },
            onError = { msg ->
                Log.e(TAG, "Sync error: $msg")
            },
            onDeviceRenamed = { oldName, newName ->
                if (oldName == config.deviceName) {
                    config.deviceName = newName
                    sendBroadcast(Intent(ACTION_DEVICE_RENAMED).setPackage(packageName)
                        .putExtra(EXTRA_DEVICE_NAME, newName))
                    Log.d(TAG, "Device renamed: $oldName -> $newName")
                }
            },
            onDeviceDeleted = { deviceId ->
                if (deviceId == config.deviceId) {
                    Log.d(TAG, "This device was removed remotely, stopping service")
                    config.token = ""
                    config.deviceName = ""
                    config.deviceId = ""
                    sendBroadcast(Intent(ACTION_DEVICE_DELETED).setPackage(packageName))
                    stopSelf()
                }
            },
            onDeviceId = { deviceId ->
                config.deviceId = deviceId
                Log.d(TAG, "Device ID: $deviceId")
            }
        )
        client.start()
        syncClient = client

        networkMonitor = NetworkMonitor(this) {
            syncClient?.reconnect()
        }
        networkMonitor?.register()

        return START_STICKY
    }

    override fun onDestroy() {
        networkMonitor?.unregister()
        clipboardBridge?.unregister()
        syncClient?.stop()
        syncClient = null
        currentStatus = "Stopped"
        sendBroadcast(Intent(ACTION_STATUS).setPackage(packageName).putExtra(EXTRA_STATUS, currentStatus))
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // Notification

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Clipboard Sync",
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "Shows clipboard sync status"
            setShowBadge(false)
        }
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(channel)
    }

    private fun buildNotification(status: String): Notification {
        val openIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("acopy")
            .setContentText(status)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentIntent(openIntent)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(status: String) {
        val nm = getSystemService(NotificationManager::class.java)
        nm.notify(NOTIFICATION_ID, buildNotification(status))
    }

    companion object {
        private const val TAG = "AcopyService"
        private const val CHANNEL_ID = "acopy_sync"
        private const val NOTIFICATION_ID = 1
        const val ACTION_STATUS = "org.acopy.android.STATUS"
        const val ACTION_PUSH_CLIPBOARD = "org.acopy.android.PUSH_CLIPBOARD"
        const val ACTION_DEVICE_RENAMED = "org.acopy.android.DEVICE_RENAMED"
        const val ACTION_DEVICE_DELETED = "org.acopy.android.DEVICE_DELETED"
        const val EXTRA_STATUS = "status"
        const val EXTRA_CLIPBOARD = "clipboard"
        const val EXTRA_CONTENT_TYPE = "content_type"
        const val EXTRA_DEVICE_NAME = "device_name"

        @Volatile
        var currentStatus: String = "Stopped"
            private set

        fun start(context: Context) {
            val intent = Intent(context, AcopyService::class.java)
            context.startForegroundService(intent)
        }

        fun pushClipboard(context: Context, content: ByteArray, contentType: String = "text/plain") {
            val intent = Intent(context, AcopyService::class.java).apply {
                action = ACTION_PUSH_CLIPBOARD
                putExtra(EXTRA_CLIPBOARD, content)
                putExtra(EXTRA_CONTENT_TYPE, contentType)
            }
            context.startService(intent)
        }
    }
}
