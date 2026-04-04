package org.acopy.android

import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.util.Log
import androidx.appcompat.app.AppCompatActivity
import kotlinx.coroutines.*

/**
 * Transparent activity that briefly gains focus to read the clipboard on Android 10+.
 *
 * Launched by [ClipboardAccessibilityService] when a copy action is detected.
 * Reads the clipboard content, pushes it to [AcopyService], then finishes immediately.
 */
class ClipboardReaderActivity : AppCompatActivity() {

    private val scope = CoroutineScope(Dispatchers.Main + SupervisorJob())

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // No layout — this activity is completely invisible
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus) return

        scope.launch {
            // Small delay to let the clipboard populate
            delay(300)
            readAndPush()
            finish()
        }
    }

    private fun readAndPush() {
        val config = ConfigStore(this)
        if (!config.isLoggedIn) return

        try {
            val clipboardManager = getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
            val clip = clipboardManager.primaryClip ?: return
            val item = clip.getItemAt(0) ?: return

            // Try text
            val text = item.text?.toString()
            if (text != null && text.isNotEmpty()) {
                val content = text.toByteArray(Charsets.UTF_8)
                if (content.size > ClipboardBridge.MAX_PAYLOAD_SIZE) return
                AcopyService.pushClipboard(this, content, "text/plain")
                Log.d(TAG, "pushed text from accessibility read (${content.size} bytes)")
                return
            }

            // Try image URI
            val uri = item.uri ?: return
            val mimeType = clip.description?.getMimeType(0) ?: return
            if (!mimeType.startsWith("image/")) return
            var bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: return
            var actualMime = mimeType
            if (bytes.size > ClipboardBridge.MAX_PAYLOAD_SIZE) {
                bytes = ClipboardBridge.compressToWebP(bytes) ?: return
                actualMime = "image/webp"
                if (bytes.size > ClipboardBridge.MAX_PAYLOAD_SIZE) return
            }
            AcopyService.pushClipboard(this, bytes, actualMime)
            Log.d(TAG, "pushed image from accessibility read ($actualMime, ${bytes.size} bytes)")
        } catch (e: Exception) {
            Log.e(TAG, "clipboard read failed", e)
        }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    companion object {
        private const val TAG = "ClipboardReader"

        fun launch(context: Context) {
            val intent = Intent(context, ClipboardReaderActivity::class.java)
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_NO_ANIMATION)
            context.startActivity(intent)
        }
    }
}
