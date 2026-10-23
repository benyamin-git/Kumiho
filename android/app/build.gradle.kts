import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

// Release scripts pass -PkumihoVersion=<version>; local builds default to the
// client version. The versionCode mirrors the Go build convention:
// major*10000 + minor*100 + patch, so v0.5.2 is 502.
val kumihoVersionName: String = providers.gradleProperty("kumihoVersion")
    .getOrElse("0.5.2")
    .removePrefix("v")
val versionMatch = Regex("""^(\d+)\.(\d+)\.(\d+)$""").find(kumihoVersionName)
    ?: throw GradleException("kumihoVersion must be MAJOR.MINOR.PATCH, was '$kumihoVersionName'")
val kumihoVersionMinor = versionMatch.groupValues[2].toInt()
val kumihoVersionPatch = versionMatch.groupValues[3].toInt()
require(kumihoVersionMinor < 100 && kumihoVersionPatch < 100) {
    "kumihoVersion minor and patch must be < 100 (versionCode scheme), was '$kumihoVersionName'"
}
val kumihoVersionCode: Int = versionMatch.groupValues[1].toInt() * 10000 +
    kumihoVersionMinor * 100 +
    kumihoVersionPatch

// The emulator AAR carries an x86_64 .so; ABI splits would leave it out of the
// debug APK, so -PkumihoEmulator=true turns splits off. AGP has no per-variant
// splits switch, so splits also stay off whenever a debug variant is part of
// the invocation: debug is always one app-debug.apk.
val emulatorBuild: Boolean = providers.gradleProperty("kumihoEmulator").orNull.toBoolean()
val debugRequested = gradle.startParameter.taskNames.any { it.contains("debug", ignoreCase = true) }

// Release signing is local-only and never committed: without
// android/keystore.properties the release APK stays unsigned, which is all a
// local compile check needs.
val keystorePropertiesFile = rootProject.file("keystore.properties")
val hasReleaseKeystore = keystorePropertiesFile.isFile
val keystoreProperties = Properties().apply {
    if (hasReleaseKeystore) {
        keystorePropertiesFile.inputStream().use(::load)
    }
}

android {
    namespace = "dev.benyamin.kumiho"
    // The pinned Compose BOM/lifecycle/navigation/core releases advertise
    // minCompileSdk 37, so the app compiles against 37.2 while targetSdk stays
    // 36. See the T2 report: this is the one value that differs from the plan's
    // "compile SDK 36".
    compileSdk = 37
    compileSdkMinor = 2
    ndkVersion = "28.2.13676358"

    defaultConfig {
        applicationId = "dev.benyamin.kumiho"
        minSdk = 26
        targetSdk = 36
        versionCode = kumihoVersionCode
        versionName = kumihoVersionName
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        if (hasReleaseKeystore) {
            create("release") {
                storeFile = rootProject.file(keystoreProperties.getProperty("storeFile"))
                storePassword = keystoreProperties.getProperty("storePassword")
                keyAlias = keystoreProperties.getProperty("keyAlias")
                keyPassword = keystoreProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        debug {
            // The debug build installs side by side with a released Kumiho.
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            if (hasReleaseKeystore) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    splits {
        abi {
            // Keeps debug (and the emulator AAR's x86_64) in one APK; release
            // packaging still emits the per-ABI APKs below plus a universal.
            isEnable = !emulatorBuild && !debugRequested
            reset()
            include("arm64-v8a", "armeabi-v7a")
            // One universal APK rides along with the per-ABI splits; release
            // scripts package all three.
            isUniversalApk = true
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    packaging {
        jniLibs {
            useLegacyPackaging = false
            // The app ships only arm ABIs. Dependency AARs carry x86 copies of
            // their own .so files; keep them for the x86_64 emulator build and
            // strip them from every device/release APK.
            if (!emulatorBuild) {
                excludes += setOf("lib/x86/**", "lib/x86_64/**")
            }
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_17)
    }
}

dependencies {
    // The gomobile AAR built by scripts/android-build-aar.sh. Gradle never runs
    // Go; the toolchain script produces libs/kumiho.aar for real devices or
    // libs/kumiho-emulator.aar (android/amd64) for the x86_64 emulator.
    implementation(files("libs/" + if (emulatorBuild) "kumiho-emulator.aar" else "kumiho.aar"))

    implementation(platform(libs.compose.bom))
    androidTestImplementation(platform(libs.compose.bom))

    implementation(libs.compose.ui)
    implementation(libs.compose.ui.graphics)
    implementation(libs.compose.ui.tooling.preview)
    implementation(libs.compose.material3)
    implementation(libs.compose.material.icons.core)

    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.core.splashscreen)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.datastore.preferences)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    testImplementation(libs.json)

    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.androidx.test.espresso.core)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.core)
    androidTestImplementation(libs.compose.ui.test.junit4)

    debugImplementation(libs.compose.ui.tooling)
    debugImplementation(libs.compose.ui.test.manifest)
}
