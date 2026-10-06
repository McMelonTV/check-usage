package ing.boykiss.aiusagewidgets.widget

import android.content.Context
import android.os.Build
import androidx.annotation.DrawableRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.doublePreferencesKey
import androidx.datastore.preferences.core.intPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.glance.ColorFilter
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalSize
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
import androidx.glance.color.ColorProvider as DayNightColorProvider
import androidx.glance.currentState
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
import androidx.glance.state.PreferencesGlanceStateDefinition
import androidx.glance.text.FontFamily
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import ing.boykiss.aiusagewidgets.MainActivity
import ing.boykiss.aiusagewidgets.R
import ing.boykiss.aiusagewidgets.UsageWidgetsApplication
import ing.boykiss.aiusagewidgets.domain.ProviderId
import ing.boykiss.aiusagewidgets.domain.DataFreshness
import ing.boykiss.aiusagewidgets.domain.AuthenticationState
import ing.boykiss.aiusagewidgets.domain.UsageWindow
import kotlinx.serialization.json.Json
import kotlinx.serialization.encodeToString
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

    // Exact sizing: bar fills are drawn in dp, so they need the real widget width rather than
    // the nearest responsive bucket, which can be far smaller on large-screen launchers.
    override val sizeMode: SizeMode = SizeMode.Exact

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val appWidgetId = GlanceAppWidgetManager(context).getAppWidgetId(id)
        // This is the fallback for a fresh Glance session. While a session is alive,
        // configuration and usage changes arrive through observable Glance state below.
        val initialState = loadWidgetRenderState(context, appWidgetId)
        provideContent {
            GlanceTheme {
                val preferences = currentState<Preferences>()
                val renderState = if (preferences.contains(HasConfigurationKey)) {
                    preferences.toWidgetRenderState()
                } else {
                    initialState
                }
                if (renderState == null) {
                    EmptyWidget()
                } else {
                    UsageWidgetContent(renderState, LocalSize.current, useSystemNdot = NothingFont.isAvailable())
                }
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
    val metricsJson: String = "[]",
    val balance: String = "",
    val statusMessage: String = "",
    val generic: Boolean = false,
)

private val RenderMetricsKey = stringPreferencesKey("render_metrics")
private val RenderBalanceKey = stringPreferencesKey("render_balance")
private val RenderStatusKey = stringPreferencesKey("render_status")
private val RenderGenericKey = booleanPreferencesKey("render_generic")
private val widgetJson = Json { ignoreUnknownKeys = true }

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
        visualStyle = this[RenderVisualStyleKey] ?: WidgetVisualStyle.Default.name,
        shortPercent = this[RenderShortPercentKey],
        longPercent = this[RenderLongPercentKey],
        resetAt = this[RenderResetAtKey],
        resetCount = this[RenderResetCountKey],
        metricsJson = this[RenderMetricsKey] ?: "[]",
        balance = this[RenderBalanceKey].orEmpty(),
        statusMessage = this[RenderStatusKey].orEmpty(),
        generic = this[RenderGenericKey] ?: false,
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
        metricsJson = widgetJson.encodeToString(snapshot?.windows.orEmpty()),
        balance = if (config.providerId == "deepseek") snapshot?.planLabel.orEmpty() else "",
        statusMessage = when {
            account.authenticationState == AuthenticationState.SIGN_IN_REQUIRED.name -> "Sign in again"
            snapshot == null -> "Refresh to load usage"
            snapshot.errorMessage != null -> "Refresh failed · cached usage"
            snapshot.freshness == DataFreshness.STALE -> "Cached usage"
            else -> ""
        },
        generic = config.providerId != "codex",
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
                    preferences[RenderMetricsKey] = renderState.metricsJson
                    preferences[RenderBalanceKey] = renderState.balance
                    preferences[RenderStatusKey] = renderState.statusMessage
                    preferences[RenderGenericKey] = renderState.generic
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

private enum class WidgetLayout { COMPACT, SQUARE, WIDE, STACKED, LARGE }

// Below this the row layout's bar and countdown would clip, so only figures are shown.
private val ExpandedMinHeight = 140.dp
private val LargeMinWidth = 180.dp
// The large layout's content needs ~245dp; shorter widgets keep the row layouts.
private val LargeMinHeight = 260.dp
// Widgets this big have room to stack a bar per usage window.
private val StackedMinWidth = 150.dp
private val StackedMinHeight = 180.dp
private const val LowRemainingPercent = 20.0

private fun layoutFor(size: DpSize): WidgetLayout = when {
    size.height >= LargeMinHeight && size.width >= LargeMinWidth -> WidgetLayout.LARGE
    size.height < ExpandedMinHeight -> WidgetLayout.COMPACT
    size.height >= StackedMinHeight && size.width >= StackedMinWidth -> WidgetLayout.STACKED
    size.width < size.height * 1.45f -> WidgetLayout.SQUARE
    else -> WidgetLayout.WIDE
}

private data class UsageMetric(val label: String, val percent: Double?)

private data class WidgetPalette(
    val background: ColorProvider,
    @param:DrawableRes val backgroundImage: Int?,
    val foreground: ColorProvider,
    val muted: ColorProvider,
    val accent: ColorProvider,
    val secondaryAccent: ColorProvider,
    val track: ColorProvider,
    val warning: ColorProvider,
    val chip: ColorProvider,
    val radius: Dp,
    val dotMatrix: Boolean = false,
) {
    val displayFont: FontFamily? get() = if (dotMatrix) NDotFontFamily else null
    val labelFont: FontFamily? get() = if (dotMatrix) NDotAllFontFamily else null

    fun text(value: String) = if (dotMatrix) value.uppercase() else value

    fun barColor(percent: Double?, color: ColorProvider) =
        if (percent != null && percent < LowRemainingPercent) warning else color
}

private fun color(argb: Long) = ColorProvider(Color(argb))

/** A color that follows the launcher's light/dark mode. */
private fun color(day: Long, night: Long) = DayNightColorProvider(Color(day), Color(night))

@Composable
private fun palette(style: WidgetVisualStyle): WidgetPalette = when (style) {
    WidgetVisualStyle.TONAL -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        // Wallpaper-derived Material colors, following the system light/dark setting.
        val colors = GlanceTheme.colors
        WidgetPalette(
            background = colors.widgetBackground, backgroundImage = null,
            foreground = colors.onSurface, muted = colors.onSurfaceVariant,
            accent = colors.primary, secondaryAccent = colors.tertiary,
            track = colors.secondaryContainer, warning = colors.error, chip = colors.secondaryContainer,
            radius = 28.dp,
        )
    } else {
        WidgetPalette(
            background = color(0xFFE2EBF5, night = 0xFF1A222B), backgroundImage = null,
            foreground = color(0xFF17212B, night = 0xFFE3EAF2), muted = color(0xFF526170, night = 0xFF9DADBD),
            accent = color(0xFF174F83, night = 0xFF9CCBFB), secondaryAccent = color(0xFF4F6C86, night = 0xFFB4C8DC),
            track = color(0xFFC3D2E0, night = 0xFF2E3A47), warning = color(0xFFB3261E, night = 0xFFFFB4AB),
            chip = color(0xFFC9D8E6, night = 0xFF2A3541),
            radius = 28.dp,
        )
    }
    WidgetVisualStyle.FROSTED -> WidgetPalette(
        background = color(0xD99AAEB3), backgroundImage = R.drawable.widget_background_frosted,
        foreground = color(0xFFFFFFFF), muted = color(0xD9FFFFFF),
        accent = color(0xFFFFFFFF), secondaryAccent = color(0xFFD7ECFF),
        track = color(0x33FFFFFF), warning = color(0xFFFFD27A), chip = color(0x2EFFFFFF),
        radius = 30.dp,
    )
    WidgetVisualStyle.MIDNIGHT -> WidgetPalette(
        background = color(0xFF151935), backgroundImage = R.drawable.widget_background_midnight,
        foreground = color(0xFFF2F4FF), muted = color(0xFF9AA3C7),
        accent = color(0xFF7CF0C5), secondaryAccent = color(0xFF9AA8FF),
        track = color(0xFF2B3263), warning = color(0xFFFF7A8A), chip = color(0xFF262D5A),
        radius = 28.dp,
    )
    WidgetVisualStyle.SUNSET -> WidgetPalette(
        background = color(0xFFE2466E), backgroundImage = R.drawable.widget_background_sunset,
        foreground = color(0xFFFFFFFF), muted = color(0xE6FFF1EA),
        accent = color(0xFFFFFFFF), secondaryAccent = color(0xFFFFE0B8),
        track = color(0x40FFFFFF), warning = color(0xFFFFE45C), chip = color(0x33FFFFFF),
        radius = 28.dp,
    )
    WidgetVisualStyle.DOT_MATRIX -> WidgetPalette(
        background = color(0xFF0A0A0A), backgroundImage = null,
        foreground = color(0xFFFFFFFF), muted = color(0xFFA8A8A8),
        accent = color(0xFFFF3B30), secondaryAccent = color(0xFFE6E6E6),
        track = color(0xFF2A2A2A), warning = color(0xFFFF3B30), chip = color(0xFF1F1F1F),
        radius = 16.dp, dotMatrix = true,
    )
}

