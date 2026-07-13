package ing.boykiss.aiusagewidgets.widget

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.doublePreferencesKey
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalSize
import androidx.glance.currentState
import androidx.glance.action.ActionParameters
import androidx.glance.action.actionParametersOf
import androidx.glance.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.ActionCallback
import androidx.glance.appwidget.action.actionRunCallback
import androidx.glance.appwidget.appWidgetBackground
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.state.updateAppWidgetState
import androidx.glance.background
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.FontFamily
import androidx.glance.text.Text
import androidx.glance.text.TextAlign
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import androidx.glance.state.PreferencesGlanceStateDefinition
import ing.boykiss.aiusagewidgets.MainActivity
import ing.boykiss.aiusagewidgets.R
import ing.boykiss.aiusagewidgets.UsageWidgetsApplication
import ing.boykiss.aiusagewidgets.domain.ProviderId
import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import ing.boykiss.aiusagewidgets.domain.WidgetVisualStyle
import ing.boykiss.aiusagewidgets.sync.UsageSyncWorker
import ing.boykiss.aiusagewidgets.ui.theme.NothingFont
import java.time.Duration
import java.time.Instant
import java.util.concurrent.ConcurrentHashMap
import kotlin.math.roundToInt
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

class UsageWidget : GlanceAppWidget() {
    override val stateDefinition = PreferencesGlanceStateDefinition

    override val sizeMode: SizeMode = SizeMode.Responsive(
        setOf(
            androidx.compose.ui.unit.DpSize(120.dp, 80.dp),
            androidx.compose.ui.unit.DpSize(120.dp, 140.dp),
            androidx.compose.ui.unit.DpSize(150.dp, 140.dp),
            androidx.compose.ui.unit.DpSize(190.dp, 140.dp),
            androidx.compose.ui.unit.DpSize(220.dp, 140.dp),
            androidx.compose.ui.unit.DpSize(300.dp, 140.dp),
            androidx.compose.ui.unit.DpSize(320.dp, 160.dp),
        )
    )

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val appWidgetId = GlanceAppWidgetManager(context).getAppWidgetId(id)
        // This is the fallback for a fresh Glance session. While a session is alive,
        // configuration and usage changes arrive through observable Glance state below.
        val initialState = loadWidgetRenderState(context, appWidgetId)
        provideContent {
            val size = LocalSize.current
            val preferences = currentState<Preferences>()
            val renderState = if (preferences.contains(HasConfigurationKey)) {
                preferences.toWidgetRenderState()
            } else {
                initialState
            }
            if (renderState == null) {
                EmptyWidget()
            } else {
                val expanded = size.height >= 115.dp
                val compact = size.height < 105.dp
                val square = expanded && size.width < size.height * 1.45f
                val horizontalPadding = if (compact || square) 24.dp else 36.dp
                UsageWidgetContent(
                    accountId = renderState.accountId,
                    visualStyle = renderState.visualStyle,
                    accountName = renderState.accountName,
                    providerName = renderState.providerName,
                    shortPercent = renderState.shortPercent,
                    longPercent = renderState.longPercent,
                    resetAt = renderState.resetAt,
                    credits = renderState.resetCount,
                    useSystemNdot = NothingFont.isAvailable(),
                    // A two-row widget has enough vertical room for the reset line even when
                    // it is only three launcher columns wide.
                    expanded = expanded,
                    compact = compact,
                    square = square,
                    progressMaxWidth = size.width - horizontalPadding,
                )
            }
        }
    }
}

private data class WidgetRenderState(
    val accountId: String,
    val accountName: String,
    val providerName: String,
    val visualStyle: String,
    val shortPercent: Double?,
    val longPercent: Double?,
    val resetAt: Long?,
    val resetCount: Int?,
)

private val HasConfigurationKey = booleanPreferencesKey("has_configuration")
private val RenderAccountIdKey = stringPreferencesKey("render_account_id")
private val RenderAccountNameKey = stringPreferencesKey("render_account_name")
private val RenderProviderNameKey = stringPreferencesKey("render_provider_name")
private val RenderVisualStyleKey = stringPreferencesKey("render_visual_style")
private val RenderShortPercentKey = doublePreferencesKey("render_short_percent")
private val RenderLongPercentKey = doublePreferencesKey("render_long_percent")
private val RenderResetAtKey = longPreferencesKey("render_reset_at")
private val RenderResetCountKey = intPreferencesKey("render_reset_count")

