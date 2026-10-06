package ing.boykiss.aiusagewidgets.providers.shared

import ing.boykiss.aiusagewidgets.data.credentials.CredentialStore
import ing.boykiss.aiusagewidgets.data.credentials.ProviderCredentials
import ing.boykiss.aiusagewidgets.domain.*
import ing.boykiss.aiusagewidgets.providers.api.*
import ing.boykiss.aiusagewidgets.providers.codex.AuthenticationRequiredException
import ing.boykiss.aiusagewidgets.gobridge.codexlogic.Codexlogic
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerialName
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.util.UUID

@Serializable private data class BrowserSession(val sessionId: String, val verificationUrl: String)
@Serializable private data class LoginResult(val credentials: ProviderCredentials, val email: String? = null, val plan: String? = null)
@Serializable internal data class SharedMetric(
    val slot: String,
    val label: String,
    @SerialName("used_percent") val used: Double? = null,
    @SerialName("reset_at") val resetAt: Long? = null,
    val scope: String = "",
) {
    fun toDomain() = UsageWindow(
        when (slot) {
            "session" -> UsageMetricKind.SHORT_WINDOW
            "weekly" -> UsageMetricKind.LONG_WINDOW
            "monthly" -> UsageMetricKind.MONTHLY_WINDOW
            else -> error("Unknown metric slot: $slot")
        }, scope.ifBlank { label }, used, used.remainingPercent(), resetAt, null,
    )
}
@Serializable private data class SharedUsage(val plan: String? = null, val metrics: List<SharedMetric> = emptyList())
@Serializable private data class SharedSnapshot(
    val credentials: ProviderCredentials,
    val usage: SharedUsage,
    val fetchedAt: Long,
    val error: String? = null,
    val retryAt: Long = 0,
)

class SharedUsageProvider(
    id: String,
    name: String,
    private val credentials: CredentialStore,
    private val json: Json,
) : UsageProvider, ProviderAuthenticator, ProviderUsageSource {
    override val descriptor = ProviderDescriptor(ProviderId(id), name, true,
        if (id == "deepseek") emptySet() else setOf(UsageMetricKind.SHORT_WINDOW, UsageMetricKind.LONG_WINDOW, UsageMetricKind.MONTHLY_WINDOW))
    override val authenticator: ProviderAuthenticator get() = this
    override val usageSource: ProviderUsageSource get() = this

    override suspend fun beginAuthentication(): AuthenticationSession = bridge {
        val body = Codexlogic.beginProviderLogin(descriptor.id.value)
        val session = json.decodeFromString<BrowserSession>(body)
        AuthenticationSession(session.sessionId, "", session.verificationUrl, 2, descriptor.id,
            requiresCode = descriptor.id.value == "claude", bridgeSession = body)
    }

    override suspend fun pollAuthentication(session: AuthenticationSession): AuthenticationProgress = bridge {
        val body = Codexlogic.completeProviderLogin(descriptor.id.value, session.bridgeSession, "")
        if (body.isEmpty()) AuthenticationProgress.Pending else AuthenticationProgress.Complete(saveLogin(body))
    }

    override suspend fun completeCode(session: AuthenticationSession, code: String): ProviderAccount = bridge {
        saveLogin(Codexlogic.completeProviderLogin(descriptor.id.value, session.bridgeSession, code))
    }

    override suspend fun addAPIKey(key: String): ProviderAccount = bridge {
        Codexlogic.validateAPIKey(descriptor.id.value, key)
        val account = newAccount(null, null)
        credentials.put(account.id.value, ProviderCredentials("", "", "", apiKey = key.trim()))
        account
    }

    private fun saveLogin(body: String): ProviderAccount {
        val result = json.decodeFromString<LoginResult>(body)
        val account = newAccount(result.email, result.plan)
        credentials.put(account.id.value, result.credentials)
        return account
    }

    private fun newAccount(email: String?, plan: String?) = ProviderAccount(
        ProviderAccountId(UUID.randomUUID().toString()), descriptor.id,
        email?.substringBefore('@')?.takeIf(String::isNotBlank) ?: "${descriptor.displayName} account", email, plan,
    )

    override suspend fun refreshCredentials(account: ProviderAccount) { fetchUsage(account) }
    override suspend fun replaceCredentials(fromAccountId: String, toAccountId: String) = credentials.move(fromAccountId, toAccountId)

    override suspend fun removeCredentials(accountId: String) = credentials.remove(accountId)

    override suspend fun fetchUsage(account: ProviderAccount): ProviderUsageSnapshot = bridge {
        val current = credentials.get(account.id.value) ?: throw AuthenticationRequiredException("Sign in again")
        val response = json.decodeFromString<SharedSnapshot>(
            Codexlogic.fetchProviderUsage(descriptor.id.value, json.encodeToString(current)))
        if (response.credentials != current) credentials.put(account.id.value, response.credentials)
        if (response.error?.startsWith("authentication required:") == true) throw AuthenticationRequiredException("Sign in again")
        ProviderUsageSnapshot(descriptor.id, account.id, response.usage.metrics.map(SharedMetric::toDomain), null,
            response.fetchedAt, if (response.error == null) DataFreshness.FRESH else DataFreshness.ERROR,
            response.error, planLabel = response.usage.plan?.takeIf(String::isNotBlank), retryAtEpochMillis = response.retryAt)
    }

    private suspend fun <T> bridge(block: () -> T): T = withContext(Dispatchers.IO) {
        try { block() } catch (error: Exception) {
            if (error.message.orEmpty().startsWith("authentication required:")) throw AuthenticationRequiredException("Sign in again", error)
            throw error
        }
    }
}
