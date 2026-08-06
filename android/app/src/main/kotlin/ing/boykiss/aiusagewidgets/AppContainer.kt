package ing.boykiss.aiusagewidgets

import android.content.Context
import ing.boykiss.aiusagewidgets.data.credentials.CredentialStore
import ing.boykiss.aiusagewidgets.data.database.UsageWidgetsDatabase
import ing.boykiss.aiusagewidgets.data.repository.UsageRepository
import ing.boykiss.aiusagewidgets.providers.api.ProviderRegistry
import ing.boykiss.aiusagewidgets.providers.codex.CodexApiClient
import ing.boykiss.aiusagewidgets.providers.codex.CodexUsageProvider
import kotlinx.serialization.json.Json

class AppContainer(context: Context) {
    val database = UsageWidgetsDatabase.create(context)
    private val credentialStore = CredentialStore(context)
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }
    private val codex = CodexUsageProvider(CodexApiClient(json), credentialStore)
    val providers = ProviderRegistry(listOf(codex))
    val repository = UsageRepository(database.dao(), providers)
}
