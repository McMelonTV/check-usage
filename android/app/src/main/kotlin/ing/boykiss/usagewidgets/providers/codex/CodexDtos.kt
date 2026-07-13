package ing.boykiss.usagewidgets.providers.codex

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable data class DeviceCodeRequest(@SerialName("client_id") val clientId: String)
@Serializable data class DeviceCodeResponse(
    @SerialName("device_auth_id") val deviceAuthId: String,
    @SerialName("user_code") val userCode: String,
    val interval: String = "5",
)
@Serializable data class DevicePollRequest(
    @SerialName("device_auth_id") val deviceAuthId: String,
    @SerialName("user_code") val userCode: String,
)
@Serializable data class DevicePollResponse(
    @SerialName("authorization_code") val authorizationCode: String,
    @SerialName("code_verifier") val codeVerifier: String,
)
@Serializable data class TokenResponse(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String? = null,
    @SerialName("id_token") val idToken: String? = null,
)
@Serializable data class UsagePayload(
    @SerialName("plan_type") val planType: String? = null,
    @SerialName("rate_limit") val rateLimit: RateLimitDetails? = null,
)
@Serializable data class RateLimitDetails(
    @SerialName("primary_window") val primaryWindow: RateLimitWindow? = null,
    @SerialName("secondary_window") val secondaryWindow: RateLimitWindow? = null,
)
@Serializable data class RateLimitWindow(
    @SerialName("used_percent") val usedPercent: Double? = null,
    @SerialName("limit_window_seconds") val limitWindowSeconds: Int? = null,
    @SerialName("reset_at") val resetAt: Long? = null,
)
@Serializable data class ResetCreditsPayload(
    @SerialName("available_count") val availableCount: Int = 0,
    @SerialName("total_earned_count") val totalEarnedCount: Int = 0,
    val credits: List<ResetCreditDetail> = emptyList(),
)
@Serializable data class ResetCreditDetail(
    val status: String = "",
    @SerialName("expires_at") val expiresAt: String = "",
)
