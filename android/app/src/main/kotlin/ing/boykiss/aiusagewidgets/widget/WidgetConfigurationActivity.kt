package ing.boykiss.aiusagewidgets.widget

import android.app.Activity
import android.appwidget.AppWidgetManager
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.lifecycleScope
import ing.boykiss.aiusagewidgets.UsageWidgetsApplication
import ing.boykiss.aiusagewidgets.data.database.AccountEntity
import ing.boykiss.aiusagewidgets.data.database.WidgetConfigurationEntity
import ing.boykiss.aiusagewidgets.domain.WidgetVisualStyle
import ing.boykiss.aiusagewidgets.sync.UsageSyncWorker
import ing.boykiss.aiusagewidgets.ui.theme.UsageWidgetsTheme
import ing.boykiss.aiusagewidgets.ui.theme.NothingFont
import kotlinx.coroutines.launch

class WidgetConfigurationActivity : ComponentActivity() {
    private var appWidgetId = AppWidgetManager.INVALID_APPWIDGET_ID
    private var accounts by mutableStateOf<List<AccountEntity>>(emptyList())
    private var selected by mutableStateOf<AccountEntity?>(null)
    private var style by mutableStateOf(WidgetVisualStyle.PIXEL)
    private var reconfiguring by mutableStateOf(false)
    private var loaded by mutableStateOf(false)
    private var saving by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setResult(Activity.RESULT_CANCELED)
        appWidgetId = intent?.getIntExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, AppWidgetManager.INVALID_APPWIDGET_ID)
            ?: AppWidgetManager.INVALID_APPWIDGET_ID
        if (appWidgetId == AppWidgetManager.INVALID_APPWIDGET_ID) { finish(); return }
        val app = application as UsageWidgetsApplication
        lifecycleScope.launch {
            accounts = app.container.database.dao().accounts()
            val existing = app.container.database.dao().widgetConfiguration(appWidgetId)
            reconfiguring = existing != null
            selected = existing?.let { configuration -> accounts.firstOrNull { it.id == configuration.accountId } }
                ?: accounts.firstOrNull()
            style = existing?.visualStyle
                ?.let { runCatching { WidgetVisualStyle.valueOf(it) }.getOrNull() }
                ?: WidgetVisualStyle.PIXEL
            if (style == WidgetVisualStyle.NOTHING && !NothingFont.isAvailable()) style = WidgetVisualStyle.PIXEL
            loaded = true
        }
        setContent {
            UsageWidgetsTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background,
                    contentColor = MaterialTheme.colorScheme.onBackground,
                ) {
                    Column(
                        Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().padding(24.dp),
                        verticalArrangement = Arrangement.spacedBy(20.dp),
                    ) {
                        Text("Configure widget", style = MaterialTheme.typography.headlineMedium)
                        Text("Account", style = MaterialTheme.typography.titleMedium)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            accounts.forEach { account ->
                                FilterChip(
                                    selected = selected?.id == account.id,
                                    onClick = { selected = account },
                                    label = { Text(account.displayName) },
                                    enabled = loaded && !saving,
                                )
                            }
                        }
                        if (accounts.isEmpty()) Text("Connect an account in AI Usage Widgets first.")
                        Text("Style", style = MaterialTheme.typography.titleMedium)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            WidgetVisualStyle.entries.filter { it != WidgetVisualStyle.NOTHING || NothingFont.isAvailable() }.forEach { option ->
                                FilterChip(
                                    selected = style == option,
                                    onClick = { style = option },
                                    label = { Text(option.name.replace('_', ' ')) },
                                    enabled = loaded && !saving,
                                    colors = FilterChipDefaults.filterChipColors(
                                        labelColor = MaterialTheme.colorScheme.onSurface,
                                        selectedLabelColor = MaterialTheme.colorScheme.onSecondaryContainer,
                                    ),
                                )
                            }
                        }
                        Button(enabled = loaded && selected != null && !saving, onClick = { save() }) {
                            Text(if (reconfiguring) "Save widget" else "Add widget")
                        }
                    }
                }
            }
        }
    }

    private fun save() {
        if (!loaded || saving) return
        val account = selected ?: return
        saving = true
        val selectedStyle = style
        val app = application as UsageWidgetsApplication
        lifecycleScope.launch {
            app.container.database.dao().upsertWidgetConfiguration(
                WidgetConfigurationEntity(appWidgetId, account.providerId, account.id, selectedStyle.name)
            )
            UsageSyncWorker.schedule(this@WidgetConfigurationActivity, account.providerId, account.id)
            setResult(Activity.RESULT_OK, Intent().putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, appWidgetId))
            finish()
            // Render from the application scope after the launcher has accepted RESULT_OK.
            // The activity scope is cancelled as soon as finish() completes.
            app.renderWidgetAfterConfiguration(appWidgetId)
        }
    }
}
