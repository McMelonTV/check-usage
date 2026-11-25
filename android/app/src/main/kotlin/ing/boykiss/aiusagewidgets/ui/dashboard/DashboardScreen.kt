package ing.boykiss.aiusagewidgets.ui.dashboard

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items as gridItems
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.*
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleStartEffect
import ing.boykiss.aiusagewidgets.R
import ing.boykiss.aiusagewidgets.domain.*
import java.time.Duration
import java.time.Instant
import kotlinx.coroutines.delay
import kotlin.math.roundToInt

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DashboardScreen(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    LifecycleStartEffect(Unit) {
        onEvent(DashboardEvent.SetForeground(true))
        onStopOrDispose { onEvent(DashboardEvent.SetForeground(false)) }
    }
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(Unit) { while (true) { delay(30_000); now = System.currentTimeMillis() } }
    Scaffold(
        topBar = {
            TopAppBar(title = { Text("Usage") }, actions = {
                TextButton(onClick = { onEvent(DashboardEvent.ConfigureServer) }) { Text("Server") }
                IconButton(onClick = { onEvent(DashboardEvent.Refresh) }, enabled = !state.refreshing && state.serverConfigured) {
                    Icon(painterResource(R.drawable.ic_widget_refresh), contentDescription = "Refresh usage")
                }
            })
        },
        floatingActionButton = {
            if (!state.loading && state.accounts.isNotEmpty()) ExtendedFloatingActionButton(
                onClick = { onEvent(DashboardEvent.Refresh) },
                icon = { Icon(painterResource(R.drawable.ic_widget_refresh), contentDescription = null) },
                text = { Text("Sync accounts") },
            )
        },
    ) { padding ->
        PullToRefreshBox(isRefreshing = state.refreshing, onRefresh = { onEvent(DashboardEvent.Refresh) },
            modifier = Modifier.fillMaxSize().padding(padding)) {
            when {
                state.loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                state.accounts.isEmpty() -> Column(
                    Modifier.fillMaxSize().padding(24.dp),
                    verticalArrangement = Arrangement.Center,
                ) {
                    Text("Your limits, at a glance", style = MaterialTheme.typography.headlineLarge)
                    Spacer(Modifier.height(12.dp))
                    Text(if (state.serverConfigured) "No accounts are configured on your server. Add your provider logins there, then refresh here." else "Connect to your homelab usage server to synchronize accounts and home-screen widgets.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Spacer(Modifier.height(24.dp))
                    Button(onClick = { onEvent(if (state.serverConfigured) DashboardEvent.Refresh else DashboardEvent.ConfigureServer) }) { Text(if (state.serverConfigured) "Sync accounts" else "Connect to server") }
                }
                else -> LazyVerticalGrid(
                    columns = GridCells.Adaptive(340.dp),
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 20.dp, top = 12.dp, end = 20.dp, bottom = 100.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                    horizontalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    gridItems(state.accounts, key = { it.id.value }) { account ->
                        AccountCard(account, state.providers.firstOrNull { it.id == account.providerId }?.displayName
                            ?: account.providerId.value, state.snapshots[account.id.value], now, state.canManageAccounts, onEvent)
                    }
                }
            }
        }
    }
    state.resetAccount?.let { account -> ResetDetailsSheet(account, state.snapshot, state.refreshingResets, onEvent) }
    AccountDialogs(state, onEvent)
    ServerConnectionDialog(state, onEvent)
    state.notice?.let { notice ->
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.DismissNotice) }, title = { Text("AI Usage Widgets") },
            text = { Text(notice) }, confirmButton = { TextButton(onClick = { onEvent(DashboardEvent.DismissNotice) }) { Text("OK") } })
    }
}

