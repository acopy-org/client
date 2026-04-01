package org.acopy.android

import android.Manifest
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

    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(15, TimeUnit.SECONDS)
        .build()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)

        config = ConfigStore(this)
        requestNotificationPermission()
        updateUI()

        binding.btnLogin.setOnClickListener { authenticate("/api/users/login") }
        binding.btnRegister.setOnClickListener { authenticate("/api/users/register") }

        binding.btnStart.setOnClickListener {
            AcopyService.start(this)
            updateUI()
        }

        binding.btnStop.setOnClickListener {
            AcopyService.stop(this)
            updateUI()
        }

        binding.btnLogout.setOnClickListener {
            AcopyService.stop(this)
            config.clear()
            updateUI()
        }
    }

    private fun updateUI() {
        if (config.isLoggedIn) {
            binding.authGroup.visibility = View.GONE
            binding.statusGroup.visibility = View.VISIBLE
            binding.tvDevice.text = config.deviceName
            binding.tvServer.text = config.serverUrl
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
                    AcopyService.start(this)
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
