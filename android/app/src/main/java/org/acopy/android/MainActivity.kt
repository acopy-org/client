package org.acopy.android

import android.Manifest
import android.content.BroadcastReceiver
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.view.View
import android.widget.EditText
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.acopy.android.databinding.ActivityMainBinding
import org.json.JSONObject
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

class MainActivity : AppCompatActivity() {

    private lateinit var binding: ActivityMainBinding
    private lateinit var config: ConfigStore
    private lateinit var clipboardManager: ClipboardManager
    private var lastPushedHash: Int? = null

    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(15, TimeUnit.SECONDS)
        .build()

    private val statusReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (intent.action) {
                AcopyService.ACTION_STATUS -> {
                    val status = intent.getStringExtra(AcopyService.EXTRA_STATUS) ?: return
                    binding.tvStatus.text = status
                }
                AcopyService.ACTION_DEVICE_RENAMED -> {
                    val name = intent.getStringExtra(AcopyService.EXTRA_DEVICE_NAME) ?: return
                    binding.tvDevice.text = name
                }
            }
        }
    }

    private val clipboardListener = ClipboardManager.OnPrimaryClipChangedListener {
        pushClipboardIfNew()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)

        config = ConfigStore(this)
        clipboardManager = getSystemService(CLIPBOARD_SERVICE) as ClipboardManager
        clipboardManager.addPrimaryClipChangedListener(clipboardListener)
        requestNotificationPermission()
        updateUI()

        binding.btnLogin.setOnClickListener { authenticate("/api/users/login") }
        binding.btnRegister.setOnClickListener { authenticate("/api/users/register") }

        binding.btnLogout.setOnClickListener {
            config.clear()
            updateUI()
        }

        binding.tvDevice.setOnClickListener { showRenameDialog() }

        binding.btnAccessibility.setOnClickListener {
            ClipboardAccessibilityService.openSettings(this)
        }
    }

    override fun onResume() {
        super.onResume()
        val filter = IntentFilter().apply {
            addAction(AcopyService.ACTION_STATUS)
            addAction(AcopyService.ACTION_DEVICE_RENAMED)
        }
        ContextCompat.registerReceiver(
            this, statusReceiver, filter,
            ContextCompat.RECEIVER_NOT_EXPORTED
        )
        if (config.isLoggedIn) {
            binding.tvStatus.text = AcopyService.currentStatus
            updateAccessibilityButton()
            pushClipboardIfNew()
        }
    }

    override fun onPause() {
        super.onPause()
        unregisterReceiver(statusReceiver)
    }

    override fun onDestroy() {
        clipboardManager.removePrimaryClipChangedListener(clipboardListener)
        super.onDestroy()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus && config.isLoggedIn) {
            pushClipboardIfNew()
        }
    }

    private fun pushClipboardIfNew() {
        if (!config.isLoggedIn) return
        try {
            val clip = clipboardManager.primaryClip ?: return
            val item = clip.getItemAt(0) ?: return

            // Try text first
            val text = item.text?.toString()
            if (text != null && text.isNotEmpty()) {
                val hash = text.hashCode()
                if (hash == lastPushedHash) return
                lastPushedHash = hash
                val content = text.toByteArray(Charsets.UTF_8)
                if (content.size > 10 * 1024 * 1024) return
                AcopyService.pushClipboard(this, content, "text/plain")
                return
            }

            // Try URI (images)
            val uri = item.uri ?: return
            val mimeType = clip.description?.getMimeType(0) ?: return
            if (!mimeType.startsWith("image/")) return
            var bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: return
            val hash = bytes.contentHashCode()
            if (hash == lastPushedHash) return
            lastPushedHash = hash
            var actualMime = mimeType
            if (bytes.size > ClipboardBridge.MAX_PAYLOAD_SIZE) {
                bytes = ClipboardBridge.compressToWebP(bytes) ?: return
                actualMime = "image/webp"
                if (bytes.size > ClipboardBridge.MAX_PAYLOAD_SIZE) return
            }
            AcopyService.pushClipboard(this, bytes, actualMime)
        } catch (_: Exception) {
            // Clipboard not accessible (Android 10+ background restriction)
        }
    }

    private fun updateUI() {
        if (config.isLoggedIn) {
            binding.authGroup.visibility = View.GONE
            binding.statusGroup.visibility = View.VISIBLE
            binding.tvDevice.text = config.deviceName
            binding.tvServer.text = config.serverUrl
            updateAccessibilityButton()
            AcopyService.start(this)
        } else {
            binding.authGroup.visibility = View.VISIBLE
            binding.statusGroup.visibility = View.GONE
        }
    }

    private fun updateAccessibilityButton() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val enabled = ClipboardAccessibilityService.isEnabled(this)
            binding.btnAccessibility.visibility = if (enabled) View.GONE else View.VISIBLE
        } else {
            binding.btnAccessibility.visibility = View.GONE
        }
    }

    private fun authenticate(endpoint: String) {
        val email = binding.etEmail.text.toString().trim()
        val password = binding.etPassword.text.toString().trim()
        if (email.isEmpty() || password.isEmpty()) {
            Toast.makeText(this, "Email and password required", Toast.LENGTH_SHORT).show()
            return
        }

        val serverUrl = config.serverUrl
        setLoading(true)

        thread {
            try {
                val body = JSONObject().apply {
                    put("email", email)
                    put("password", password)
                }
                val request = Request.Builder()
                    .url(serverUrl + endpoint)
                    .post(body.toString().toRequestBody("application/json".toMediaType()))
                    .build()

                val response = http.newCall(request).execute()
                val code = response.code
                val respBody = response.body?.string()
                response.close()

                if (endpoint.endsWith("/register")) {
                    when (code) {
                        201 -> {
                            runOnUiThread {
                                setLoading(false)
                                authenticate("/api/users/login")
                            }
                            return@thread
                        }
                        409 -> throw Exception("Email already registered")
                        else -> throw Exception("Registration failed (status $code)")
                    }
                }

                // Login
                if (code == 401) throw Exception("Invalid email or password")
                if (code != 200) throw Exception("Login failed (status $code)")

                val json = JSONObject(respBody ?: throw Exception("Empty response"))
                val token = json.getString("token")

                config.token = token
                config.deviceName = Build.MODEL

                runOnUiThread {
                    setLoading(false)
                    updateUI()
                }
            } catch (e: Exception) {
                runOnUiThread {
                    setLoading(false)
                    Toast.makeText(this, e.message ?: "Connection failed", Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private fun showRenameDialog() {
        val deviceId = config.deviceId
        if (deviceId.isEmpty()) {
            Toast.makeText(this, "Device ID not available yet", Toast.LENGTH_SHORT).show()
            return
        }
        val input = EditText(this).apply {
            setText(config.deviceName)
            selectAll()
            setPadding(48, 32, 48, 16)
        }
        AlertDialog.Builder(this)
            .setTitle("Rename device")
            .setView(input)
            .setPositiveButton("Rename") { _, _ ->
                val newName = input.text.toString().trim()
                if (newName.isNotEmpty() && newName != config.deviceName) {
                    renameDevice(deviceId, newName)
                }
            }
            .setNegativeButton("Cancel", null)
            .show()
    }

    private fun renameDevice(deviceId: String, newName: String) {
        thread {
            try {
                val body = JSONObject().apply { put("device_name", newName) }
                val request = Request.Builder()
                    .url("${config.serverUrl}/api/devices/$deviceId")
                    .header("Authorization", "Bearer ${config.token}")
                    .patch(body.toString().toRequestBody("application/json".toMediaType()))
                    .build()
                val response = http.newCall(request).execute()
                val code = response.code
                response.close()
                if (code == 200) {
                    runOnUiThread {
                        binding.tvDevice.text = newName
                    }
                } else {
                    runOnUiThread {
                        Toast.makeText(this, "Rename failed (status $code)", Toast.LENGTH_SHORT).show()
                    }
                }
            } catch (e: Exception) {
                runOnUiThread {
                    Toast.makeText(this, e.message ?: "Rename failed", Toast.LENGTH_SHORT).show()
                }
            }
        }
    }

    private fun setLoading(loading: Boolean) {
        binding.btnLogin.isEnabled = !loading
        binding.btnRegister.isEnabled = !loading
        binding.progressBar.visibility = if (loading) View.VISIBLE else View.GONE
    }

    private fun requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                ActivityCompat.requestPermissions(
                    this,
                    arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                    REQ_NOTIFICATION
                )
            }
        }
    }

    companion object {
        private const val REQ_NOTIFICATION = 100
    }
}
