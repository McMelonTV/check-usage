package ing.boykiss.usagewidgets.providers.codex

import android.util.Base64
import ing.boykiss.usagewidgets.data.credentials.CredentialStore
import ing.boykiss.usagewidgets.data.credentials.ProviderCredentials
import ing.boykiss.usagewidgets.domain.CreditMetric
import ing.boykiss.usagewidgets.domain.ProviderAccount
import ing.boykiss.usagewidgets.domain.ProviderAccountId
import ing.boykiss.usagewidgets.domain.ProviderDescriptor
import ing.boykiss.usagewidgets.domain.ProviderId
import ing.boykiss.usagewidgets.domain.ProviderUsageSnapshot
import ing.boykiss.usagewidgets.domain.UsageMetricKind
import ing.boykiss.usagewidgets.domain.UsageWindow
import ing.boykiss.usagewidgets.domain.DataFreshness
import ing.boykiss.usagewidgets.domain.remainingPercent
import ing.boykiss.usagewidgets.providers.api.AuthenticationProgress
import ing.boykiss.usagewidgets.providers.api.AuthenticationSession
import ing.boykiss.usagewidgets.providers.api.ProviderAuthenticator
import ing.boykiss.usagewidgets.providers.api.ProviderUsageSource
import ing.boykiss.usagewidgets.providers.api.UsageProvider
import java.time.Instant
import java.util.UUID
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

class CodexUsageProvider(
    private val api: CodexApiClient,
    private val credentials: CredentialStore,
) : UsageProvider, ProviderAuthenticator, ProviderUsageSource {
    override val descriptor = ProviderDescriptor(
        id = ProviderId("codex"),
        displayName = "Codex",
        supportsMultipleAccounts = true,
        supportedMetrics = UsageMetricKind.entries.toSet(),
    )
    override val authenticator: ProviderAuthenticator get() = this
    override val usageSource: ProviderUsageSource get() = this

    override suspend fun beginAuthentication(): AuthenticationSession {
        val response = api.requestDeviceCode()
        return AuthenticationSession(
            response.deviceAuthId,
            response.userCode,
            CodexApiClient.DEVICE_VERIFICATION_URL,
            response.interval.toIntOrNull()?.coerceAtLeast(1) ?: 5,
        )
    }

    override suspend fun pollAuthentication(session: AuthenticationSession): AuthenticationProgress {
        val result = api.pollDeviceCode(session.sessionId, session.userCode) ?: return AuthenticationProgress.Pending
        val tokens = api.exchangeCode(result.authorizationCode, result.codeVerifier)
        val idToken = requireNotNull(tokens.idToken)
        val claims = claims(idToken)
        val email = claims["email"]
        val authClaims = claims["https://api.openai.com/auth"]?.let { nested ->
            runCatching { Json.parseToJsonElement(nested).jsonObject }.getOrNull()
        }
        val plan = authClaims?.get("chatgpt_plan_type")?.jsonPrimitive?.content ?: claims["chatgpt_plan_type"]
        val remoteAccountId = authClaims?.get("chatgpt_account_id")?.jsonPrimitive?.content ?: claims["chatgpt_account_id"]
        val localId = UUID.randomUUID().toString()
        credentials.put(localId, ProviderCredentials(tokens.accessToken, requireNotNull(tokens.refreshToken), idToken, remoteAccountId))
        return AuthenticationProgress.Complete(
            ProviderAccount(
                id = ProviderAccountId(localId),
                providerId = descriptor.id,
                displayName = email?.substringBefore('@')?.takeIf(String::isNotBlank) ?: "Codex account",
                identityLabel = email,
                planLabel = plan,
            )
        )
    }

    override suspend fun refreshCredentials(account: ProviderAccount) {
        freshCredentials(account.id.value)
    }

    override suspend fun removeCredentials(accountId: String) = credentials.remove(accountId)

    override suspend fun fetchUsage(account: ProviderAccount): ProviderUsageSnapshot {
        val tokens = freshCredentials(account.id.value)
        val usage = api.usage(tokens.accessToken, tokens.remoteAccountId)
        val creditsResult = runCatching { api.resetCredits(tokens.accessToken, tokens.remoteAccountId) }
        val available = creditsResult.getOrNull()
        val earliest = available?.credits
            ?.asSequence()
            ?.filter { it.status.equals("available", ignoreCase = true) }
            ?.mapNotNull { runCatching { Instant.parse(it.expiresAt).epochSecond }.getOrNull() }
            ?.minOrNull()
        return ProviderUsageSnapshot(
            descriptor.id,
            account.id,
            listOfNotNull(
                usage.rateLimit?.primaryWindow?.toWindow(fallbackKind = UsageMetricKind.SHORT_WINDOW),
                usage.rateLimit?.secondaryWindow?.toWindow(fallbackKind = UsageMetricKind.LONG_WINDOW),
            ).distinctBy { it.kind },
            available?.let { CreditMetric(it.availableCount, it.totalEarnedCount, earliest) },
            System.currentTimeMillis(),
            DataFreshness.FRESH,
            creditsResult.exceptionOrNull()?.let { "Reset credits unavailable" },
        )
    }

    private suspend fun freshCredentials(accountId: String): ProviderCredentials {
        val current = requireNotNull(credentials.get(accountId)) { "Sign in again" }
        if (!isExpiring(current.accessToken)) return current
        val refreshed = api.refresh(current.refreshToken)
        return current.copy(
            accessToken = refreshed.accessToken,
            refreshToken = refreshed.refreshToken ?: current.refreshToken,
            idToken = refreshed.idToken ?: current.idToken,
        ).also { credentials.put(accountId, it) }
    }

    private fun isExpiring(token: String): Boolean {
        val exp = claims(token)["exp"]?.toLongOrNull() ?: return true
        return exp <= Instant.now().epochSecond + 60
    }

    private fun claims(token: String): Map<String, String> = runCatching {
        val part = token.split('.')[1]
        val decoded = String(Base64.decode(part, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING))
        Json.parseToJsonElement(decoded).jsonObject.mapValues { (_, value) ->
            if (value is kotlinx.serialization.json.JsonPrimitive) value.content else value.toString()
        }
    }.getOrDefault(emptyMap())

    private fun RateLimitWindow.toWindow(fallbackKind: UsageMetricKind): UsageWindow {
        val kind = codexWindowKind(limitWindowSeconds, fallbackKind)
        val label = when (kind) {
            UsageMetricKind.SHORT_WINDOW -> "5H"
            UsageMetricKind.LONG_WINDOW -> "7D"
            UsageMetricKind.RESET_CREDITS -> error("Credits are not a rate-limit window")
        }
        return UsageWindow(kind, label, usedPercent, usedPercent.remainingPercent(), resetAt, limitWindowSeconds)
    }
}

internal fun codexWindowKind(seconds: Int?, fallback: UsageMetricKind): UsageMetricKind = when (seconds) {
    null -> fallback
    in 1..86_400 -> UsageMetricKind.SHORT_WINDOW
    else -> UsageMetricKind.LONG_WINDOW
}
