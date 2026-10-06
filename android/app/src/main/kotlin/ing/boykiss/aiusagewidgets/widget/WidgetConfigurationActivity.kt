package ing.boykiss.aiusagewidgets.widget

import android.app.Activity
import android.appwidget.AppWidgetManager
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.lifecycle.lifecycleScope
import ing.boykiss.aiusagewidgets.UsageWidgetsApplication
import ing.boykiss.aiusagewidgets.data.database.AccountEntity
import ing.boykiss.aiusagewidgets.data.database.WidgetConfigurationEntity
import ing.boykiss.aiusagewidgets.domain.WidgetVisualStyle
import ing.boykiss.aiusagewidgets.sync.UsageSyncWorker
import ing.boykiss.aiusagewidgets.ui.theme.NothingFont
import ing.boykiss.aiusagewidgets.ui.theme.UsageWidgetsTheme
import kotlinx.coroutines.launch

class WidgetConfigurationActivity : ComponentActivity() {
    private var appWidgetId = AppWidgetManager.INVALID_APPWIDGET_ID
    private var accounts by mutableStateOf<List<AccountEntity>>(emptyList())
    private var selected by mutableStateOf<AccountEntity?>(null)
    private var style by mutableStateOf(WidgetVisualStyle.Default)
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
            style = WidgetVisualStyle.fromStoredName(existing?.visualStyle)
            if (style == WidgetVisualStyle.DOT_MATRIX && !NothingFont.isAvailable()) style = WidgetVisualStyle.Default
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
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
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
                        FlowRow(
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                            verticalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            WidgetVisualStyle.entries.filter { it != WidgetVisualStyle.DOT_MATRIX || NothingFont.isAvailable() }.forEach { option ->
                                FilterChip(
                                    selected = style == option,
                                    onClick = { style = option },
                                    label = { Text(option.displayName) },
                                    leadingIcon = { StyleSwatch(option) },
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

/** A small dot previewing a style's background and accent colors. */
@Composable
private fun StyleSwatch(style: WidgetVisualStyle) {
    val (background, accent) = when (style) {
        WidgetVisualStyle.TONAL -> MaterialTheme.colorScheme.secondaryContainer to MaterialTheme.colorScheme.primary
        WidgetVisualStyle.FROSTED -> Color(0xFF9AAEB3) to Color.White
        WidgetVisualStyle.MIDNIGHT -> Color(0xFF181C40) to Color(0xFF7CF0C5)
        WidgetVisualStyle.SUNSET -> Color(0xFFEA4C6B) to Color(0xFFFFE0B8)
        WidgetVisualStyle.DOT_MATRIX -> Color(0xFF0A0A0A) to Color(0xFFFF3B30)
    }
    Box(
        Modifier.size(18.dp).clip(CircleShape).background(background),
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.size(7.dp).clip(CircleShape).background(accent))
    }
}