private fun Preferences.toWidgetRenderState(): WidgetRenderState? {
    if (this[HasConfigurationKey] != true) return null
    return WidgetRenderState(
        accountId = this[RenderAccountIdKey] ?: return null,
        accountName = this[RenderAccountNameKey] ?: return null,
        providerName = this[RenderProviderNameKey].orEmpty(),
        visualStyle = this[RenderVisualStyleKey] ?: WidgetVisualStyle.PIXEL.name,
        shortPercent = this[RenderShortPercentKey],
        longPercent = this[RenderLongPercentKey],
        resetAt = this[RenderResetAtKey],
        resetCount = this[RenderResetCountKey],
    )
}

private suspend fun loadWidgetRenderState(context: Context, appWidgetId: Int): WidgetRenderState? {
    val app = context.applicationContext as UsageWidgetsApplication
    val config = app.container.database.dao().widgetConfiguration(appWidgetId) ?: return null
    val account = app.container.database.dao().account(config.accountId) ?: return null
    val snapshot = app.container.repository.snapshot(config.accountId)
    val shortWindow = snapshot?.windows?.firstOrNull { it.kind == UsageMetricKind.SHORT_WINDOW }
    val longWindow = snapshot?.windows?.firstOrNull { it.kind == UsageMetricKind.LONG_WINDOW }
    val providerName = runCatching {
        app.container.providers.require(ProviderId(config.providerId)).descriptor.displayName
    }.getOrDefault(config.providerId.replaceFirstChar(Char::titlecase))
    return WidgetRenderState(
        accountId = config.accountId,
        accountName = account.displayName,
        providerName = providerName,
        visualStyle = config.visualStyle,
        shortPercent = shortWindow?.remainingPercent,
        longPercent = longWindow?.remainingPercent,
        resetAt = longWindow?.resetsAtEpochSeconds ?: shortWindow?.resetsAtEpochSeconds,
        resetCount = snapshot?.credits?.availableCount,
    )
}

/** Serializes every update source and publishes fresh data into the live Glance composition. */
internal object WidgetUpdater {
    private val updateMutexes = ConcurrentHashMap<Int, Mutex>()

    suspend fun update(context: Context, glanceId: GlanceId) {
        val appWidgetId = GlanceAppWidgetManager(context).getAppWidgetId(glanceId)
        update(context, appWidgetId, glanceId)
    }

    suspend fun update(context: Context, appWidgetId: Int) {
        val glanceId = GlanceAppWidgetManager(context).getGlanceIdBy(appWidgetId)
        update(context, appWidgetId, glanceId)
    }

    suspend fun updateAll(context: Context) {
        GlanceAppWidgetManager(context).getGlanceIds(UsageWidget::class.java).forEach { glanceId ->
            update(context, glanceId)
        }
    }

    private suspend fun update(context: Context, appWidgetId: Int, glanceId: GlanceId) {
        updateMutexes.computeIfAbsent(appWidgetId) { Mutex() }.withLock {
            val renderState = loadWidgetRenderState(context, appWidgetId)
            updateAppWidgetState(context, glanceId) { preferences ->
                preferences.clear()
                preferences[HasConfigurationKey] = renderState != null
                if (renderState != null) {
                    preferences[RenderAccountIdKey] = renderState.accountId
                    preferences[RenderAccountNameKey] = renderState.accountName
                    preferences[RenderProviderNameKey] = renderState.providerName
                    preferences[RenderVisualStyleKey] = renderState.visualStyle
                    renderState.shortPercent?.let { preferences[RenderShortPercentKey] = it }
                    renderState.longPercent?.let { preferences[RenderLongPercentKey] = it }
                    renderState.resetAt?.let { preferences[RenderResetAtKey] = it }
                    renderState.resetCount?.let { preferences[RenderResetCountKey] = it }
                }
            }
            UsageWidget().update(context, glanceId)
        }
    }
}

private data class WidgetPalette(
    val background: ColorProvider,
    val foreground: ColorProvider,
    val muted: ColorProvider,
    val accent: ColorProvider,
    val track: ColorProvider,
    val radius: androidx.compose.ui.unit.Dp,
)

