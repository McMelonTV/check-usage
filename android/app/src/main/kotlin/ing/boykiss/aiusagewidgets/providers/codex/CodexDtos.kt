package ing.boykiss.aiusagewidgets.providers.codex

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable data class DeviceCodeResponse(
    @SerialName("device_auth_id") val deviceAuthId: String,
    @SerialName("user_code") val userCode: String,
    val interval: String = "5",
)

@Serializable data class DevicePollResponse(
    @SerialName("authorization_code") val authorizationCode: String,
    @SerialName("code_verifier") val codeVerifier: String,
)

@Serializable data class TokenResponse(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String = "",
    @SerialName("id_token") val idToken: String = "",
)

@Serializable data class IdentityResponse(
    val email: String? = null,
    @SerialName("plan_type") val planType: String? = null,
    @SerialName("account_id") val accountId: String? = null,
)

@Serializable data class GoCredentials(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String,
    @SerialName("id_token") val idToken: String,
    @SerialName("account_id") val accountId: String? = null,
)

@Serializable data class RefreshCredentialsResponse(
    val credentials: GoCredentials,
    val changed: Boolean,
)

@Serializable data class GoUsageSnapshot(
    @SerialName("plan_type") val planType: String? = null,
    val windows: List<GoUsageWindow> = emptyList(),
    val credits: GoCreditMetric? = null,
    @SerialName("fetched_at_epoch_millis") val fetchedAtEpochMillis: Long,
    @SerialName("credits_error") val creditsError: String? = null,
)

@Serializable data class GoUsageWindow(
    val kind: String,
    val label: String,
    @SerialName("used_percent") val usedPercent: Double? = null,
    @SerialName("remaining_percent") val remainingPercent: Double? = null,
    @SerialName("reset_at") val resetAt: Long? = null,
    @SerialName("window_seconds") val windowSeconds: Int? = null,
)

@Serializable data class GoCreditMetric(
    @SerialName("available_count") val availableCount: Int,
    @SerialName("total_earned_count") val totalEarnedCount: Int,
    @SerialName("earliest_expiry_epoch_seconds") val earliestExpiryEpochSeconds: Long? = null,
)