@Composable
private fun UsageWidgetContent(state: WidgetRenderState, size: DpSize, useSystemNdot: Boolean) {
    val configuredStyle = WidgetVisualStyle.fromStoredName(state.visualStyle)
    // The dot-matrix style depends on the NDot system fonts; elsewhere it falls back to the default.
    val style = if (configuredStyle == WidgetVisualStyle.DOT_MATRIX && !useSystemNdot) WidgetVisualStyle.Default else configuredStyle
    val p = palette(style)
    val layout = layoutFor(size)
    val long = state.longPercent?.let { UsageMetric("7D", it) }
    val short = state.shortPercent?.let { UsageMetric("5H", it) }
    val featured = long ?: short ?: UsageMetric("7D", null)
    val secondary = if (long != null) short else null
    val horizontalPadding = when (layout) {
        WidgetLayout.COMPACT, WidgetLayout.SQUARE -> 14.dp
        WidgetLayout.WIDE, WidgetLayout.STACKED -> 18.dp
        WidgetLayout.LARGE -> 20.dp
    }
    val barWidth = size.width - horizontalPadding * 2
    val background = p.backgroundImage?.let { GlanceModifier.background(ImageProvider(it)) }
        ?: GlanceModifier.background(p.background)
    Column(
        GlanceModifier.fillMaxSize().appWidgetBackground().then(background).cornerRadius(p.radius)
            .clickable(actionStartActivity<MainActivity>())
            .padding(
                horizontal = horizontalPadding,
                vertical = if (layout == WidgetLayout.LARGE) 18.dp else if (layout == WidgetLayout.COMPACT) 10.dp else 12.dp,
            ),
    ) {
        Header(state, p, layout, narrow = size.width < 200.dp)
        Spacer(GlanceModifier.defaultWeight())
        if (state.generic) {
            GenericWidgetBody(state, p, barWidth, layout)
        } else when (layout) {
            // Compact cards have no countdown line, so wide ones show the time to reset inline.
            WidgetLayout.COMPACT -> MetricRow(
                featured, secondary, p, heroSize = 22.sp,
                resetIn = state.resetAt?.takeIf { size.width >= 200.dp }?.let(::timeUntil),
            )
            WidgetLayout.SQUARE -> SquareBody(featured, secondary, p, barWidth)
            WidgetLayout.WIDE -> Column(GlanceModifier.fillMaxWidth()) {
                MetricRow(featured, secondary, p, heroSize = 30.sp)
                Spacer(GlanceModifier.height(8.dp))
                UsageBar(featured.percent, p.barColor(featured.percent, p.accent), p, 7.dp, barWidth)
            }
            WidgetLayout.STACKED -> StackedBody(featured, secondary, p, barWidth, large = false, narrow = size.width < 240.dp)
            WidgetLayout.LARGE -> StackedBody(featured, secondary, p, barWidth, large = true, resetCount = state.resetCount)
        }
        if (layout != WidgetLayout.COMPACT) {
            Spacer(GlanceModifier.defaultWeight())
            Text(
                p.text(state.statusMessage.ifBlank { if (state.balance.isNotBlank()) "Balance" else countdown(state.resetAt) }),
                style = TextStyle(
                    color = p.muted,
                    fontSize = if (layout == WidgetLayout.LARGE) 13.sp else 11.sp,
                    fontFamily = p.labelFont,
                ),
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun GenericWidgetBody(state: WidgetRenderState, p: WidgetPalette, barWidth: Dp, layout: WidgetLayout) {
    val windows = runCatching { widgetJson.decodeFromString<List<UsageWindow>>(state.metricsJson) }.getOrDefault(emptyList())
    if (state.balance.isNotBlank()) {
        Text(p.text(state.balance), style = TextStyle(p.foreground, 22.sp, FontWeight.Bold, fontFamily = p.displayFont), maxLines = 2)
    } else if (windows.isEmpty()) {
        Text(p.text(state.statusMessage.ifBlank { "Usage unavailable" }), style = TextStyle(p.muted, 14.sp), maxLines = 2)
    } else {
        Column(GlanceModifier.fillMaxWidth()) {
            windows.take(if (layout == WidgetLayout.COMPACT) 2 else 3).forEach { window ->
                Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text(p.text("${window.label} left"), GlanceModifier.defaultWeight(), style = TextStyle(p.muted, 11.sp, fontFamily = p.labelFont), maxLines = 1)
                    PercentText(window.remainingPercent, p, if (layout == WidgetLayout.COMPACT) 16.sp else 20.sp)
                }
                if (layout != WidgetLayout.COMPACT) {
                    Spacer(GlanceModifier.height(4.dp))
                    UsageBar(window.remainingPercent, p.barColor(window.remainingPercent, p.accent), p, 5.dp, barWidth)
                    if (layout == WidgetLayout.LARGE || layout == WidgetLayout.STACKED) {
                        Text(p.text(countdown(window.resetsAtEpochSeconds)), style = TextStyle(p.muted, 10.sp), maxLines = 1)
                    }
                }
                Spacer(GlanceModifier.height(6.dp))
            }
        }
    }
}

@Composable
private fun Header(state: WidgetRenderState, p: WidgetPalette, layout: WidgetLayout, narrow: Boolean) {
    val large = layout == WidgetLayout.LARGE
    val credits = state.resetCount?.takeIf { it > 0 }
    // A pill beside the name leaves it no room on narrow widgets, so the count joins the provider
    // line there. The large layout shows its pill beside the hero figure instead.
    val inlineResets = credits != null && narrow && !large
    Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(GlanceModifier.defaultWeight()) {
            Text(
                p.text(state.accountName),
                style = TextStyle(p.foreground, if (large) 17.sp else 14.sp, FontWeight.Bold, fontFamily = p.displayFont),
                maxLines = 1,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(GlanceModifier.size(6.dp).background(p.accent).cornerRadius(3.dp)) {}
                Spacer(GlanceModifier.width(5.dp))
                Text(
                    p.text(if (inlineResets) "${state.providerName} · ${resetsLabel(credits)}" else state.providerName),
                    style = TextStyle(p.muted, if (large) 12.sp else 10.sp, fontFamily = p.labelFont),
                    maxLines = 1,
                )
            }
        }
        if (credits != null && !large && !inlineResets) {
            ResetPill(credits, p, showLabel = true, large = false)
            Spacer(GlanceModifier.width(6.dp))
        }
        val buttonSize = if (large) 36.dp else 30.dp
        Box(
            GlanceModifier.size(buttonSize).background(p.chip).cornerRadius(buttonSize / 2)
                .clickable(actionRunCallback<RefreshAction>(actionParametersOf(AccountIdKey to state.accountId))),
            contentAlignment = Alignment.Center,
        ) {
            Image(
                provider = ImageProvider(R.drawable.ic_widget_refresh),
                contentDescription = "Refresh usage",
                modifier = GlanceModifier.size(if (large) 20.dp else 16.dp),
                colorFilter = ColorFilter.tint(p.foreground),
            )
        }
    }
}

@Composable
private fun ResetPill(count: Int, p: WidgetPalette, showLabel: Boolean, large: Boolean) {
    Row(
        GlanceModifier.background(p.chip).cornerRadius(12.dp)
            .padding(horizontal = if (large) 11.dp else 9.dp, vertical = if (large) 6.dp else 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            count.toString(),
            style = TextStyle(p.foreground, if (large) 15.sp else 13.sp, FontWeight.Bold, fontFamily = p.displayFont),
        )
        if (showLabel) {
            Spacer(GlanceModifier.width(4.dp))
            Text(
                p.text(resetsLabel(count, withCount = false)),
                style = TextStyle(p.muted, if (large) 12.sp else 11.sp, FontWeight.Medium, fontFamily = p.labelFont),
            )
        }
    }
}

/** Percentage with a smaller, baseline-aligned percent sign. */
@Composable
private fun PercentText(percent: Double?, p: WidgetPalette, size: TextUnit, color: ColorProvider = p.foreground) {
    Row(verticalAlignment = Alignment.Bottom) {
        Text(
            percent?.roundToInt()?.toString() ?: "–",
            style = TextStyle(color, size, FontWeight.Bold, fontFamily = p.displayFont),
        )
        if (percent != null) {
            Text(
                "%",
                style = TextStyle(color, size * 0.5f, FontWeight.Bold, fontFamily = p.displayFont),
                // Both texts are bottom-aligned, so lift the smaller one onto the larger baseline.
                modifier = GlanceModifier.padding(bottom = (size.value * 0.1f).dp),
            )
        }
    }
}

@Composable
private fun MetricLabel(
    metric: UsageMetric,
    p: WidgetPalette,
    size: TextUnit,
    modifier: GlanceModifier = GlanceModifier,
    short: Boolean = false,
) {
    Text(
        p.text("${metric.label} ${if (short) "left" else "remaining"}"),
        style = TextStyle(p.muted, size, FontWeight.Medium, fontFamily = p.labelFont),
        maxLines = 1,
        modifier = modifier,
    )
}

/** One or two windows side by side, each as a figure over its label. */
@Composable
private fun MetricRow(
    featured: UsageMetric,
    secondary: UsageMetric?,
    p: WidgetPalette,
    heroSize: TextUnit,
    resetIn: String? = null,
) {
    Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom) {
        listOfNotNull(featured, secondary).forEach { metric ->
            Column(GlanceModifier.defaultWeight().padding(end = 8.dp)) {
                PercentText(metric.percent, p, heroSize)
                // Three columns leave little room, and the dot-matrix font runs wide.
                MetricLabel(metric, p, 10.sp, short = resetIn != null)
            }
        }
        if (resetIn != null) {
            Column(horizontalAlignment = Alignment.End) {
                Text(
                    p.text(resetIn),
                    style = TextStyle(p.foreground, 15.sp, FontWeight.Bold, fontFamily = p.displayFont),
                    maxLines = 1,
                )
                Text(
                    p.text("until reset"),
                    style = TextStyle(p.muted, 10.sp, FontWeight.Medium, fontFamily = p.labelFont),
                    maxLines = 1,
                )
            }
        }
    }
}

@Composable
private fun SquareBody(featured: UsageMetric, secondary: UsageMetric?, p: WidgetPalette, barWidth: Dp) {
    Column(GlanceModifier.fillMaxWidth()) {
        Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom) {
            Box(GlanceModifier.defaultWeight()) { PercentText(featured.percent, p, 32.sp) }
            if (secondary != null) {
                Column(horizontalAlignment = Alignment.End) {
                    PercentText(secondary.percent, p, 15.sp, p.muted)
                    Text(p.text(secondary.label), style = TextStyle(p.muted, 10.sp, fontFamily = p.labelFont))
                }
            }
        }
        MetricLabel(featured, p, 10.sp)
        Spacer(GlanceModifier.height(6.dp))
        UsageBar(featured.percent, p.barColor(featured.percent, p.accent), p, 7.dp, barWidth)
    }
}