private fun palette(style: WidgetVisualStyle): WidgetPalette = when (style) {
    WidgetVisualStyle.NOTHING -> WidgetPalette(
        ColorProvider(Color(0xFF090909)), ColorProvider(Color.White), ColorProvider(Color(0xFFBDBDBD)),
        ColorProvider(Color(0xFFFF3B30)), ColorProvider(Color(0xFF353535)), 16.dp,
    )
    WidgetVisualStyle.GLASS -> WidgetPalette(
        ColorProvider(Color(0xD99AAEB3)), ColorProvider(Color.White), ColorProvider(Color(0xFFE3ECEE)),
        ColorProvider(Color.White), ColorProvider(Color(0xFF536F7A)), 34.dp,
    )
    WidgetVisualStyle.PIXEL -> WidgetPalette(
        ColorProvider(Color(0xFFE2EBF5)), ColorProvider(Color(0xFF17212B)),
        ColorProvider(Color(0xFF526170)), ColorProvider(Color(0xFF174F83)), ColorProvider(Color(0xFF9BAFC0)), 28.dp,
    )
}

@Composable
private fun UsageWidgetContent(
    accountId: String,
    visualStyle: String,
    accountName: String,
    providerName: String,
    shortPercent: Double?,
    longPercent: Double?,
    resetAt: Long?,
    credits: Int?,
    useSystemNdot: Boolean,
    expanded: Boolean,
    compact: Boolean,
    square: Boolean,
    progressMaxWidth: androidx.compose.ui.unit.Dp,
) {
    val configuredStyle = runCatching { WidgetVisualStyle.valueOf(visualStyle) }.getOrDefault(WidgetVisualStyle.PIXEL)
    val style = if (configuredStyle == WidgetVisualStyle.NOTHING && !useSystemNdot) WidgetVisualStyle.PIXEL else configuredStyle
    val p = palette(style)
    val params = actionParametersOf(AccountIdKey to accountId)
    val featuredPercent = longPercent ?: shortPercent
    val featuredLabel = if (longPercent != null) "7D" else "5H"
    Column(
        GlanceModifier.fillMaxSize().appWidgetBackground().background(p.background).cornerRadius(p.radius)
            .clickable(actionStartActivity<MainActivity>())
            .padding(
                start = if (compact || square) 12.dp else 18.dp,
                top = if (compact || square) 10.dp else 12.dp,
                end = if (compact || square) 12.dp else 18.dp,
                bottom = if (square) 8.dp else 10.dp,
            ),
    ) {
        Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(GlanceModifier.defaultWeight()) {
                if (style == WidgetVisualStyle.NOTHING) {
                    Text(
                        accountName.uppercase(),
                        style = TextStyle(
                            color = p.foreground,
                            fontSize = 13.sp,
                            fontWeight = FontWeight.Bold,
                            fontFamily = if (useSystemNdot) NDotFontFamily else FontFamily.Monospace,
                        ),
                        maxLines = 1,
                    )
                    Text(
                        providerName,
                        style = TextStyle(
                            color = p.muted,
                            fontSize = 9.sp,
                            fontFamily = if (useSystemNdot) NDotAllFontFamily else FontFamily.Monospace,
                        ),
                        maxLines = 1,
                    )
                } else {
                    Text(
                        accountName,
                        style = TextStyle(p.foreground, fontSize = 14.sp, fontWeight = FontWeight.Bold),
                        maxLines = 1,
                    )
                    Text(providerName, style = TextStyle(p.muted, fontSize = 9.sp), maxLines = 1)
                }
            }
            Box(
                modifier = GlanceModifier.size(if (square) 28.dp else 30.dp).background(p.track)
                    .cornerRadius(if (square) 14.dp else 15.dp)
                    .clickable(actionRunCallback<RefreshAction>(params)),
                contentAlignment = Alignment.Center,
            ) {
                Image(
                    provider = ImageProvider(refreshIcon(style)),
                    contentDescription = "Refresh usage",
                    modifier = GlanceModifier.size(if (square) 16.dp else 17.dp),
                )
            }
        }
        if (square) {
            Spacer(GlanceModifier.defaultWeight())
        } else if (expanded) {
            Spacer(GlanceModifier.defaultWeight())
        } else {
            Spacer(GlanceModifier.height(if (compact) 4.dp else 6.dp))
        }
        Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            if (square) {
                Metric(
                    featuredLabel,
                    featuredPercent,
                    p,
                    false,
                    style,
                    useSystemNdot,
                    GlanceModifier.defaultWeight(),
                    showLabel = false,
                )
                if (credits != null && credits > 0) {
                    Spacer(GlanceModifier.width(8.dp))
                    ResetCount(credits, p, true, style, useSystemNdot)
                }
            } else {
                if (shortPercent != null) {
                    Metric("5H", shortPercent, p, compact, style, useSystemNdot, GlanceModifier.defaultWeight())
                    Spacer(GlanceModifier.width(10.dp))
                }
                if (longPercent != null) {
                    Metric("7D", longPercent, p, compact, style, useSystemNdot, GlanceModifier.defaultWeight())
                    Spacer(GlanceModifier.width(10.dp))
                }
                if (credits != null && credits > 0) {
                    ResetCount(credits, p, compact, style, useSystemNdot)
                }
            }
        }
        if (!compact) {
            if (square) {
                Spacer(GlanceModifier.defaultWeight())
                Column(GlanceModifier.fillMaxWidth()) {
                    Text(
                        "$featuredLabel Usage Remaining",
                        style = TextStyle(
                            color = p.muted,
                            fontSize = 10.sp,
                            fontWeight = FontWeight.Medium,
                            fontFamily = if (style == WidgetVisualStyle.NOTHING && useSystemNdot) NDotAllFontFamily else FontFamily.SansSerif,
                        ),
                        maxLines = 1,
                    )
                    Spacer(GlanceModifier.height(3.dp))
                    UsageProgressBar(featuredPercent, p, square = true, progressMaxWidth = progressMaxWidth)
                }
            } else {
                Spacer(GlanceModifier.height(8.dp))
                UsageProgressBar(featuredPercent, p, square = false, progressMaxWidth = progressMaxWidth)
            }
        }
        if (expanded) {
            if (square) {
                Spacer(GlanceModifier.defaultWeight())
            } else {
                Spacer(GlanceModifier.defaultWeight())
            }
            Text(
                countdown(resetAt),
                style = TextStyle(
                    color = p.muted,
                    fontSize = if (square) 11.sp else 12.sp,
                    fontFamily = if (style == WidgetVisualStyle.NOTHING && useSystemNdot) NDotAllFontFamily else FontFamily.SansSerif,
                    textAlign = if (square) TextAlign.Center else TextAlign.Start,
                ),
                modifier = if (square) GlanceModifier.fillMaxWidth() else GlanceModifier,
            )
        }
    }
}

