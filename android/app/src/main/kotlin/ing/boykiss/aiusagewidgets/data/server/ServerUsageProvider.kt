package ing.boykiss.aiusagewidgets.data.server

import ing.boykiss.aiusagewidgets.domain.*
import ing.boykiss.aiusagewidgets.providers.api.*

// This registry supplies display metadata only. The server owns every login.
class ServerUsageProvider(id: String, name: String, private val server: ServerClient) : UsageProvider {
    override val descriptor = ProviderDescriptor(ProviderId(id), name, true,
        if (id == "deepseek") emptySet() else setOf(UsageMetricKind.SHORT_WINDOW, UsageMetricKind.LONG_WINDOW, UsageMetricKind.MONTHLY_WINDOW))
    override val usageSource = object : ProviderUsageSource {
        override suspend fun fetchUsage(account: ProviderAccount) = server.usage(account.id.value).single().toDomain()
    }
    override val authenticator = object : ProviderAuthenticator {
        override suspend fun beginAuthentication(): AuthenticationSession = serverLogin()
        override suspend fun pollAuthentication(session: AuthenticationSession): AuthenticationProgress = serverLogin()
        override suspend fun refreshCredentials(account: ProviderAccount): Unit = serverLogin()
        override suspend fun removeCredentials(accountId: String): Unit = serverLogin()
    }
    private fun serverLogin(): Nothing = error("Manage provider logins on the usage server")
}
