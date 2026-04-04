package org.acopy.android

import android.view.accessibility.AccessibilityEvent

/**
 * Heuristic detector that analyzes accessibility events to determine
 * when the user performs a copy or cut action.
 *
 * Inspired by XClipper's ClipboardDetection approach.
 */
class CopyDetector {

    private val copyWords = listOf("copy", "cut", "copied", "clipboard")
    private val toastCopiedPattern = "(copied|clipboard)".toRegex(RegexOption.IGNORE_CASE)

    private var prevSelectionEvent: SelectionSnapshot? = null
    private var lastWindowStateText: String? = null

    private data class SelectionSnapshot(
        val packageName: CharSequence?,
        val className: CharSequence?,
        val text: String?,
        val fromIndex: Int,
        val toIndex: Int
    )

    /**
     * Returns true if the given accessibility event indicates a copy/cut action.
     */
    fun isCopyEvent(event: AccessibilityEvent): Boolean {
        return when (event.eventType) {
            // Method 1: User clicked a button/menu item whose text says "Copy" or "Cut"
            AccessibilityEvent.TYPE_VIEW_CLICKED -> {
                val desc = event.contentDescription?.toString() ?: ""
                val text = event.text?.joinToString() ?: ""
                val combined = "$desc $text"
                if (combined.length < MAX_LABEL_LENGTH) {
                    copyWords.any { combined.contains(it, ignoreCase = true) }
                } else false
            }

            // Method 2: Text selection collapsed after being active → likely a copy
            AccessibilityEvent.TYPE_VIEW_TEXT_SELECTION_CHANGED -> {
                val current = SelectionSnapshot(
                    event.packageName, event.className,
                    event.text?.joinToString(), event.fromIndex, event.toIndex
                )
                val prev = prevSelectionEvent
                prevSelectionEvent = current

                if (prev != null
                    && prev.packageName == current.packageName
                    && prev.className == current.className
                    && prev.text == current.text
                    && prev.fromIndex != prev.toIndex       // was selected
                    && current.fromIndex == current.toIndex  // now collapsed
                ) {
                    true
                } else false
            }

            // Method 3: A window state with "Copy" text followed by content change
            AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED -> {
                val text = event.text?.joinToString() ?: ""
                lastWindowStateText = text
                false
            }

            // Method 4: Toast with "copied" or "clipboard" keyword
            AccessibilityEvent.TYPE_NOTIFICATION_STATE_CHANGED -> {
                val className = event.className?.toString() ?: ""
                if (className.contains("Toast")) {
                    val text = event.text?.joinToString() ?: ""
                    toastCopiedPattern.containsMatchIn(text)
                } else false
            }

            else -> false
        }
    }

    companion object {
        private const val MAX_LABEL_LENGTH = 30
    }
}