@Composable
private fun AccountCard(account: ProviderAccount, providerName: String, snapshot: ProviderUsageSnapshot?, now: Long, canManageAccounts: Boolean, onEvent: (DashboardEvent) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    val isBalance = account.providerId.value == "deepseek"
    val cached = snapshot != null && (snapshot.freshness != DataFreshness.FRESH || now - snapshot.fetchedAtEpochMillis > 30 * 60_000L)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(account.displayName, style = MaterialTheme.typography.titleLarge)
                    Text(listOfNotNull(providerName, (snapshot?.planLabel ?: account.planLabel).takeUnless { isBalance })
                        .joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    account.identityLabel?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
                Box {
                    IconButton(onClick = { menu = true }) {
                        Icon(painterResource(R.drawable.ic_more_vert), contentDescription = "Manage ${account.displayName}")
                    }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(enabled = canManageAccounts, text = { Text("Rename shared account") }, onClick = { menu = false; onEvent(DashboardEvent.StartRenamingAccount(account)) })
                        DropdownMenuItem(text = { Text("Manage on server") }, onClick = { menu = false; onEvent(DashboardEvent.Reauthenticate(account)) })
                        DropdownMenuItem(enabled = canManageAccounts, text = { Text("Remove shared account") }, onClick = { menu = false; onEvent(DashboardEvent.StartRemovingAccount(account)) })
                    }
                }
            }
            if (account.authenticationState == AuthenticationState.SIGN_IN_REQUIRED) {
                Text("Sign in to update your usage", color = MaterialTheme.colorScheme.error)
                OutlinedButton(onClick = { onEvent(DashboardEvent.Reauthenticate(account)) }) { Text("Sign in again") }
            }
            if (snapshot == null) {
                Text("Loading usage…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                LinearProgressIndicator(Modifier.fillMaxWidth())
            } else {
                if (isBalance) {
                    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text("Available balance", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(snapshot.planLabel ?: account.planLabel ?: "—", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
                    }
                }
                val groups = groupedUsage(account.providerId, snapshot.windows)
                if (groups.isNotEmpty()) UsageGrid(groups, now)
                else snapshot.windows.forEach { window -> UsageMetric(window, now) }
                if (snapshot.windows.isEmpty() && !isBalance && snapshot.errorMessage == null) Text("No usage windows reported")
                snapshot.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                if (snapshot.fetchedAtEpochMillis > 0) Text(
                    "Updated ${ago(snapshot.fetchedAtEpochMillis, now)}${if (cached) " · saved usage" else ""}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (account.providerId.hasResetCredits) {
                Surface(onClick = { onEvent(DashboardEvent.OpenResets(account)) }, shape = MaterialTheme.shapes.medium,
                    color = MaterialTheme.colorScheme.secondaryContainer) {
                    Row(Modifier.fillMaxWidth().padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f)) {
                            Text("Reset credits", style = MaterialTheme.typography.titleSmall)
                            Text("View details", style = MaterialTheme.typography.bodySmall)
                        }
                        Text(snapshot?.credits?.availableCount?.let { "$it available" } ?: "—", style = MaterialTheme.typography.labelLarge)
                    }
                }
            }
        }
    }
}

@Composable
private fun UsageGrid(groups: List<UsageGroup>, now: Long) {
    val kinds = groups.windowKinds()
    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Spacer(Modifier.width(84.dp))
            kinds.forEach { kind ->
                Text(when (kind) {
                    UsageMetricKind.SHORT_WINDOW -> "5-hour"
                    UsageMetricKind.LONG_WINDOW -> "Weekly"
                    else -> "Monthly"
                }, Modifier.weight(1f), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        groups.forEach { group ->
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(group.name, Modifier.width(84.dp), style = MaterialTheme.typography.labelLarge,
                    maxLines = 2, overflow = TextOverflow.Ellipsis)
                kinds.forEach { kind ->
                    val window = group.window(kind)
                    UsageGridCell(window, window?.let(group::isBlocked) == true, now, Modifier.weight(1f), group.name)
                }
            }
        }
    }
}

@Composable
private fun UsageGridCell(window: UsageWindow?, blocked: Boolean, now: Long, modifier: Modifier, group: String) {
    val remaining = window?.remainingPercent
    val color = when {
        remaining == null -> MaterialTheme.colorScheme.onSurfaceVariant
        blocked -> MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f)
        remaining <= 20 -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.primary
    }
    val decoration = if (blocked) TextDecoration.LineThrough else TextDecoration.None
    val caption = when {
        remaining == null -> "Not reported"
        window?.resetsAtEpochSeconds != null -> resetText(window.resetsAtEpochSeconds, now)
        remaining == 100.0 -> "Full"
        else -> "Reset time unknown"
    }
    Column(modifier.then(if (blocked) Modifier.semantics { contentDescription = "$group ${window?.label}, unavailable until weekly reset" } else Modifier),
        verticalArrangement = Arrangement.spacedBy(5.dp)) {
        Text(remaining?.roundToInt()?.let { "$it% left" } ?: "—", style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold, color = color, textDecoration = decoration)
        val bar = Modifier.fillMaxWidth().height(6.dp).drawWithContent {
            drawContent()
            if (blocked) drawLine(color, Offset(0f, size.height / 2), Offset(size.width, size.height / 2), strokeWidth = 1.dp.toPx())
        }
        if (remaining == null) Surface(bar, color = MaterialTheme.colorScheme.surfaceVariant, shape = MaterialTheme.shapes.small) {}
        else LinearProgressIndicator(progress = { (remaining / 100).toFloat().coerceIn(0f, 1f) }, modifier = bar, color = color)
        Text(caption, style = MaterialTheme.typography.bodySmall, color = color, textDecoration = decoration)
    }
}

