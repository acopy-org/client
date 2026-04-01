package org.acopy.android

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import golib.Golib

class AcopyService : Service(), golib.Callback {

    private var bridge: Golib.Bridge? = null
    private var clipboardBridge: ClipboardBridge? = null
    private var networkMonitor: NetworkMonitor? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }

        val config = ConfigStore(this)
        if (!config.isLoggedIn) {
            stopSelf()
            return START_NOT_STICKY
        }

        startForeground(NOTIFICATION_ID, buildNotification("Connecting..."))

        val cb = ClipboardBridge(this) { content ->
            bridge?.pushClipboard(content, config.deviceName)
        }
        cb.register()
        clipboardBridge = cb

        try {
            val b = Golib.newBridge(config.serverUrl, config.token, config.deviceName, this)
            b.start()
            bridge = b
        } catch (e: Exception) {
            Log.e(TAG, "Failed to start bridge", e)
            stopSelf()
            return START_NOT_STICKY
        }

        networkMonitor = NetworkMonitor(this) {
            bridge?.reconnect()
        }
        networkMonitor?.register()

        return START_STICKY
    }

    override fun onDestroy() {
        networkMonitor?.unregister()
        clipboardBridge?.unregister()
        bridge?.stop()
        bridge = null
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    // golib.Callback

    override fun onClipboardReceived(content: ByteArray, device: String) {
        clipboardBridge?.writeClipboard(content)
        Log.d(TAG, "Clipboard updated from $device")
    }

    override fun onConnectionStateChanged(connected: Boolean) {
        val text = if (connected) "Connected" else "Reconnecting..."
        updateNotification(text)
        Log.d(TAG, "Connection state: $text")
    }

    override fun onError(msg: String) {
        Log.e(TAG, "Bridge error: $msg")
    }

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
        val stopIntent = PendingIntent.getService(
            this, 1,
            Intent(this, AcopyService::class.java).apply { action = ACTION_STOP },
            PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("acopy")
            .setContentText(status)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentIntent(openIntent)
            .addAction(0, "Stop", stopIntent)
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
        private const val ACTION_STOP = "org.acopy.android.STOP"

        fun start(context: Context) {
            val intent = Intent(context, AcopyService::class.java)
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, AcopyService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }
}
