package ing.boykiss.aiusagewidgets.ui.dashboard

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.*
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleStartEffect
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
            TopAppBar(title = { Text("AI Usage Widgets") }, actions = {
                TextButton(onClick = { onEvent(DashboardEvent.AddAccount) }) { Text("+ Account") }
                TextButton(onClick = { onEvent(DashboardEvent.Refresh) }, enabled = !state.refreshing && !state.refreshingResets) { Text("Refresh") }
            })
        },
        bottomBar = {
            NavigationBar {
                DashboardTab.entries.forEach { tab ->
                    val label = tab.name.lowercase().replaceFirstChar { it.uppercase() }
                    NavigationBarItem(selected = state.tab == tab,
                        onClick = { onEvent(DashboardEvent.SelectTab(tab)) },
                        icon = { Text(when (tab) { DashboardTab.USAGE -> "▥"; DashboardTab.RESETS -> "↻"; DashboardTab.SETTINGS -> "⚙" }) },
                        label = { Text(label) })
                }
            }
        },
    ) { padding ->
        PullToRefreshBox(
            isRefreshing = if (state.tab == DashboardTab.RESETS) state.refreshingResets else state.refreshing,
            onRefresh = { onEvent(DashboardEvent.Refresh) }, modifier = Modifier.fillMaxSize().padding(padding),
        ) {
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(20.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                if (state.loading) item { CircularProgressIndicator() }
                else when (state.tab) {
                    DashboardTab.USAGE -> {
                        if (state.accounts.isEmpty()) item {
                            Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                                Text("Your limits, at a glance", style = MaterialTheme.typography.headlineLarge)
                                Text("Connect Codex, Claude, Cursor, OpenCode or DeepSeek to track all your accounts.")
                                Button(onClick = { onEvent(DashboardEvent.AddAccount) }) { Text("Connect an account") }
                            }
                        }
                        items(state.accounts, key = { it.id.value }) { account ->
                            AccountCard(account, state.snapshots[account.id.value], state.settings, now, onEvent)
                        }
                    }
                    DashboardTab.RESETS -> {
                        val accounts = state.accounts.filter { it.providerId.value == "codex" }
                        if (accounts.isEmpty()) item { Text("Reset credits are available for Codex accounts. Connect Codex to see them here.") }
                        else {
                            item {
                                Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    accounts.forEach { account ->
                                        FilterChip(state.selectedAccount?.id == account.id,
                                            onClick = { onEvent(DashboardEvent.SelectAccount(account)) }, label = { Text(account.displayName) })
                                    }
                                }
                            }
                            val snapshot = state.snapshot
                            item {
                                Text("Reset credits", style = MaterialTheme.typography.headlineMedium)
                                Text("${snapshot?.credits?.availableCount ?: "—"} available · ${snapshot?.credits?.totalEarnedCount ?: "—"} earned")
                                Text("Claim confirmation is a preview; claiming is not connected yet.", style = MaterialTheme.typography.bodySmall)
                                snapshot?.resetsError?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                            }
                            val credits = snapshot?.resetDetails
                            if (credits == null) item { Text(if (state.refreshingResets) "Loading reset details…" else "Refresh to load reset details.") }
                            else if (credits.isEmpty()) item { Text("No reset credits for this account.") }
                            else items(credits.sortedBy { runCatching { Instant.parse(it.expiresAt) }.getOrNull() ?: Instant.MAX }) { credit ->
                                ResetCard(credit, onEvent)
                            }
                        }
                    }
                    DashboardTab.SETTINGS -> item { SettingsContent(state.settings) { onEvent(DashboardEvent.SaveSettings(it)) } }
                }
            }
        }
    }
    AccountDialogs(state, onEvent)
    AuthenticationDialogs(state, onEvent)
    state.notice?.let { notice ->
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.DismissNotice) }, title = { Text("AI Usage Widgets") },
            text = { Text(notice) }, confirmButton = { TextButton(onClick = { onEvent(DashboardEvent.DismissNotice) }) { Text("OK") } })
    }
}