@Composable
private fun UsageMetric(window: UsageWindow, now: Long) {
    val remaining = window.remainingPercent
    val color = if ((window.usedPercent ?: 0.0) >= 80.0) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(when (window.label) {
                "5H" -> "5-hour window"
                "7D", "WEEKLY" -> "Weekly"
                "SESSION" -> "Session"
                "MONTHLY" -> "Monthly"
                else -> window.label
            }, Modifier.weight(1f), style = MaterialTheme.typography.labelLarge)
            Text(remaining?.roundToInt()?.let { "$it% left" } ?: "—", style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold, color = color)
        }
        if (remaining == null) Surface(Modifier.fillMaxWidth().height(8.dp), color = MaterialTheme.colorScheme.surfaceVariant,
            shape = MaterialTheme.shapes.small) {}
        else LinearProgressIndicator(progress = { (remaining / 100).toFloat().coerceIn(0f, 1f) },
            modifier = Modifier.fillMaxWidth().height(8.dp), color = color)
        window.resetsAtEpochSeconds?.let { Text(resetText(it, now), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ResetDetailsSheet(account: ProviderAccount, snapshot: ProviderUsageSnapshot?, refreshing: Boolean, onEvent: (DashboardEvent) -> Unit) {
    ModalBottomSheet(onDismissRequest = { onEvent(DashboardEvent.CloseResets) }, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxWidth()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 24.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Reset credits", style = MaterialTheme.typography.headlineSmall)
                    Text(account.displayName, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                TextButton(onClick = { onEvent(DashboardEvent.CloseResets) }) { Text("Done") }
            }
            LazyColumn(Modifier.fillMaxWidth().heightIn(max = 480.dp), contentPadding = PaddingValues(24.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                item {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                        Column {
                            Text("${snapshot?.credits?.availableCount ?: "—"} available", style = MaterialTheme.typography.titleLarge)
                            snapshot?.credits?.totalEarnedCount?.let { Text("$it total earned", color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                        TextButton(onClick = { onEvent(DashboardEvent.RefreshResets) }, enabled = !refreshing) { Text("Refresh") }
                    }
                    if (refreshing) LinearProgressIndicator(Modifier.fillMaxWidth().padding(top = 12.dp))
                    snapshot?.resetsError?.let { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(top = 12.dp)) }
                    if (account.authenticationState == AuthenticationState.SIGN_IN_REQUIRED) TextButton(onClick = {
                        onEvent(DashboardEvent.CloseResets)
                        onEvent(DashboardEvent.Reauthenticate(account))
                    }) { Text("Sign in again") }
                }
                val credits = snapshot?.resetDetails
                if (credits == null && !refreshing) item { Text("Refresh to load credit details.", color = MaterialTheme.colorScheme.onSurfaceVariant) }
                else if (credits?.isEmpty() == true) item { Text("No reset credits for this account.") }
                else if (credits != null) items(credits.sortedBy { runCatching { Instant.parse(it.expiresAt) }.getOrNull() ?: Instant.MAX }) { credit ->
                    ResetCreditRow(credit)
                }
            }
        }
    }
}

@Composable
private fun ResetCreditRow(credit: ResetCredit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(credit.title.ifBlank { "Reset credit" }, Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            val available = credit.status.equals("available", true)
            Surface(shape = MaterialTheme.shapes.small,
                color = if (available) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surfaceVariant) {
                Text(credit.status.replace('_', ' ').ifBlank { "Unknown" }.replaceFirstChar { it.uppercase() },
                    Modifier.padding(horizontal = 8.dp, vertical = 4.dp), style = MaterialTheme.typography.labelSmall)
            }
        }
        if (credit.expiresAt.isNotBlank()) Text("Expires ${formatDate(credit.expiresAt)}", style = MaterialTheme.typography.bodyMedium)
        if (credit.grantedAt.isNotBlank()) Text("Granted ${formatDate(credit.grantedAt)}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (credit.redeemStartedAt.isNotBlank()) Text("Claim started ${formatDate(credit.redeemStartedAt)}", style = MaterialTheme.typography.bodySmall)
        if (credit.redeemedAt.isNotBlank()) Text("Redeemed ${formatDate(credit.redeemedAt)}", style = MaterialTheme.typography.bodySmall)
        HorizontalDivider(Modifier.padding(top = 8.dp))
    }
}

@Composable
private fun ServerConnectionDialog(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    if (!state.serverDialog) return
    var url by rememberSaveable(state.serverAddress, state.pendingServerAddress) {
        mutableStateOf(state.pendingServerAddress ?: state.serverAddress)
    }
    var code by remember(state.pendingPairCode) {
        mutableStateOf(state.pendingPairCode.orEmpty())
    }
    var inviteApplied by remember(state.inviteFromLink, state.pendingPairCode) {
        mutableStateOf(state.inviteFromLink)
    }
    fun applyIfInvite(input: String): Boolean {
        val invite = ing.boykiss.aiusagewidgets.data.server.parsePairingInvite(input) ?: return false
        url = invite.url
        code = invite.code
        inviteApplied = true
        return true
    }
    AlertDialog(
        onDismissRequest = { onEvent(DashboardEvent.CloseServerDialog) },
        title = { Text("Connect to usage server") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    if (inviteApplied) "Pairing invite loaded. Confirm the server address below, then tap Pair device."
                    else "Run check-usage server pair on your homelab, then scan its QR code or paste the pairing link below."
                )
                TextField(
                    value = url,
                    onValueChange = { if (!applyIfInvite(it)) { url = it; inviteApplied = false } },
                    label = { Text("Server URL or pairing link") },
                    placeholder = { Text("https://homelab.your-tailnet.ts.net") },
                    singleLine = true,
                    enabled = !state.pairingServer,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                )
                TextField(
                    value = code,
                    onValueChange = { if (!applyIfInvite(it)) code = it },
                    label = { Text("One-time pairing code") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    enabled = !state.pairingServer,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                )
                Text("Keep Tailscale connected. Provider logins stay on the server. Pairing to a different server replaces local accounts and widget assignments.", style = MaterialTheme.typography.bodySmall)
                state.serverError?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                if (state.pairingServer) CircularProgressIndicator(Modifier.size(24.dp))
            }
        },
        confirmButton = {
            TextButton(enabled = url.isNotBlank() && code.isNotBlank() && !state.pairingServer,
                onClick = { onEvent(DashboardEvent.PairServer(url.trim(), code.trim())) }) { Text("Pair device") }
        },
        dismissButton = { TextButton(enabled = !state.pairingServer, onClick = { onEvent(DashboardEvent.CloseServerDialog) }) { Text("Cancel") } },
    )
}

@Composable private fun AccountDialogs(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    state.accountBeingRenamed?.let { account ->
        var name by rememberSaveable(account.id.value) { mutableStateOf(account.displayName) }
        val valid = name.trim().isNotEmpty() && name.trim().length <= MAX_ACCOUNT_DISPLAY_NAME_LENGTH
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.CancelRenamingAccount) }, title = { Text("Rename account") },
            text = { TextField(value = name, onValueChange = { name = it }, label = { Text("Account name") }, singleLine = true,
                enabled = !state.renamingAccount, isError = !valid || state.renameError != null,
                supportingText = { Text(state.renameError ?: "${name.trim().length}/$MAX_ACCOUNT_DISPLAY_NAME_LENGTH characters") }) },
            confirmButton = { TextButton(enabled = valid && !state.renamingAccount, onClick = { onEvent(DashboardEvent.RenameAccount(name)) }) { Text("Save") } },
            dismissButton = { TextButton(enabled = !state.renamingAccount, onClick = { onEvent(DashboardEvent.CancelRenamingAccount) }) { Text("Cancel") } })
    }
    state.accountBeingRemoved?.let { account ->
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.CancelRemovingAccount) }, title = { Text("Remove ${account.displayName}?") },
            text = { Text("This removes the shared account from the server and all connected devices. Provider CLI sign-ins on the server are kept. Its widgets will ask you to choose an account.") },
            confirmButton = { TextButton(onClick = { onEvent(DashboardEvent.RemoveAccount) }) { Text("Remove") } },
            dismissButton = { TextButton(onClick = { onEvent(DashboardEvent.CancelRemovingAccount) }) { Text("Cancel") } })
    }
}

internal fun resetText(epoch: Long?, now: Long): String {
    if (epoch == null) return "Reset time unknown"
    val duration = Duration.between(Instant.ofEpochMilli(now), Instant.ofEpochSecond(epoch))
    if (duration.isNegative || duration.isZero) return "Refresh needed"
    return when {
        duration.toDays() > 0 -> "Resets in ${duration.toDays()}d ${duration.toHours() % 24}h"
        duration.toHours() > 0 -> "Resets in ${duration.toHours()}h"
        else -> "Resets in ${duration.toMinutes().coerceAtLeast(1)}m"
    }
}
private fun ago(epoch: Long, now: Long): String {
    val minutes = ((now - epoch) / 60_000).coerceAtLeast(0)
    return if (minutes < 1) "just now" else "${minutes}m ago"
}
private fun formatDate(value: String): String = runCatching {
    java.time.format.DateTimeFormatter.ofPattern("d MMM yyyy, HH:mm").withZone(java.time.ZoneId.systemDefault()).format(Instant.parse(value))
}.getOrDefault(value)
