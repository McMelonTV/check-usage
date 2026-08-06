package ing.boykiss.aiusagewidgets.providers.codex

import ing.boykiss.aiusagewidgets.data.credentials.CredentialStore
import ing.boykiss.aiusagewidgets.data.credentials.ProviderCredentials
import ing.boykiss.aiusagewidgets.domain.CreditMetric
import ing.boykiss.aiusagewidgets.domain.DataFreshness
import ing.boykiss.aiusagewidgets.domain.ProviderAccount
import ing.boykiss.aiusagewidgets.domain.ProviderAccountId
import ing.boykiss.aiusagewidgets.domain.ProviderDescriptor
import ing.boykiss.aiusagewidgets.domain.ProviderId
import ing.boykiss.aiusagewidgets.domain.ProviderUsageSnapshot
import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import ing.boykiss.aiusagewidgets.domain.UsageWindow
import ing.boykiss.aiusagewidgets.providers.api.AuthenticationProgress
import ing.boykiss.aiusagewidgets.providers.api.AuthenticationSession
import ing.boykiss.aiusagewidgets.providers.api.ProviderAuthenticator
import ing.boykiss.aiusagewidgets.providers.api.ProviderUsageSource
import ing.boykiss.aiusagewidgets.providers.api.UsageProvider
import java.util.UUID

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
            api.verificationUrl,
            response.interval.toIntOrNull()?.coerceAtLeast(1) ?: 5,
        )
    }

    override suspend fun pollAuthentication(session: AuthenticationSession): AuthenticationProgress {
        val result = api.pollDeviceCode(session.sessionId, session.userCode) ?: return AuthenticationProgress.Pending
        val tokens = api.exchangeCode(result.authorizationCode, result.codeVerifier)
        check(tokens.idToken.isNotBlank() && tokens.refreshToken.isNotBlank()) { "Token response is incomplete" }
        val identity = api.identity(tokens.idToken)
        val localId = UUID.randomUUID().toString()
        credentials.put(
            localId,
            ProviderCredentials(tokens.accessToken, tokens.refreshToken, tokens.idToken, identity.accountId),
        )
        return AuthenticationProgress.Complete(
            ProviderAccount(
                id = ProviderAccountId(localId),
                providerId = descriptor.id,
                displayName = identity.email?.substringBefore('@')?.takeIf(String::isNotBlank) ?: "Codex account",
                identityLabel = identity.email,
                planLabel = identity.planType,
            ),
        )
    }

    override suspend fun refreshCredentials(account: ProviderAccount) {
        freshCredentials(account.id.value)
    }

    override suspend fun removeCredentials(accountId: String) = credentials.remove(accountId)

    override suspend fun fetchUsage(account: ProviderAccount): ProviderUsageSnapshot {
        val tokens = freshCredentials(account.id.value)
        val snapshot = api.snapshot(tokens.accessToken, tokens.remoteAccountId)
        return ProviderUsageSnapshot(
            descriptor.id,
            account.id,
            snapshot.windows.map(GoUsageWindow::toDomain),
            snapshot.credits?.let {
                CreditMetric(it.availableCount, it.totalEarnedCount, it.earliestExpiryEpochSeconds)
            },
            snapshot.fetchedAtEpochMillis,
            DataFreshness.FRESH,
            snapshot.creditsError,
        )
    }

    private suspend fun freshCredentials(accountId: String): ProviderCredentials {
        val current = requireNotNull(credentials.get(accountId)) { "Sign in again" }
        val refresh = api.refreshCredentials(current)
        val updated = refresh.credentials.toProviderCredentials()
        if (refresh.changed) credentials.put(accountId, updated)
        return updated
    }
}

private fun GoCredentials.toProviderCredentials() = ProviderCredentials(
    accessToken,
    refreshToken,
    idToken,
    accountId,
)

private fun GoUsageWindow.toDomain() = UsageWindow(
    kind = goUsageMetricKind(kind),
    label = label,
    usedPercent = usedPercent,
    remainingPercent = remainingPercent,
    resetsAtEpochSeconds = resetAt,
    windowSeconds = windowSeconds,
)

internal fun goUsageMetricKind(kind: String): UsageMetricKind = when (kind) {
    "short_window" -> UsageMetricKind.SHORT_WINDOW
    "long_window" -> UsageMetricKind.LONG_WINDOW
    else -> error("Unknown Go usage metric kind: $kind")
}
