package ing.boykiss.aiusagewidgets

import android.content.Context
import ing.boykiss.aiusagewidgets.data.credentials.CredentialStore
import ing.boykiss.aiusagewidgets.data.database.UsageWidgetsDatabase
import ing.boykiss.aiusagewidgets.data.repository.UsageRepository
import ing.boykiss.aiusagewidgets.providers.api.ProviderRegistry
import ing.boykiss.aiusagewidgets.providers.codex.CodexApiClient
import ing.boykiss.aiusagewidgets.providers.codex.CodexUsageProvider
import java.util.concurrent.TimeUnit
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient

class AppContainer(context: Context) {
    val database = UsageWidgetsDatabase.create(context)
    private val credentialStore = CredentialStore(context)
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }
    private val http = OkHttpClient.Builder()
        .connectTimeout(20, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()
    private val codex = CodexUsageProvider(CodexApiClient(http, json), credentialStore)
    val providers = ProviderRegistry(listOf(codex))
    val repository = UsageRepository(database.dao(), providers)
}