/** The featured window as a hero figure with a bar, and the other window as a slimmer bar below it. */
@Composable
private fun StackedBody(
    featured: UsageMetric,
    secondary: UsageMetric?,
    p: WidgetPalette,
    barWidth: Dp,
    large: Boolean,
    resetCount: Int? = null,
    narrow: Boolean = false,
) {
    Column(GlanceModifier.fillMaxWidth()) {
        if (large) {
            Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom) {
                Column(GlanceModifier.defaultWeight()) {
                    PercentText(featured.percent, p, 52.sp)
                    MetricLabel(featured, p, 12.sp)
                }
                if (resetCount != null && resetCount > 0) ResetPill(resetCount, p, showLabel = true, large = true)
            }
        } else if (narrow) {
            PercentText(featured.percent, p, 30.sp)
            MetricLabel(featured, p, 11.sp)
        } else {
            Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.Bottom) {
                PercentText(featured.percent, p, 30.sp)
                Spacer(GlanceModifier.width(8.dp))
                MetricLabel(featured, p, 11.sp, GlanceModifier.defaultWeight().padding(bottom = 5.dp))
            }
        }
        Spacer(GlanceModifier.height(if (large) 10.dp else 6.dp))
        UsageBar(featured.percent, p.barColor(featured.percent, p.accent), p, if (large) 10.dp else 8.dp, barWidth)
        if (secondary != null) {
            Spacer(GlanceModifier.height(if (large) 16.dp else 10.dp))
            Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                MetricLabel(secondary, p, if (large) 12.sp else 11.sp, GlanceModifier.defaultWeight())
                PercentText(secondary.percent, p, if (large) 16.sp else 14.sp)
            }
            Spacer(GlanceModifier.height(if (large) 6.dp else 4.dp))
            UsageBar(secondary.percent, p.barColor(secondary.percent, p.secondaryAccent), p, if (large) 6.dp else 5.dp, barWidth)
        }
    }
}