@Composable
private fun AccountCard(account: ProviderAccount, snapshot: ProviderUsageSnapshot?, settings: DashboardSettings, now: Long, onEvent: (DashboardEvent) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(if (settings.compactMode) 12.dp else 20.dp), verticalArrangement = Arrangement.spacedBy(if (settings.compactMode) 6.dp else 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(account.displayName, style = MaterialTheme.typography.titleLarge)
                    Text(listOfNotNull(account.providerId.value, snapshot?.planLabel ?: account.planLabel).joinToString(" · "), style = MaterialTheme.typography.bodySmall)
                    if (!settings.compactMode) account.identityLabel?.let { Text(it, style = MaterialTheme.typography.bodySmall) }
                }
                Box {
                    TextButton(onClick = { menu = true }) { Text("Manage") }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text("Rename") }, onClick = { menu = false; onEvent(DashboardEvent.StartRenamingAccount(account)) })
                        DropdownMenuItem(text = { Text("Sign in again") }, onClick = { menu = false; onEvent(DashboardEvent.Reauthenticate(account)) })
                        DropdownMenuItem(text = { Text("Remove") }, onClick = { menu = false; onEvent(DashboardEvent.StartRemovingAccount(account)) })
                    }
                }
            }
            if (account.authenticationState == AuthenticationState.SIGN_IN_REQUIRED) {
                Text("Sign-in required", color = MaterialTheme.colorScheme.error)
                TextButton(onClick = { onEvent(DashboardEvent.Reauthenticate(account)) }) { Text("Sign in again") }
            }
            if (snapshot == null) {
                Text("Loading usage…", color = MaterialTheme.colorScheme.onSurfaceVariant)
                LinearProgressIndicator(Modifier.fillMaxWidth())
            } else {
                snapshot.windows.forEach { window -> UsageMetric(window, settings, now, snapshot.freshness != DataFreshness.FRESH) }
                if (snapshot.windows.isEmpty() && account.providerId.value != "deepseek" && snapshot.errorMessage == null) Text("No usage windows reported")
                snapshot.credits?.let { Text("${it.availableCount ?: "—"} reset credits available", style = MaterialTheme.typography.labelLarge) }
                snapshot.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                snapshot.resetsError?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
                if (snapshot.fetchedAtEpochMillis > 0) Text(
                    "Updated ${ago(snapshot.fetchedAtEpochMillis, now)}${if (snapshot.freshness != DataFreshness.FRESH) " · cached" else ""}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun UsageMetric(window: UsageWindow, settings: DashboardSettings, now: Long, cached: Boolean) {
    val value = if (settings.usageDisplay == "used") window.usedPercent else window.remainingPercent
    val severity = usageSeverity(window.usedPercent, cached)
    val color = when (settings.colorTheme) {
        "monochrome" -> MaterialTheme.colorScheme.onSurface
        "colorblind" -> when (severity) {
            UsageSeverity.GOOD -> Color(0xFF2879C7)
            UsageSeverity.WARNING -> Color(0xFFD97706)
            UsageSeverity.BAD -> Color(0xFFC04FA2)
        }
        else -> when (severity) {
            UsageSeverity.GOOD -> MaterialTheme.colorScheme.primary
            UsageSeverity.WARNING -> Color(0xFFB8860B)
            UsageSeverity.BAD -> MaterialTheme.colorScheme.error
        }
    }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(window.label, style = MaterialTheme.typography.labelLarge)
        settings.barOrder.split('_').forEach { part ->
            when (part) {
                "percent" -> if (settings.showPercent) Text("${value?.roundToInt()?.let { "$it%" } ?: "—"} ${settings.usageDisplay}",
                    style = if (settings.compactMode) MaterialTheme.typography.bodyLarge else MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold, color = color)
                "bar" -> if (settings.showBar) {
                    Box(Modifier.fillMaxWidth().height(8.dp), contentAlignment = if (settings.barFill == "right") Alignment.CenterEnd else Alignment.CenterStart) {
                        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surfaceVariant, shape = MaterialTheme.shapes.small) {}
                        value?.takeIf { it > 0 }?.let {
                            Surface(Modifier.fillMaxWidth((it / 100).toFloat().coerceIn(0f, 1f)).fillMaxHeight(), color = color, shape = MaterialTheme.shapes.small) {}
                        }
                    }
                }
                "reset" -> if (settings.showReset) Text(resetText(window.resetsAtEpochSeconds, now), style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
private fun ResetCard(credit: ResetCredit, onEvent: (DashboardEvent) -> Unit) {
    var confirming by remember(credit) { mutableStateOf(false) }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(credit.title.ifBlank { "Reset credit" }, style = MaterialTheme.typography.titleMedium)
            Text(credit.status.replace('_', ' '))
            if (credit.grantedAt.isNotBlank()) Text("Granted: ${formatDate(credit.grantedAt)}", style = MaterialTheme.typography.bodySmall)
            if (credit.expiresAt.isNotBlank()) Text("Expires: ${formatDate(credit.expiresAt)}", style = MaterialTheme.typography.bodySmall)
            if (credit.redeemStartedAt.isNotBlank()) Text("Claim started: ${formatDate(credit.redeemStartedAt)}", style = MaterialTheme.typography.bodySmall)
            if (credit.redeemedAt.isNotBlank()) Text("Redeemed: ${formatDate(credit.redeemedAt)}", style = MaterialTheme.typography.bodySmall)
            if (credit.status.equals("available", true)) TextButton(onClick = { confirming = true }) { Text("Claim…") }
        }
    }
    if (confirming) AlertDialog(onDismissRequest = { confirming = false }, title = { Text("Confirm claim") },
        text = { Text("Confirm this reset credit? Claiming is not connected yet and will not change the credit.") },
        confirmButton = { TextButton(onClick = { confirming = false; onEvent(DashboardEvent.ConfirmResetClaim) }) { Text("Confirm") } },
        dismissButton = { TextButton(onClick = { confirming = false }) { Text("Cancel") } })
}

@Composable
private fun SettingsContent(settings: DashboardSettings, save: (DashboardSettings) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text("Settings", style = MaterialTheme.typography.headlineMedium)
        SettingChoice("Usage display", listOf("used", "remaining"), settings.usageDisplay) { save(settings.copy(usageDisplay = it)) }
        SettingChoice("Bar fill", listOf("left", "right"), settings.barFill) { save(settings.copy(barFill = it)) }
        SettingChoice("Metric order", DashboardSettings.barOrders, settings.barOrder) { save(settings.copy(barOrder = it)) }
        SettingToggle("Show percentage", settings.showPercent) { save(settings.copy(showPercent = it)) }
        SettingToggle("Show bar", settings.showBar) { save(settings.copy(showBar = it)) }
        SettingToggle("Show reset countdown", settings.showReset) { save(settings.copy(showReset = it)) }
        SettingChoice("Semantic colors", listOf("default", "colorblind", "monochrome"), settings.colorTheme) { save(settings.copy(colorTheme = it)) }
        Text("Automatic refresh", style = MaterialTheme.typography.titleMedium)
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            DashboardSettings.refreshIntervals.forEach { seconds ->
                FilterChip(selected = settings.autoRefreshSeconds == seconds, onClick = { save(settings.copy(autoRefreshSeconds = seconds)) },
                    label = { Text(when { seconds == 0 -> "Off"; seconds < 60 -> "${seconds}s"; else -> "${seconds / 60}m" }) })
            }
        }
        Text("Applies while the app is open. Widgets refresh approximately every 15 minutes, as scheduled by Android.", style = MaterialTheme.typography.bodySmall)
        SettingToggle("Compact account rows", settings.compactMode) { save(settings.copy(compactMode = it)) }
    }
}

