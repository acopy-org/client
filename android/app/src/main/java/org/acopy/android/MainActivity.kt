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
import android.widget.Toast
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
            val status = intent.getStringExtra(AcopyService.EXTRA_STATUS) ?: return
            binding.tvStatus.text = status
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
    }

    override fun onResume() {
        super.onResume()
        ContextCompat.registerReceiver(
            this, statusReceiver,
            IntentFilter(AcopyService.ACTION_STATUS),
            ContextCompat.RECEIVER_NOT_EXPORTED
        )
        if (config.isLoggedIn) {
            binding.tvStatus.text = AcopyService.currentStatus
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
                bytes = ClipboardBridge.compressToJpeg(bytes) ?: return
                actualMime = "image/jpeg"
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
            AcopyService.start(this)
        } else {
            binding.authGroup.visibility = View.VISIBLE
            binding.statusGroup.visibility = View.GONE
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
