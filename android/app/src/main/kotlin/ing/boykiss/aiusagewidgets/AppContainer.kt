package ing.boykiss.aiusagewidgets

import android.content.Context
import ing.boykiss.aiusagewidgets.data.database.UsageWidgetsDatabase
import ing.boykiss.aiusagewidgets.data.repository.UsageRepository
import ing.boykiss.aiusagewidgets.data.server.ServerClient
import ing.boykiss.aiusagewidgets.data.server.ServerConnectionStore
import ing.boykiss.aiusagewidgets.data.server.ServerUsageProvider
import ing.boykiss.aiusagewidgets.providers.api.ProviderRegistry
import ing.boykiss.aiusagewidgets.sync.UsageSyncWorker
import kotlinx.serialization.json.Json

class AppContainer(context: Context) {
    val database = UsageWidgetsDatabase.create(context)
    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }
    val server = ServerClient(ServerConnectionStore(context), json)
    val providers = ProviderRegistry(listOf(
        "codex" to "Codex", "claude" to "Claude", "cursor" to "Cursor",
        "opencode-go" to "OpenCode", "deepseek" to "DeepSeek", "antigravity" to "Antigravity",
        "fireworks" to "Fireworks Nexus",
    ).map { (id, name) -> ServerUsageProvider(id, name, server) })
    val repository = UsageRepository(database.dao(), providers, server)

    init { UsageSyncWorker.scheduleCatalog(context) }
}