@Composable
private fun UsageProgressBar(
    percent: Double?,
    p: WidgetPalette,
    square: Boolean,
    progressMaxWidth: androidx.compose.ui.unit.Dp,
) {
    Box(
        GlanceModifier.fillMaxWidth().height(if (square) 6.dp else 7.dp)
            .background(p.track).cornerRadius(if (square) 3.dp else 4.dp),
    ) {
        val fraction = ((percent ?: 0.0) / 100.0).coerceIn(0.05, 1.0)
        Box(
            GlanceModifier.width((progressMaxWidth.value * fraction).dp).height(if (square) 6.dp else 7.dp)
                .background(p.accent).cornerRadius(if (square) 3.dp else 4.dp),
        ) {}
    }
}

@Composable
private fun ResetCount(
    count: Int?,
    p: WidgetPalette,
    compact: Boolean,
    style: WidgetVisualStyle,
    useSystemNdot: Boolean,
) {
    Column(horizontalAlignment = Alignment.Horizontal.CenterHorizontally) {
        if (style == WidgetVisualStyle.NOTHING) {
            if (useSystemNdot) {
                Text(
                    (count ?: 0).toString(),
                    style = TextStyle(p.foreground, if (compact) 26.sp else 32.sp, fontFamily = NDotFontFamily),
                )
                Text("RESETS", style = TextStyle(p.muted, 9.sp, fontFamily = NDotFontFamily))
            } else {
                DotMatrixText((count ?: 0).toString(), p.foreground, ColorProvider(Color(0xFF303030)), if (compact) 2.dp else 3.dp)
                Spacer(GlanceModifier.height(3.dp))
                DotMatrixText("RESETS", p.muted, ColorProvider(Color(0xFF252525)), 1.dp)
            }
        } else {
            Text((count ?: 0).toString(), style = TextStyle(p.foreground, fontSize = if (compact) 22.sp else 28.sp, fontWeight = FontWeight.Bold))
            Text("RESETS", style = TextStyle(p.muted, fontSize = 9.sp, fontWeight = FontWeight.Medium))
        }
    }
}

