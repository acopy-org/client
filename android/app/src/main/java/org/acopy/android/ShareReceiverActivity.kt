package org.acopy.android

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity

class ShareReceiverActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val config = ConfigStore(this)
        if (!config.isLoggedIn) {
            Toast.makeText(this, "Not logged in to acopy", Toast.LENGTH_SHORT).show()
            finish()
            return
        }

        when (intent?.action) {
            Intent.ACTION_SEND -> handleSend()
            Intent.ACTION_PROCESS_TEXT -> handleProcessText()
        }

        finish()
    }

    private fun handleSend() {
        val text = intent.getStringExtra(Intent.EXTRA_TEXT)
        if (!text.isNullOrEmpty()) {
            pushText(text)
            return
        }

        val uri = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra(Intent.EXTRA_STREAM)
        }
        if (uri != null) {
            pushUri(uri)
        }
    }

    private fun handleProcessText() {
        val text = intent.getCharSequenceExtra(Intent.EXTRA_PROCESS_TEXT)?.toString()
        if (!text.isNullOrEmpty()) {
            pushText(text)
        }
    }

    private fun pushText(text: String) {
        val content = text.toByteArray(Charsets.UTF_8)
        if (content.size > ClipboardBridge.MAX_PAYLOAD_SIZE) {
            Toast.makeText(this, "Text too large to sync", Toast.LENGTH_SHORT).show()
            return
        }
        AcopyService.pushClipboard(this, content, "text/plain")
        Toast.makeText(this, "Synced to acopy", Toast.LENGTH_SHORT).show()
    }

    private fun pushUri(uri: Uri) {
        try {
            val mimeType = contentResolver.getType(uri) ?: return
            var bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: return
            var actualMime = mimeType
            if (bytes.size > ClipboardBridge.MAX_PAYLOAD_SIZE) {
                if (mimeType.startsWith("image/")) {
                    bytes = ClipboardBridge.compressToWebP(bytes) ?: run {
                        Toast.makeText(this, "Failed to compress image", Toast.LENGTH_SHORT).show()
                        return
                    }
                    actualMime = "image/webp"
                }
                if (bytes.size > ClipboardBridge.MAX_PAYLOAD_SIZE) {
                    Toast.makeText(this, "File too large to sync", Toast.LENGTH_SHORT).show()
                    return
                }
            }
            AcopyService.pushClipboard(this, bytes, actualMime)
            Toast.makeText(this, "Synced to acopy", Toast.LENGTH_SHORT).show()
        } catch (_: Exception) {
            Toast.makeText(this, "Failed to read shared content", Toast.LENGTH_SHORT).show()
        }
    }
}
