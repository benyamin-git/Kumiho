// The Android Gradle root. AGP 9 compiles Kotlin itself (built-in Kotlin), so
// org.jetbrains.kotlin.android must not be applied. The Kotlin Gradle plugin is
// pinned on the buildscript classpath so built-in Kotlin runs on the project
// pin instead of AGP's runtime default; the Compose compiler plugin matches it.
buildscript {
    repositories {
        google()
        mavenCentral()
    }
    dependencies {
        classpath("org.jetbrains.kotlin:kotlin-gradle-plugin:2.4.21")
    }
}

plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.kotlin.compose) apply false
}
