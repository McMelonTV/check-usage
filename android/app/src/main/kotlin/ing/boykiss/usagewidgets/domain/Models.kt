package ing.boykiss.usagewidgets.domain

import androidx.compose.runtime.Immutable

@JvmInline value class ProviderId(val value: String)
@JvmInline value class ProviderAccountId(val value: String)

enum class UsageMetricKind { SHORT_WINDOW, LONG_WINDOW, RESET_CREDITS }
enum class AuthenticationState { CONNECTED, SIGN_IN_REQUIRED }
enum class DataFreshness { FRESH, STALE, ERROR }
enum class WidgetVisualStyle { NOTHING, GLASS, PIXEL }

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

@Immutable
data class UsageWindow(
    val kind: UsageMetricKind,
    val label: String,
    val usedPercent: Double?,
    val remainingPercent: Double?,
    val resetsAtEpochSeconds: Long?,
    val windowSeconds: Int?,
)

@Immutable
data class CreditMetric(
    val availableCount: Int?,
    val totalEarnedCount: Int?,
    val earliestExpiryEpochSeconds: Long?,
)

@Immutable
data class ProviderUsageSnapshot(
    val providerId: ProviderId,
    val accountId: ProviderAccountId,
    val windows: List<UsageWindow>,
    val credits: CreditMetric?,
    val fetchedAtEpochMillis: Long,
    val freshness: DataFreshness,
    val errorMessage: String? = null,
)

@Immutable
data class UsageWidgetConfiguration(
    val appWidgetId: Int,
    val providerId: ProviderId,
    val accountId: ProviderAccountId,
    val visualStyle: WidgetVisualStyle,
)

fun Double?.remainingPercent(): Double? = this?.let { (100.0 - it).coerceIn(0.0, 100.0) }
