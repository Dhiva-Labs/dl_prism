plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "dev.dhivalabs.dlprism"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "dev.dhivalabs.dlprism"
        // flutter_secure_storage 10.x requires API 23 (Android Keystore +
        // EncryptedSharedPreferences). Flutter's own default floor is
        // already 24 today, so this is a floor rather than a pin: it stops
        // a future Flutter default drop from silently taking the app below
        // what the secure-storage plugin needs.
        minSdk = maxOf(flutter.minSdkVersion, 23)
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    buildTypes {
        release {
            // No upload keystore is configured for this repo (see the
            // desktop app's precedent — signing is left to whoever
            // actually ships this to a store). Debug-signed so
            // `flutter build apk --release` still succeeds for local
            // verification; NOT suitable for Play upload as-is.
            signingConfig = signingConfigs.getByName("debug")
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
