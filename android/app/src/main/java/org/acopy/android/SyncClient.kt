package org.acopy.android

import android.util.Log
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

class SyncClient(
    private val serverUrl: String,
    private val token: String,
    private val deviceName: String,
    private val onClipboard: (content: ByteArray, device: String, contentType: String) -> Unit,
    private val onConnectionState: (connected: Boolean) -> Unit,
    private val onError: (msg: String) -> Unit,
    private val onDeviceRenamed: (oldName: String, newName: String) -> Unit = { _, _ -> }
) {
    private val client = OkHttpClient.Builder()
        .pingInterval(30, TimeUnit.SECONDS)
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MINUTES)
        .build()

    private val ws = AtomicReference<WebSocket?>(null)
    private val connected = AtomicBoolean(false)
    private val running = AtomicBoolean(false)
    private val pendingClipboard = AtomicReference<ClipboardPushPayload?>(null)

    @Volatile
    private var backoff = BACKOFF_INITIAL

    fun start() {
        if (running.getAndSet(true)) return
        connect()
    }

    fun stop() {
        running.set(false)
        ws.getAndSet(null)?.close(1000, "stopped")
        client.dispatcher.executorService.shutdown()
    }

    fun pushClipboard(content: ByteArray, device: String, contentType: String = "text/plain") {
        val socket = ws.get()
        if (socket == null || !connected.get()) {
            pendingClipboard.set(ClipboardPushPayload(content, device, contentType))
            Log.d(TAG, "offline — queued clipboard for sync on reconnect")
            return
        }

        val payload = Codec.encodeClipboardPush(content, device, contentType)
        val frame = Codec.encode(MsgType.CLIPBOARD_PUSH, payload)
        if (!socket.send(frame.toByteString())) {
            pendingClipboard.set(ClipboardPushPayload(content, device, contentType))
            Log.d(TAG, "send failed — queued clipboard for sync on reconnect")
        }
    }

    fun sendCopyIntent(device: String) {
        val socket = ws.get() ?: return
        if (!connected.get()) return
        val payload = Codec.encodeCopyIntent(device)
        val frame = Codec.encode(MsgType.COPY_INTENT, payload)
        socket.send(frame.toByteString())
    }

    fun sendCopyCancel() {
        val socket = ws.get() ?: return
        if (!connected.get()) return
        val frame = Codec.encode(MsgType.COPY_CANCEL, null)
        socket.send(frame.toByteString())
    }

    fun isConnected(): Boolean = connected.get()

    fun reconnect() {
        ws.getAndSet(null)?.close(1000, "reconnect")
    }

    private fun connect() {
        if (!running.get()) return

        val wsUrl = serverUrl
            .replace("https://", "wss://")
            .replace("http://", "ws://")
            .trimEnd('/') + "/ws"

        val request = Request.Builder().url(wsUrl).build()

        client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.d(TAG, "websocket open, sending auth")
                val authPayload = Codec.encodeAuth(token)
                val frame = Codec.encode(MsgType.AUTH, authPayload)
                webSocket.send(frame.toByteString())
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                try {
                    handleFrame(webSocket, bytes.toByteArray())
                } catch (e: Exception) {
                    Log.e(TAG, "handle frame error", e)
                    onError("protocol error: ${e.message}")
                }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.e(TAG, "websocket failure", t)
                onDisconnected()
                scheduleReconnect()
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                webSocket.close(code, reason)
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "websocket closed: $code $reason")
                onDisconnected()
                if (code != 1000) scheduleReconnect()
            }
        })
    }

    private fun handleFrame(webSocket: WebSocket, frame: ByteArray) {
        val (msgType, raw) = Codec.decode(frame)

        when (msgType) {
            MsgType.ACK -> {
                if (!connected.getAndSet(true)) {
                    ws.set(webSocket)
                    backoff = BACKOFF_INITIAL
                    Log.d(TAG, "connected and authenticated")
                    onConnectionState(true)
                    flushPending()
                }
            }
            MsgType.CLIPBOARD_BROADCAST -> {
                val payload = Codec.decodeClipboardBroadcast(raw)
                onClipboard(payload.content, payload.device, payload.contentType)
            }
            MsgType.ERROR -> {
                val payload = Codec.decodeError(raw)
                Log.e(TAG, "server error: [${payload.code}] ${payload.msg}")
                if (payload.code == 401) {
                    onError("auth rejected: ${payload.msg}")
                    webSocket.close(1000, "auth failed")
                }
            }
            MsgType.PING -> {
                val pongFrame = Codec.encode(MsgType.PONG, null)
                webSocket.send(pongFrame.toByteString())
            }
            MsgType.PONG -> { /* keepalive */ }
            MsgType.DEVICE_RENAMED -> {
                val payload = Codec.decodeDeviceRenamed(raw)
                Log.d(TAG, "device renamed: ${payload.oldName} -> ${payload.newName}")
                onDeviceRenamed(payload.oldName, payload.newName)
            }
            else -> Log.w(TAG, "unexpected message type: $msgType")
        }
    }

    private fun flushPending() {
        val p = pendingClipboard.getAndSet(null) ?: return
        Log.d(TAG, "flushing queued clipboard")
        pushClipboard(p.content, p.device, p.contentType)
    }

    private fun onDisconnected() {
        if (connected.getAndSet(false)) {
            ws.set(null)
            onConnectionState(false)
        }
    }

    private fun scheduleReconnect() {
        if (!running.get()) return
        Log.d(TAG, "reconnecting in ${backoff}ms")
        Thread {
            Thread.sleep(backoff)
            backoff = (backoff * 2).coerceAtMost(BACKOFF_MAX)
            connect()
        }.start()
    }

    companion object {
        private const val TAG = "SyncClient"
        private const val BACKOFF_INITIAL = 1000L
        private const val BACKOFF_MAX = 30_000L
    }
}
