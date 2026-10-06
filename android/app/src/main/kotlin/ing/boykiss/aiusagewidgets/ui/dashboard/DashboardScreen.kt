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
                IconButton(onClick = { onEvent(DashboardEvent.Refresh) }, enabled = !state.refreshing && state.accounts.isNotEmpty()) {
                    Icon(painterResource(R.drawable.ic_widget_refresh), contentDescription = "Refresh usage")
                }
            })
        },
        floatingActionButton = {
            if (!state.loading && state.accounts.isNotEmpty()) ExtendedFloatingActionButton(
                onClick = { onEvent(DashboardEvent.AddAccount) },
                icon = { Icon(painterResource(R.drawable.ic_add), contentDescription = null) },
                text = { Text("Add account") },
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
                    Text("Connect your AI accounts to see remaining usage and keep it on your home screen.", color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Spacer(Modifier.height(24.dp))
                    Button(onClick = { onEvent(DashboardEvent.AddAccount) }) { Text("Connect an account") }
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
                            ?: account.providerId.value, state.snapshots[account.id.value], now, onEvent)
                    }
                }
            }
        }
    }
    state.resetAccount?.let { account -> ResetDetailsSheet(account, state.snapshot, state.refreshingResets, onEvent) }
    AccountDialogs(state, onEvent)
    AuthenticationDialogs(state, onEvent)
    state.notice?.let { notice ->
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.DismissNotice) }, title = { Text("AI Usage Widgets") },
            text = { Text(notice) }, confirmButton = { TextButton(onClick = { onEvent(DashboardEvent.DismissNotice) }) { Text("OK") } })
    }
}

@Composable
private fun AccountCard(account: ProviderAccount, providerName: String, snapshot: ProviderUsageSnapshot?, now: Long, onEvent: (DashboardEvent) -> Unit) {
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
                        DropdownMenuItem(text = { Text("Rename") }, onClick = { menu = false; onEvent(DashboardEvent.StartRenamingAccount(account)) })
                        DropdownMenuItem(text = { Text("Sign in again") }, onClick = { menu = false; onEvent(DashboardEvent.Reauthenticate(account)) })
                        DropdownMenuItem(text = { Text("Remove account") }, onClick = { menu = false; onEvent(DashboardEvent.StartRemovingAccount(account)) })
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
                snapshot.windows.forEach { window -> UsageMetric(window, now) }
                if (snapshot.windows.isEmpty() && !isBalance && snapshot.errorMessage == null) Text("No usage windows reported")
                snapshot.errorMessage?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                if (snapshot.fetchedAtEpochMillis > 0) Text(
                    "Updated ${ago(snapshot.fetchedAtEpochMillis, now)}${if (cached) " · saved usage" else ""}",
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (account.providerId.value == "codex") {
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
private fun AuthenticationDialogs(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    val context = LocalContext.current
    if (state.choosingProvider) {
        var choosingCursorMethod by rememberSaveable { mutableStateOf(false) }
        AlertDialog(onDismissRequest = { onEvent(DashboardEvent.CancelAuthentication) },
            title = { Text(if (choosingCursorMethod) "Connect Cursor" else "Connect an account") },
            text = {
                Column {
                    if (choosingCursorMethod) {
                        TextButton(onClick = { onEvent(DashboardEvent.ConnectProvider(ProviderId("cursor"))) }) { Text("Sign in with browser") }
                        TextButton(onClick = { onEvent(DashboardEvent.ConnectProvider(ProviderId("cursor"), true)) }) { Text("Use an API key") }
                        TextButton(onClick = { choosingCursorMethod = false }) { Text("Back to providers") }
                    } else state.providers.forEach { provider ->
                        TextButton(onClick = {
                            if (provider.id.value == "cursor") choosingCursorMethod = true
                            else onEvent(DashboardEvent.ConnectProvider(provider.id))
                        }, modifier = Modifier.fillMaxWidth()) { Text(provider.displayName) }
                    }
                }
            }, confirmButton = {}, dismissButton = {
                TextButton(onClick = { onEvent(DashboardEvent.CancelAuthentication) }) { Text("Cancel") }
            })
    }
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
                        if (session.requiresCode) Text("Sign in in your browser, then paste the authorization code here.")
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
