package org.acopy.android

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.os.Build
import android.util.Log
import java.io.ByteArrayOutputStream

class ClipboardBridge(
    private val context: Context,
    private val onLocalCopy: (content: ByteArray, contentType: String) -> Unit
) : ClipboardManager.OnPrimaryClipChangedListener {

    private val clipboardManager =
        context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    private var ignoreNextChange = false
    @Volatile
    var lastContentHash: Int? = null

    override fun onPrimaryClipChanged() {
        if (ignoreNextChange) {
            ignoreNextChange = false
            return
        }

        val clip = clipboardManager.primaryClip
        if (clip == null) {
            Log.d(TAG, "clipboard changed but content not accessible (background restriction)")
            return
        }
        val item = clip.getItemAt(0) ?: return

        // Try text first
        val text = item.text?.toString()
        if (text != null && text.isNotEmpty()) {
            val hash = text.hashCode()
            if (hash == lastContentHash) return
            lastContentHash = hash
            val content = text.toByteArray(Charsets.UTF_8)
            if (content.size > MAX_PAYLOAD_SIZE) return
            onLocalCopy(content, "text/plain")
            return
        }

        // Try URI (images, files)
        val uri = item.uri ?: return
        val mimeType = clip.description?.getMimeType(0) ?: return
        if (!mimeType.startsWith("image/")) return

        try {
            var bytes = context.contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: return
            val hash = bytes.contentHashCode()
            if (hash == lastContentHash) return
            lastContentHash = hash

            var actualMime = mimeType
            if (bytes.size > MAX_PAYLOAD_SIZE) {
                bytes = compressToWebP(bytes) ?: return
                actualMime = "image/webp"
                Log.d(TAG, "compressed image to WebP (${bytes.size} bytes)")
                if (bytes.size > MAX_PAYLOAD_SIZE) return
            }

            Log.d(TAG, "captured image: $actualMime (${bytes.size} bytes)")
            onLocalCopy(bytes, actualMime)
        } catch (e: Exception) {
            Log.e(TAG, "failed to read clipboard URI", e)
        }
    }

    fun writeClipboard(content: ByteArray, contentType: String = "text/plain") {
        ignoreNextChange = true
        if (contentType == "text/plain") {
            val text = String(content, Charsets.UTF_8)
            lastContentHash = text.hashCode()
            val clip = ClipData.newPlainText("acopy", text)
            clipboardManager.setPrimaryClip(clip)
        } else {
            // For non-text content, just update the hash to avoid re-pushing
            lastContentHash = content.contentHashCode()
        }
    }

    fun register() {
        clipboardManager.addPrimaryClipChangedListener(this)
    }

    fun unregister() {
        clipboardManager.removePrimaryClipChangedListener(this)
    }

    companion object {
        private const val TAG = "ClipboardBridge"
        const val MAX_PAYLOAD_SIZE = 10 * 1024 * 1024

        fun compressToWebP(imageBytes: ByteArray, quality: Int = 80): ByteArray? {
            val bitmap = BitmapFactory.decodeByteArray(imageBytes, 0, imageBytes.size) ?: return null
            val format = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                Bitmap.CompressFormat.WEBP_LOSSY
            } else {
                @Suppress("DEPRECATION")
                Bitmap.CompressFormat.WEBP
            }
            val out = ByteArrayOutputStream()
            bitmap.compress(format, quality, out)
            bitmap.recycle()
            return out.toByteArray()
        }
    }
}