@Composable
private fun Metric(
    label: String,
    percent: Double?,
    p: WidgetPalette,
    compact: Boolean,
    style: WidgetVisualStyle,
    useSystemNdot: Boolean,
    modifier: GlanceModifier,
    showLabel: Boolean = true,
) {
    val fullLabel = "$label Usage Remaining"
    Column(modifier) {
        if (style == WidgetVisualStyle.NOTHING) {
            if (useSystemNdot) {
                Text(
                    percent?.let { "${it.roundToInt()}%" } ?: "-",
                    style = TextStyle(p.foreground, if (compact) 28.sp else 36.sp, fontFamily = NDotFontFamily),
                )
                if (showLabel) {
                    Text(
                        fullLabel,
                        style = TextStyle(p.muted, 10.sp, fontFamily = NDotAllFontFamily),
                        maxLines = 1,
                    )
                }
            } else {
                DotMatrixText(
                    percent?.let { "${it.roundToInt()}%" } ?: "-",
                    p.foreground,
                    ColorProvider(Color(0xFF303030)),
                    if (compact) 2.dp else 3.dp,
                )
                if (showLabel) {
                    Spacer(GlanceModifier.height(3.dp))
                    Text(
                        fullLabel,
                        style = TextStyle(p.muted, 10.sp, fontFamily = FontFamily.Monospace),
                        maxLines = 1,
                    )
                }
            }
        } else {
            Text(
                percent?.let { "${it.roundToInt()}%" } ?: "—",
                style = TextStyle(p.foreground, fontSize = if (compact) 22.sp else 32.sp, fontWeight = FontWeight.Bold),
            )
            if (showLabel) {
                Text(
                    fullLabel,
                    style = TextStyle(p.muted, fontSize = 10.sp, fontWeight = FontWeight.Medium),
                    maxLines = 1,
                )
            }
        }
    }
}

private val NDotFontFamily = FontFamily("ndot")
private val NDotAllFontFamily = FontFamily("NDot55All")

private fun refreshIcon(style: WidgetVisualStyle): Int = when (style) {
    WidgetVisualStyle.NOTHING -> R.drawable.ic_widget_refresh_nothing
    WidgetVisualStyle.GLASS -> R.drawable.ic_widget_refresh_glass
    WidgetVisualStyle.PIXEL -> R.drawable.ic_widget_refresh_pixel
}

/** A launcher-safe 5x7 dot display inspired by Nothing's NDot treatment. */
@Composable
private fun DotMatrixText(
    value: String,
    onColor: ColorProvider,
    offColor: ColorProvider,
    dotSize: androidx.compose.ui.unit.Dp,
    modifier: GlanceModifier = GlanceModifier,
) {
    val text = value.uppercase()
    Column(modifier) {
        repeat(7) { row ->
            Row(GlanceModifier.padding(bottom = if (row < 6) 1.dp else 0.dp)) {
                text.forEach { char ->
                    val bits = DotGlyphs[char] ?: DotGlyphs.getValue(' ')
                    Row(GlanceModifier.padding(end = 2.dp)) {
                        repeat(5) { column ->
                            val on = bits[row] and (1 shl (4 - column)) != 0
                            Box(
                                GlanceModifier.padding(end = 1.dp).size(dotSize)
                                    .background(if (on) onColor else offColor)
                            ) {}
                        }
                    }
                }
            }
        }
    }
}

