package ing.boykiss.aiusagewidgets.providers.api

import ing.boykiss.aiusagewidgets.domain.ProviderAccount
import ing.boykiss.aiusagewidgets.domain.ProviderDescriptor
import ing.boykiss.aiusagewidgets.domain.ProviderId
import ing.boykiss.aiusagewidgets.domain.ProviderUsageSnapshot

data class AuthenticationSession(
    val sessionId: String,
    val userCode: String,
    val verificationUrl: String,
    val pollIntervalSeconds: Int,
)

sealed interface AuthenticationProgress {
    data object Pending : AuthenticationProgress
    data class Complete(val account: ProviderAccount) : AuthenticationProgress
}

interface ProviderAuthenticator {
    suspend fun beginAuthentication(): AuthenticationSession
    suspend fun pollAuthentication(session: AuthenticationSession): AuthenticationProgress
    suspend fun refreshCredentials(account: ProviderAccount)
    suspend fun removeCredentials(accountId: String)
}

interface ProviderUsageSource {
    suspend fun fetchUsage(account: ProviderAccount): ProviderUsageSnapshot
}

interface UsageProvider {
    val descriptor: ProviderDescriptor
    val authenticator: ProviderAuthenticator
    val usageSource: ProviderUsageSource
}

class ProviderRegistry(providers: List<UsageProvider>) {
    private val byId = providers.associateBy { it.descriptor.id }
    fun all(): List<UsageProvider> = byId.values.sortedBy { it.descriptor.displayName }
    fun require(id: ProviderId): UsageProvider = requireNotNull(byId[id]) { "Unknown provider: ${id.value}" }
}
