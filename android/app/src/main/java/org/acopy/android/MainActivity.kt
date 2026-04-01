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
import org.acopy.android.databinding.ActivityMainBinding
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

class MainActivity : AppCompatActivity() {

    private lateinit var binding: ActivityMainBinding
    private lateinit var config: ConfigStore

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
                val url = URL(serverUrl + endpoint)
                val conn = url.openConnection() as HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "application/json")
                conn.doOutput = true

                val body = JSONObject().apply {
                    put("email", email)
                    put("password", password)
                }
                conn.outputStream.use { it.write(body.toString().toByteArray()) }

                val code = conn.responseCode

                if (endpoint.endsWith("/register")) {
                    if (code == 201) {
                        // Registration succeeded, now login
                        runOnUiThread {
                            setLoading(false)
                            authenticate("/api/users/login")
                        }
                        return@thread
                    } else if (code == 409) {
                        throw Exception("Email already registered")
                    } else {
                        throw Exception("Registration failed (status $code)")
                    }
                }

                // Login
                if (code == 401) throw Exception("Invalid email or password")
                if (code != 200) throw Exception("Login failed (status $code)")

                val respBody = conn.inputStream.bufferedReader().readText()
                val json = JSONObject(respBody)
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
                    Toast.makeText(this, e.message, Toast.LENGTH_LONG).show()
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
