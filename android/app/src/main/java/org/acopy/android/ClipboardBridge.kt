package org.acopy.android

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context

class ClipboardBridge(
    context: Context,
    private val onLocalCopy: (ByteArray) -> Unit
) : ClipboardManager.OnPrimaryClipChangedListener {

    private val clipboardManager =
        context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    private var ignoreNextChange = false

    override fun onPrimaryClipChanged() {
        if (ignoreNextChange) {
            ignoreNextChange = false
            return
        }

        val clip = clipboardManager.primaryClip ?: return
        val text = clip.getItemAt(0)?.text?.toString() ?: return
        if (text.isEmpty()) return

        val content = text.toByteArray(Charsets.UTF_8)
        if (content.size > MAX_PAYLOAD_SIZE) return

        onLocalCopy(content)
    }

    fun writeClipboard(content: ByteArray) {
        ignoreNextChange = true
        val text = String(content, Charsets.UTF_8)
        val clip = ClipData.newPlainText("acopy", text)
        clipboardManager.setPrimaryClip(clip)
    }

    fun register() {
        clipboardManager.addPrimaryClipChangedListener(this)
    }

    fun unregister() {
        clipboardManager.removePrimaryClipChangedListener(this)
    }

    companion object {
        private const val MAX_PAYLOAD_SIZE = 10 * 1024 * 1024 // 10 MB, matches protocol.MaxPayloadSize
    }
}
