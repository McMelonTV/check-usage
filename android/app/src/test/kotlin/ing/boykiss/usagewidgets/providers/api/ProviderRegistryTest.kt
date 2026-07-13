package ing.boykiss.usagewidgets.providers.api

import ing.boykiss.usagewidgets.domain.ProviderAccount
import ing.boykiss.usagewidgets.domain.ProviderDescriptor
import ing.boykiss.usagewidgets.domain.ProviderId
import ing.boykiss.usagewidgets.domain.ProviderUsageSnapshot
import org.junit.Assert.assertEquals
import org.junit.Test

class ProviderRegistryTest {
    @Test fun providersAreResolvedByStableId() {
        val provider = object : UsageProvider {
            override val descriptor = ProviderDescriptor(ProviderId("fixture"), "Fixture", true, emptySet())
            override val authenticator = object : ProviderAuthenticator {
                override suspend fun beginAuthentication() = error("unused")
                override suspend fun pollAuthentication(session: AuthenticationSession) = error("unused")
                override suspend fun refreshCredentials(account: ProviderAccount) = Unit
                override suspend fun removeCredentials(accountId: String) = Unit
            }
            override val usageSource = object : ProviderUsageSource {
                override suspend fun fetchUsage(account: ProviderAccount): ProviderUsageSnapshot = error("unused")
            }
        }
        assertEquals(provider, ProviderRegistry(listOf(provider)).require(ProviderId("fixture")))
    }
}
