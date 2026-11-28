package ing.boykiss.aiusagewidgets.ui.dashboard

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ing.boykiss.aiusagewidgets.R
import ing.boykiss.aiusagewidgets.domain.*
import kotlin.math.roundToInt

@Composable
internal fun UsageAccounts(state: DashboardState, now: Long, onEvent: (DashboardEvent) -> Unit) {
    val tiles = state.layout == DashboardLayout.TILES
    val fontScale = LocalDensity.current.fontScale.coerceAtLeast(1f)
    val groups = remember(state.snapshots) {
        state.snapshots.mapValues { (_, snapshot) -> groupedUsage(snapshot.providerId, snapshot.windows) }
    }
    Column(Modifier.fillMaxSize()) {
        Text(
            "Quota left · Time until reset",
            Modifier.padding(start = 16.dp, end = 16.dp, bottom = 8.dp),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        LazyVerticalGrid(
            columns = if (tiles) GridCells.Adaptive(144.dp * fontScale) else GridCells.Fixed(1),
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(start = 12.dp, end = 12.dp, bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(if (tiles) 8.dp else 0.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(
                items = state.accounts,
                key = { it.id.value },
                span = { account ->
                    GridItemSpan(if (tiles && !groups[account.id.value].isNullOrEmpty()) minOf(2, maxLineSpan) else 1)
                },
            ) { account ->
                UsageAccount(
                    account = account,
                    providerName = state.providers.firstOrNull { it.id == account.providerId }?.displayName
                        ?: account.providerId.value,
                    snapshot = state.snapshots[account.id.value],
                    groups = groups[account.id.value].orEmpty(),
                    now = now,
                    tiles = tiles,
                    canManage = state.canManageAccounts,
                    onEvent = onEvent,
                )
            }
        }
    }
}

@Composable
private fun UsageAccount(
    account: ProviderAccount,
    providerName: String,
    snapshot: ProviderUsageSnapshot?,
    groups: List<UsageGroup>,
    now: Long,
    tiles: Boolean,
    canManage: Boolean,
    onEvent: (DashboardEvent) -> Unit,
) {
    val isBalance = account.providerId.value == "deepseek"
    val updateTime = snapshot?.fetchedAtEpochMillis?.takeIf { it > 0 }
    val updated = updateTime?.let { updatedLabel(it, now) }
    val saved = snapshot != null && (snapshot.freshness != DataFreshness.FRESH ||
        (updateTime != null && now - updateTime > 30 * 60_000L))
    val fontScale = LocalDensity.current.fontScale.coerceAtLeast(1f)
    val identity: @Composable () -> Unit = {
        AccountIdentity(account, providerName, snapshot, isBalance, updated, canManage, onEvent)
    }
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = if (tiles) MaterialTheme.shapes.medium else RectangleShape,
        color = if (tiles) MaterialTheme.colorScheme.surfaceContainerHighest else MaterialTheme.colorScheme.background,
    ) {
        Column {
            BoxWithConstraints(Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = 5.dp)) {
                val inlineColumns = maxWidth >= 280.dp * fontScale
                Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    when {
                        groups.isNotEmpty() -> {
                            identity()
                            GroupedLimits(groups, now, tiles, sideBySide = inlineColumns)
                        }
                        snapshot != null -> AccountLine(sideBySide = !tiles && inlineColumns, label = identity) {
                            if (isBalance) {
                                Text("Available balance", style = MaterialTheme.typography.bodySmall)
                                Text(
                                    snapshot.planLabel ?: account.planLabel ?: "—",
                                    style = MaterialTheme.typography.titleSmall,
                                    color = MaterialTheme.colorScheme.primary,
                                )
                            }
                            UsageLimits(snapshot.windows, null, now, tiles, columns = if (tiles) 1 else 2)
                            if (snapshot.windows.isEmpty() && !isBalance && snapshot.errorMessage == null) {
                                Text("No usage windows reported", style = MaterialTheme.typography.bodySmall)
                            }
                        }
                        else -> {
                            identity()
                            Text("Loading usage…", style = MaterialTheme.typography.bodySmall)
                            LinearProgressIndicator(Modifier.fillMaxWidth())
                        }
                    }
                    if (account.authenticationState == AuthenticationState.SIGN_IN_REQUIRED) {
                        TextButton(onClick = { onEvent(DashboardEvent.Reauthenticate(account)) }) {
                            Text("Sign in to update usage")
                        }
                    }
                    snapshot?.errorMessage?.let {
                        Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
                    }
                    if (saved) {
                        Text(
                            listOfNotNull("Saved usage", updated).joinToString(" · "),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            if (!tiles) HorizontalDivider()
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun GroupedLimits(groups: List<UsageGroup>, now: Long, tiles: Boolean, sideBySide: Boolean) {
    if (tiles) {
        FlowRow(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
            maxItemsInEachRow = if (sideBySide) 2 else 1,
        ) {
            for (group in groups) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(group.name, style = MaterialTheme.typography.labelMedium)
                    UsageLimits(group.windows, group, now, tiles = true, columns = 1)
                }
            }
        }
    } else {
        for (group in groups) {
            AccountLine(sideBySide, label = { Text(group.name, style = MaterialTheme.typography.labelMedium) }) {
                UsageLimits(group.windows, group, now, tiles = false, columns = 2)
            }
        }
    }
}

@Composable
private fun AccountLine(
    sideBySide: Boolean,
    label: @Composable () -> Unit,
    content: @Composable () -> Unit,
) {
    if (sideBySide) {
        Row(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(Modifier.weight(2f)) { label() }
            Column(Modifier.weight(3f)) { content() }
        }
    } else {
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            label()
            content()
        }
    }
}

@Composable
private fun AccountIdentity(
    account: ProviderAccount,
    providerName: String,
    snapshot: ProviderUsageSnapshot?,
    isBalance: Boolean,
    updated: String?,
    canManage: Boolean,
    onEvent: (DashboardEvent) -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    val hasCredits = account.providerId.hasResetCredits
    val credits = snapshot?.credits?.availableCount
    val creditLabel = when (credits) {
        null -> "Reset credits unknown"
        1 -> "1 reset credit"
        else -> "$credits reset credits"
    }
    val metadata = listOfNotNull(
        providerName.takeUnless { account.displayName.equals(it, ignoreCase = true) },
        (snapshot?.planLabel ?: account.planLabel).takeUnless { isBalance },
        creditLabel.takeIf { hasCredits },
    ).joinToString(" · ")
    Box(Modifier.fillMaxWidth()) {
        Column(
            Modifier.fillMaxWidth()
                .clickable(role = Role.Button, onClickLabel = "Manage ${account.displayName}") { menu = true }
                .heightIn(min = 48.dp),
            verticalArrangement = Arrangement.Center,
        ) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(account.displayName, Modifier.weight(1f), style = MaterialTheme.typography.titleSmall)
                Icon(painterResource(R.drawable.ic_more_vert), contentDescription = null, modifier = Modifier.size(16.dp))
            }
            if (metadata.isNotEmpty()) {
                Text(metadata, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            account.identityLabel?.let {
                Text(
                    it, style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
            }
        }
        DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
            if (account.identityLabel != null || updated != null) {
                Column(Modifier.widthIn(max = 280.dp).padding(horizontal = 12.dp, vertical = 8.dp)) {
                    account.identityLabel?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                    updated?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                }
                HorizontalDivider()
            }
            if (hasCredits) {
                DropdownMenuItem(text = { Text("Reset credits") }, onClick = {
                    menu = false
                    onEvent(DashboardEvent.OpenResets(account))
                })
            }
            DropdownMenuItem(enabled = canManage, text = { Text("Rename shared account") }, onClick = {
                menu = false
                onEvent(DashboardEvent.StartRenamingAccount(account))
            })
            DropdownMenuItem(text = { Text("Manage on server") }, onClick = {
                menu = false
                onEvent(DashboardEvent.Reauthenticate(account))
            })
            DropdownMenuItem(enabled = canManage, text = { Text("Remove shared account") }, onClick = {
                menu = false
                onEvent(DashboardEvent.StartRemovingAccount(account))
            })
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun UsageLimits(
    windows: List<UsageWindow>,
    group: UsageGroup?,
    now: Long,
    tiles: Boolean,
    columns: Int,
) {
    FlowRow(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
        maxItemsInEachRow = columns,
    ) {
        windows.forEach { window ->
            UsageLimit(Modifier.weight(1f), window, group, now, tiles)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun UsageLimit(
    modifier: Modifier,
    window: UsageWindow,
    group: UsageGroup?,
    now: Long,
    tiles: Boolean,
) {
    val remaining = window.remainingPercent
    val blocked = group?.isBlocked(window) == true
    val label = if (group != null) when (window.kind) {
        UsageMetricKind.SHORT_WINDOW -> "5-hour"
        UsageMetricKind.LONG_WINDOW -> "Weekly"
        UsageMetricKind.MONTHLY_WINDOW -> "Monthly"
        UsageMetricKind.RESET_CREDITS -> window.label
    } else when (window.label.uppercase()) {
        "5H" -> "5-hour"
        "7D", "WEEKLY" -> "Weekly"
        "SESSION" -> "Session"
        "MONTHLY" -> "Monthly"
        else -> window.label
    }
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val color = when {
        remaining == null || blocked -> muted
        remaining <= 20 -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.primary
    }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = muted)
            Text(
                remaining?.roundToInt()?.let { "$it%" } ?: "—",
                style = MaterialTheme.typography.labelMedium,
                fontWeight = FontWeight.Bold,
                color = color,
                textDecoration = if (blocked) TextDecoration.LineThrough else TextDecoration.None,
            )
        }
        if (tiles && remaining != null && !blocked) {
            LinearProgressIndicator(
                progress = { (remaining / 100).toFloat().coerceIn(0f, 1f) },
                modifier = Modifier.fillMaxWidth().height(3.dp),
                color = color,
            )
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            if (blocked) {
                Text(
                    "Blocked", Modifier.clearAndSetSemantics { contentDescription = "Blocked until weekly reset" },
                    style = MaterialTheme.typography.bodySmall, color = muted,
                )
            }
            if (remaining == null) {
                Text("Not reported", style = MaterialTheme.typography.bodySmall, color = muted)
            }
            window.resetsAtEpochSeconds?.let { epoch ->
                val reset = resetText(epoch, now)
                Text(
                    reset.removePrefix("Resets in "),
                    Modifier.clearAndSetSemantics { contentDescription = reset },
                    style = MaterialTheme.typography.bodySmall,
                    color = muted,
                )
            }
        }
    }
}

private fun updatedLabel(fetchedAt: Long, now: Long): String {
    val minutes = ((now - fetchedAt) / 60_000L).coerceAtLeast(0)
    return if (minutes == 0L) "Updated just now" else "Updated ${minutes}m ago"
}
