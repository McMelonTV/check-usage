package ing.boykiss.aiusagewidgets.domain

import androidx.compose.runtime.Immutable
import kotlinx.serialization.Serializable

@Serializable
@JvmInline value class ProviderId(val value: String)
@Serializable
@JvmInline value class ProviderAccountId(val value: String)

enum class UsageMetricKind { SHORT_WINDOW, LONG_WINDOW, MONTHLY_WINDOW, RESET_CREDITS }
enum class AuthenticationState { CONNECTED, SIGN_IN_REQUIRED }
enum class DataFreshness { FRESH, STALE, ERROR }
enum class WidgetVisualStyle(val displayName: String) {
    TONAL("Tonal"),
    FROSTED("Frosted"),
    MIDNIGHT("Midnight"),
    SUNSET("Sunset"),
    DOT_MATRIX("Dot Matrix"),
    ;

    companion object {
        val Default = TONAL

        /** Resolves a persisted style name, including names written before the styles were renamed. */
        fun fromStoredName(name: String?): WidgetVisualStyle = when (name) {
            "MATERIAL_YOU" -> TONAL
            "GLASS" -> FROSTED
            "NOTHING" -> DOT_MATRIX
            else -> entries.firstOrNull { it.name == name } ?: Default
        }
    }
}

const val MAX_ACCOUNT_DISPLAY_NAME_LENGTH = 50

fun normalizedAccountDisplayName(value: String): String {
    val normalized = value.trim()
    require(normalized.isNotEmpty()) { "Account name cannot be empty" }
    require(normalized.length <= MAX_ACCOUNT_DISPLAY_NAME_LENGTH) {
        "Account name cannot exceed $MAX_ACCOUNT_DISPLAY_NAME_LENGTH characters"
    }
    return normalized
}

@Immutable
data class ProviderDescriptor(
    val id: ProviderId,
    val displayName: String,
    val supportsMultipleAccounts: Boolean,
    val supportedMetrics: Set<UsageMetricKind>,
)

@Immutable
data class ProviderAccount(
    val id: ProviderAccountId,
    val providerId: ProviderId,
    val displayName: String,
    val identityLabel: String?,
    val planLabel: String?,
    val authenticationState: AuthenticationState = AuthenticationState.CONNECTED,
)

@Serializable
@Immutable
data class UsageWindow(
    val kind: UsageMetricKind,
    val label: String,
    val usedPercent: Double?,
    val remainingPercent: Double?,
    val resetsAtEpochSeconds: Long?,
    val windowSeconds: Int?,
)

@Serializable
@Immutable
data class CreditMetric(
    val availableCount: Int?,
    val totalEarnedCount: Int?,
    val earliestExpiryEpochSeconds: Long?,
)

@Serializable
@Immutable
data class ProviderUsageSnapshot(
    val providerId: ProviderId,
    val accountId: ProviderAccountId,
    val windows: List<UsageWindow>,
    val credits: CreditMetric?,
    val fetchedAtEpochMillis: Long,
    val freshness: DataFreshness,
    val errorMessage: String? = null,
    val resetDetails: List<ResetCredit>? = null,
    val planLabel: String? = null,
    val retryAtEpochMillis: Long = 0,
    val resetsError: String? = null,
)

@Immutable
data class UsageWidgetConfiguration(
    val appWidgetId: Int,
    val providerId: ProviderId,
    val accountId: ProviderAccountId,
    val visualStyle: WidgetVisualStyle,
)

fun Double?.remainingPercent(): Double? = this?.let { (100.0 - it).coerceIn(0.0, 100.0) }

@Serializable
data class ResetCredit(
    val status: String,
    val title: String,
    @kotlinx.serialization.SerialName("granted_at") val grantedAt: String = "",
    @kotlinx.serialization.SerialName("expires_at") val expiresAt: String = "",
    @kotlinx.serialization.SerialName("redeem_started_at") val redeemStartedAt: String = "",
    @kotlinx.serialization.SerialName("redeemed_at") val redeemedAt: String = "",
)

@Serializable
data class DashboardSettings(
    val usageDisplay: String = "used",
    val barFill: String = "left",
    val barOrder: String = "bar_percent_reset",
    val showPercent: Boolean = true,
    val showReset: Boolean = true,
    val showBar: Boolean = true,
    val colorTheme: String = "default",
    val autoRefreshSeconds: Int = 60,
    val compactMode: Boolean = false,
) {
    companion object {
        val barOrders = listOf("bar_percent_reset", "bar_reset_percent", "percent_bar_reset",
            "percent_reset_bar", "reset_bar_percent", "reset_percent_bar")
        val refreshIntervals = listOf(0, 30, 60, 300, 900)
    }
}

/** Matches the TUI thresholds; colors always describe consumed quota. */
enum class UsageSeverity { GOOD, WARNING, BAD }
fun usageSeverity(usedPercent: Double?, cached: Boolean = false): UsageSeverity = when {
    cached -> UsageSeverity.WARNING
    (usedPercent ?: 0.0) >= 65.0 -> UsageSeverity.BAD
    (usedPercent ?: 0.0) >= 50.0 -> UsageSeverity.WARNING
    else -> UsageSeverity.GOOD
}