@Composable
private fun UsageBar(percent: Double?, color: ColorProvider, p: WidgetPalette, thickness: Dp, maxWidth: Dp) {
    val fraction = ((percent ?: 0.0) / 100.0).coerceIn(0.0, 1.0)
    if (p.dotMatrix) {
        SegmentedBar(fraction, color, p, thickness)
        return
    }
    Box(GlanceModifier.fillMaxWidth().height(thickness).background(p.track).cornerRadius(thickness / 2)) {
        if (fraction > 0.0) {
            // Never narrower than the bar is tall, so a nearly empty window still reads as a dot.
            val fill = (maxWidth.value * fraction).dp.coerceAtLeast(thickness)
            Box(GlanceModifier.width(fill).height(thickness).background(color).cornerRadius(thickness / 2)) {}
        }
    }
}

private const val BarSegments = 10

@Composable
private fun SegmentedBar(fraction: Double, color: ColorProvider, p: WidgetPalette, thickness: Dp) {
    val gap = 3.dp
    val lit = (fraction * BarSegments).roundToInt().coerceIn(if (fraction > 0.0) 1 else 0, BarSegments)
    // Equal weights fill the width exactly; each segment carries its own trailing gap because
    // Glance rows hold at most ten children.
    Row(GlanceModifier.fillMaxWidth()) {
        repeat(BarSegments) { index ->
            val last = index == BarSegments - 1
            Box(GlanceModifier.defaultWeight().height(thickness).padding(end = if (last) 0.dp else gap)) {
                Box(
                    GlanceModifier.fillMaxSize().background(if (index < lit) color else p.track)
                        .cornerRadius(thickness / 2),
                ) {}
            }
        }
    }
}

