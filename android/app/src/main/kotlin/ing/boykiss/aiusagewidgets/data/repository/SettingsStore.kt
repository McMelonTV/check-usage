package ing.boykiss.aiusagewidgets.data.repository

import android.content.Context
import ing.boykiss.aiusagewidgets.domain.DashboardSettings
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

class SettingsStore(context: Context) {
    private val preferences = context.getSharedPreferences("dashboard_settings", Context.MODE_PRIVATE)
    private val json = Json { ignoreUnknownKeys = true }
    private val mutable = MutableStateFlow(runCatching {
        json.decodeFromString<DashboardSettings>(preferences.getString("settings", null) ?: "{}")
    }.getOrDefault(DashboardSettings()))
    val state = mutable.asStateFlow()
    fun save(settings: DashboardSettings) {
        check(settings.usageDisplay in listOf("used", "remaining"))
        check(settings.barFill in listOf("left", "right"))
        check(settings.barOrder in DashboardSettings.barOrders)
        check(settings.colorTheme in listOf("default", "colorblind", "monochrome"))
        check(settings.autoRefreshSeconds in DashboardSettings.refreshIntervals)
        preferences.edit().putString("settings", json.encodeToString(settings)).apply()
        mutable.value = settings
    }
}
