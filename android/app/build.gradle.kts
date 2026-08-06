import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
}

val minimumAndroidApi = 26

android {
    namespace = "ing.boykiss.aiusagewidgets"
    compileSdk = 36

    defaultConfig {
        applicationId = "ing.boykiss.aiusagewidgets"
        minSdk = minimumAndroidApi
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        vectorDrawables.useSupportLibrary = true
    }

    buildFeatures.compose = true
    packaging.resources.excludes += "/META-INF/{AL2.0,LGPL2.1}"

    splits {
        abi {
            isEnable = true
            reset()
            include("armeabi-v7a", "arm64-v8a", "x86", "x86_64")
            isUniversalApk = true
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_25
        targetCompatibility = JavaVersion.VERSION_25
    }
}

val goMobileAar = layout.buildDirectory.file("generated/go/codexlogic.aar")
val repositoryRoot = rootProject.projectDir.parentFile

fun androidSdkPath(): String? {
    System.getenv("ANDROID_HOME")?.takeIf(String::isNotBlank)?.let { return it }
    System.getenv("ANDROID_SDK_ROOT")?.takeIf(String::isNotBlank)?.let { return it }
    val localProperties = rootProject.file("local.properties")
    if (!localProperties.isFile) return null
    return Properties().apply { localProperties.inputStream().use(::load) }
        .getProperty("sdk.dir")
        ?.takeIf(String::isNotBlank)
}

val initializeGoMobile = tasks.register<Exec>("initializeGoMobile") {
    group = "go mobile"
    description = "Initializes the pinned Go Mobile toolchain."
    workingDir(repositoryRoot)
    commandLine("go", "tool", "gomobile", "init")
    doFirst {
        val sdk = androidSdkPath() ?: error("Set ANDROID_HOME/ANDROID_SDK_ROOT or sdk.dir in android/local.properties")
        environment("ANDROID_HOME", sdk)
        environment("ANDROID_SDK_ROOT", sdk)
    }
}

val buildGoMobile = tasks.register<Exec>("buildGoMobile") {
    group = "go mobile"
    description = "Builds the shared Codex Go logic as an Android AAR."
    dependsOn(initializeGoMobile)
    workingDir(repositoryRoot)
    commandLine(
        "go", "tool", "gomobile", "bind",
        "-target=android",
        "-androidapi=$minimumAndroidApi",
        "-javapkg=ing.boykiss.aiusagewidgets.gobridge",
        "-o", goMobileAar.get().asFile.absolutePath,
        "./mobile/codexlogic",
    )
    inputs.files(
        fileTree(repositoryRoot.resolve("codexapi")) { include("**/*.go") },
        fileTree(repositoryRoot.resolve("mobile/codexlogic")) { include("**/*.go") },
        repositoryRoot.resolve("go.mod"),
        repositoryRoot.resolve("go.sum"),
    )
    outputs.file(goMobileAar)
    doFirst {
        goMobileAar.get().asFile.parentFile.mkdirs()
        val sdk = androidSdkPath() ?: error("Set ANDROID_HOME/ANDROID_SDK_ROOT or sdk.dir in android/local.properties")
        environment("ANDROID_HOME", sdk)
        environment("ANDROID_SDK_ROOT", sdk)
    }
}

tasks.named("preBuild").configure { dependsOn(buildGoMobile) }

kotlin {
    jvmToolchain(25)
    compilerOptions.jvmTarget.set(JvmTarget.JVM_25)
}

dependencies {
    implementation(files(goMobileAar))
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.androidx.room.runtime)
    implementation(libs.androidx.room.ktx)
    ksp(libs.androidx.room.compiler)
    implementation(libs.androidx.work.runtime)
    implementation(libs.androidx.glance.appwidget)
    implementation(libs.androidx.glance.material3)
    implementation(libs.androidx.security.crypto)
    implementation(libs.kotlinx.serialization.json)
    testImplementation(libs.junit)
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.androidx.test.espresso)
    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
}