@Composable private fun SettingChoice(label: String, values: List<String>, selected: String, change: (String) -> Unit) {
    Column {
        Text(label, style = MaterialTheme.typography.titleMedium)
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            values.forEach { value -> FilterChip(selected == value, onClick = { change(value) }, label = { Text(value.replace('_', ' ')) }) }
        }
    }
}
@Composable private fun SettingToggle(label: String, enabled: Boolean, change: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f)); Switch(checked = enabled, onCheckedChange = change)
    }
}

@Composable
private fun AuthenticationDialogs(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    val context = LocalContext.current
    if (state.choosingProvider) AlertDialog(onDismissRequest = { onEvent(DashboardEvent.CancelAuthentication) },
        title = { Text("Connect an account") }, text = {
            Column { state.providers.forEach { provider ->
                TextButton(onClick = { onEvent(DashboardEvent.ConnectProvider(provider.id)) }) { Text(provider.displayName) }
                if (provider.id.value == "cursor") TextButton(onClick = { onEvent(DashboardEvent.ConnectProvider(provider.id, true)) }) { Text("Cursor with API key") }
            } }
        }, confirmButton = {}, dismissButton = { TextButton(onClick = { onEvent(DashboardEvent.CancelAuthentication) }) { Text("Cancel") } })
    state.connectingProvider?.let { provider ->
        var code by rememberSaveable(provider.id.value, state.authSession?.sessionId) { mutableStateOf("") }
        val session = state.authSession
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.CancelAuthentication) },
            title = { Text("Connect ${provider.displayName}") }, text = {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    if (state.enteringAPIKey) {
                        TextField(value = code, onValueChange = { code = it }, label = { Text("API key") }, singleLine = true,
                            visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), enabled = !state.authenticating)
                    } else if (session != null) {
                        if (session.userCode.isNotEmpty()) {
                            Text("Enter this code on the verification page:")
                            SelectionContainer { Text(session.userCode, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold) }
                        }
                        SelectionContainer { Text(session.verificationUrl, style = MaterialTheme.typography.bodySmall) }
                        if (session.requiresCode) TextField(value = code, onValueChange = { code = it }, label = { Text("Authorization code") }, singleLine = true, enabled = !state.authenticating)
                        else Text("Waiting for browser approval…")
                    }
                    if (state.authenticating) CircularProgressIndicator(Modifier.size(24.dp))
                    state.authError?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                }
            }, confirmButton = {
                when {
                    state.enteringAPIKey -> TextButton(enabled = code.isNotBlank() && !state.authenticating, onClick = { onEvent(DashboardEvent.SubmitAPIKey(code)) }) { Text("Connect") }
                    session?.requiresCode == true -> Row {
                        TextButton(onClick = { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(session.verificationUrl))) }) { Text("Open browser") }
                        TextButton(enabled = code.isNotBlank() && !state.authenticating, onClick = { onEvent(DashboardEvent.SubmitCode(code)) }) { Text("Connect") }
                    }
                    session != null -> TextButton(onClick = { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(session.verificationUrl))) }) { Text("Open browser") }
                    state.authError != null -> TextButton(onClick = { onEvent(DashboardEvent.ConnectProvider(provider.id)) }) { Text("Retry") }
                }
            }, dismissButton = { TextButton(onClick = { onEvent(DashboardEvent.CancelAuthentication) }) { Text("Cancel") } })
    }
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
            text = { Text("This removes its saved login and cached usage. Its widgets will ask you to choose an account.") },
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