private val NDotFontFamily = FontFamily("ndot")
private val NDotAllFontFamily = FontFamily("NDot55All")

@Composable private fun EmptyWidget() {
    Box(
        GlanceModifier.fillMaxSize().appWidgetBackground().background(ColorProvider(Color(0xFF202124))).cornerRadius(28.dp)
            .clickable(actionStartActivity<MainActivity>()).padding(20.dp),
        contentAlignment = Alignment.Center,
    ) { Text("Choose an account", style = TextStyle(ColorProvider(Color.White), fontSize = 16.sp, fontWeight = FontWeight.Bold)) }
}

private fun countdown(epoch: Long?): String {
    if (epoch == null) return "Reset time unavailable"
    return timeUntil(epoch)?.let { "Resets in $it" } ?: "Refresh needed"
}

private fun resetsLabel(count: Int, withCount: Boolean = true): String {
    val noun = if (count == 1) "reset" else "resets"
    return if (withCount) "$count $noun" else noun
}

/** Compact time until [epoch], e.g. "2d 4h", "5h" or "40m"; null once it has passed. */
private fun timeUntil(epoch: Long): String? {
    val duration = Duration.between(Instant.now(), Instant.ofEpochSecond(epoch))
    if (duration.isNegative || duration.isZero) return null
    val days = duration.toDays()
    val hours = duration.minusDays(days).toHours()
    return when {
        days > 0 -> "${days}d ${hours}h"
        hours > 0 -> "${hours}h"
        else -> "${duration.toMinutes().coerceAtLeast(1)}m"
    }
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
