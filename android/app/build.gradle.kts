plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

val appVersionName: String = project.findProperty("APP_VERSION")?.toString()?.removePrefix("v") ?: "1.0.0"
val appVersionCode: Int = appVersionName.split(".").lastOrNull()?.toIntOrNull() ?: 1

android {
    namespace = "org.acopy.android"
    compileSdk = 35

    defaultConfig {
        applicationId = "org.acopy.android"
        minSdk = 26
        targetSdk = 35
        versionCode = appVersionCode
        versionName = appVersionName
    }

    signingConfigs {
        getByName("debug") {
            // Uses default debug keystore at ~/.android/debug.keystore
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            isShrinkResources = false
            signingConfig = signingConfigs.getByName("debug")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        viewBinding = true
    }
}

dependencies {
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.msgpack:msgpack-core:0.9.8")
    implementation("com.github.luben:zstd-jni:1.5.6-4@aar")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
}
