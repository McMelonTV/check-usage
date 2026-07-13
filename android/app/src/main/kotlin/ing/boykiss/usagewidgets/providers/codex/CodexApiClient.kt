package ing.boykiss.usagewidgets.providers.codex

import java.io.IOException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.FormBody
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

class AuthenticationRequiredException(message: String) : IOException(message)
class ProviderCompatibilityException(message: String) : IOException(message)

class CodexApiClient(
    private val client: OkHttpClient,
    private val json: Json,
) {
    suspend fun requestDeviceCode(): DeviceCodeResponse = postJson(
        "$DEVICE_AUTH_BASE/usercode",
        json.encodeToString(DeviceCodeRequest(CLIENT_ID)),
    )

    suspend fun pollDeviceCode(deviceAuthId: String, userCode: String): DevicePollResponse? {
        val request = Request.Builder()
            .url("$DEVICE_AUTH_BASE/token")
            .post(json.encodeToString(DevicePollRequest(deviceAuthId, userCode)).toRequestBody(JSON))
            .build()
        return client.newCall(request).execute().use { response ->
            when (response.code) {
                403, 404 -> null
                in 200..299 -> decode(response.body.string())
                else -> throw IOException("Device authorization failed (${response.code})")
            }
        }
    }

    suspend fun exchangeCode(code: String, verifier: String): TokenResponse {
        val body = FormBody.Builder()
            .add("grant_type", "authorization_code")
            .add("code", code)
            .add("redirect_uri", DEVICE_REDIRECT_URI)
            .add("client_id", CLIENT_ID)
            .add("code_verifier", verifier)
            .build()
        return postForm(TOKEN_URL, body)
    }

    suspend fun refresh(refreshToken: String): TokenResponse {
        val body = FormBody.Builder()
            .add("grant_type", "refresh_token")
            .add("refresh_token", refreshToken)
            .add("client_id", CLIENT_ID)
            .build()
        return postForm(TOKEN_URL, body)
    }

    suspend fun usage(accessToken: String, accountId: String?): UsagePayload = get(
        USAGE_URL, accessToken, accountId, resetCredits = false,
    )

    suspend fun resetCredits(accessToken: String, accountId: String?): ResetCreditsPayload = get(
        RESET_CREDITS_URL, accessToken, accountId, resetCredits = true,
    )

    private inline fun <reified T> get(
        url: String,
        token: String,
        accountId: String?,
        resetCredits: Boolean,
    ): T {
        val builder = Request.Builder().url(url)
            .header("Authorization", "Bearer $token")
            .header("User-Agent", USER_AGENT)
        if (!accountId.isNullOrBlank()) builder.header("chatgpt-account-id", accountId)
        if (resetCredits) {
            builder.header("OpenAI-Beta", "codex-1")
            builder.header("originator", "Codex Desktop")
        }
        return client.newCall(builder.build()).execute().use { response ->
            if (response.code == 401) throw AuthenticationRequiredException("Sign in again")
            if (!response.isSuccessful) throw IOException("Provider request failed (${response.code})")
            decode(response.body.string())
        }
    }

    private inline fun <reified T> postForm(url: String, body: FormBody): T {
        val request = Request.Builder().url(url).post(body).build()
        return client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) throw AuthenticationRequiredException("Token request failed (${response.code})")
            decode(response.body.string())
        }
    }

    private inline fun <reified T> postJson(url: String, body: String): T {
        val request = Request.Builder().url(url).post(body.toRequestBody(JSON)).build()
        return client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) throw IOException("Provider request failed (${response.code})")
            decode(response.body.string())
        }
    }

    private inline fun <reified T> decode(body: String): T = try {
        json.decodeFromString(body)
    } catch (_: Exception) {
        throw ProviderCompatibilityException("Integration needs an update")
    }

    companion object {
        private val JSON = "application/json; charset=utf-8".toMediaType()
        const val CLIENT_ID = "app_EMoamEEZ73f0CkXaXp7hrann"
        const val DEVICE_AUTH_BASE = "https://auth.openai.com/api/accounts/deviceauth"
        const val DEVICE_VERIFICATION_URL = "https://auth.openai.com/codex/device"
        const val DEVICE_REDIRECT_URI = "https://auth.openai.com/deviceauth/callback"
        const val TOKEN_URL = "https://auth.openai.com/oauth/token"
        const val USAGE_URL = "https://chatgpt.com/backend-api/wham/usage"
        const val RESET_CREDITS_URL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
        const val USER_AGENT = "usage-widgets/0.1.0"
    }
}