private val DotGlyphs: Map<Char, IntArray> = mapOf(
    ' ' to intArrayOf(0, 0, 0, 0, 0, 0, 0),
    '-' to intArrayOf(0, 0, 0, 31, 0, 0, 0),
    '%' to intArrayOf(25, 25, 2, 4, 8, 19, 19),
    '0' to intArrayOf(14, 17, 19, 21, 25, 17, 14),
    '1' to intArrayOf(4, 12, 4, 4, 4, 4, 14),
    '2' to intArrayOf(14, 17, 1, 2, 4, 8, 31),
    '3' to intArrayOf(30, 1, 1, 14, 1, 1, 30),
    '4' to intArrayOf(2, 6, 10, 18, 31, 2, 2),
    '5' to intArrayOf(31, 16, 16, 30, 1, 1, 30),
    '6' to intArrayOf(14, 16, 16, 30, 17, 17, 14),
    '7' to intArrayOf(31, 1, 2, 4, 8, 8, 8),
    '8' to intArrayOf(14, 17, 17, 14, 17, 17, 14),
    '9' to intArrayOf(14, 17, 17, 15, 1, 1, 14),
    'A' to intArrayOf(14, 17, 17, 31, 17, 17, 17),
    'B' to intArrayOf(30, 17, 17, 30, 17, 17, 30),
    'C' to intArrayOf(14, 17, 16, 16, 16, 17, 14),
    'D' to intArrayOf(30, 17, 17, 17, 17, 17, 30),
    'E' to intArrayOf(31, 16, 16, 30, 16, 16, 31),
    'F' to intArrayOf(31, 16, 16, 30, 16, 16, 16),
    'G' to intArrayOf(14, 17, 16, 23, 17, 17, 15),
    'H' to intArrayOf(17, 17, 17, 31, 17, 17, 17),
    'I' to intArrayOf(14, 4, 4, 4, 4, 4, 14),
    'J' to intArrayOf(7, 2, 2, 2, 18, 18, 12),
    'K' to intArrayOf(17, 18, 20, 24, 20, 18, 17),
    'L' to intArrayOf(16, 16, 16, 16, 16, 16, 31),
    'M' to intArrayOf(17, 27, 21, 21, 17, 17, 17),
    'N' to intArrayOf(17, 25, 21, 19, 17, 17, 17),
    'O' to intArrayOf(14, 17, 17, 17, 17, 17, 14),
    'P' to intArrayOf(30, 17, 17, 30, 16, 16, 16),
    'Q' to intArrayOf(14, 17, 17, 17, 21, 18, 13),
    'R' to intArrayOf(30, 17, 17, 30, 20, 18, 17),
    'S' to intArrayOf(15, 16, 16, 14, 1, 1, 30),
    'T' to intArrayOf(31, 4, 4, 4, 4, 4, 4),
    'U' to intArrayOf(17, 17, 17, 17, 17, 17, 14),
    'V' to intArrayOf(17, 17, 17, 17, 17, 10, 4),
    'W' to intArrayOf(17, 17, 17, 21, 21, 21, 10),
    'X' to intArrayOf(17, 17, 10, 4, 10, 17, 17),
    'Y' to intArrayOf(17, 17, 10, 4, 4, 4, 4),
    'Z' to intArrayOf(31, 1, 2, 4, 8, 16, 31),
)

@Composable private fun EmptyWidget() {
    Box(
        GlanceModifier.fillMaxSize().appWidgetBackground().background(ColorProvider(Color(0xFF202124))).cornerRadius(28.dp)
            .clickable(actionStartActivity<MainActivity>()).padding(20.dp),
        contentAlignment = Alignment.Center,
    ) { Text("Choose an account", style = TextStyle(ColorProvider(Color.White), fontSize = 16.sp, fontWeight = FontWeight.Bold)) }
}

private fun countdown(epoch: Long?): String {
    if (epoch == null) return "Reset time unavailable"
    val duration = Duration.between(Instant.now(), Instant.ofEpochSecond(epoch))
    if (duration.isNegative || duration.isZero) return "Refresh needed"
    val days = duration.toDays()
    val hours = duration.minusDays(days).toHours()
    return if (days > 0) "Resets in ${days}d ${hours}h" else "Resets in ${duration.toHours()}h"
}

private val AccountIdKey = ActionParameters.Key<String>("account_id")

class RefreshAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        val accountId = parameters[AccountIdKey] ?: return
        val app = context.applicationContext as UsageWidgetsApplication
        val account = app.container.database.dao().account(accountId) ?: return
        UsageSyncWorker.schedule(context, account.providerId, accountId)
        app.container.repository.refresh(accountId)
        WidgetUpdater.update(context, glanceId)
    }
}
