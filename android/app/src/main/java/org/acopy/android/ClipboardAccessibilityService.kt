package org.acopy.android

import android.accessibilityservice.AccessibilityService
import android.content.Context
import android.content.Intent
import android.provider.Settings
import android.util.Log
import android.view.accessibility.AccessibilityEvent

/**
 * Accessibility service that detects copy/cut actions on Android 10+.
 *
 * When a copy is detected, it launches a transparent [ClipboardReaderActivity]
 * that briefly gains focus, reads the clipboard, and pushes it to the sync service.
 */
class ClipboardAccessibilityService : AccessibilityService() {

    private val copyDetector = CopyDetector()

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        if (event == null) return
        if (event.packageName == packageName) return

        if (copyDetector.isCopyEvent(event)) {
            Log.d(TAG, "copy detected via accessibility event: ${event.eventType}")
            ClipboardReaderActivity.launch(applicationContext)
        }
    }

    override fun onInterrupt() {}

    companion object {
        private const val TAG = "ClipboardA11yService"

        fun isEnabled(context: Context): Boolean {
            val enabledServices = Settings.Secure.getString(
                context.contentResolver,
                Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES
            ) ?: return false
            val componentName = "${context.packageName}/${ClipboardAccessibilityService::class.java.canonicalName}"
            return enabledServices.contains(componentName)
        }

        fun openSettings(context: Context) {
            val intent = Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            context.startActivity(intent)
        }
    }
}
