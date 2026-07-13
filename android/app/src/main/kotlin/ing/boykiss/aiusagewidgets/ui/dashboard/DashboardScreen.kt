package ing.boykiss.aiusagewidgets.ui.dashboard

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import ing.boykiss.aiusagewidgets.domain.UsageWindow
import ing.boykiss.aiusagewidgets.domain.MAX_ACCOUNT_DISPLAY_NAME_LENGTH
import ing.boykiss.aiusagewidgets.domain.ProviderAccount
import java.time.Duration
import java.time.Instant
import kotlin.math.roundToInt

@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
fun DashboardScreen(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    val context = LocalContext.current
    Scaffold(topBar = { TopAppBar(title = { Text("AI Usage Widgets") }) }) { padding ->
        PullToRefreshBox(
            isRefreshing = state.refreshing,
            onRefresh = { onEvent(DashboardEvent.Refresh) },
            modifier = Modifier.fillMaxSize().padding(padding),
        ) {
            Column(
                Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 20.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                if (state.loading) {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                } else if (state.accounts.isEmpty()) {
                    Spacer(Modifier.height(36.dp))
                    Text("Your limits, at a glance", style = MaterialTheme.typography.headlineLarge)
                    Text(
                        "Connect a provider to keep usage windows and reset credits on your home screen.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Button(onClick = { onEvent(DashboardEvent.AddAccount) }) { Text("Connect Codex") }
                } else {
                    AccountSelector(state, onEvent)
                    val snapshot = state.snapshot
                    if (snapshot == null) CircularProgressIndicator()
                    else {
                        snapshot.windows.firstOrNull { it.kind == UsageMetricKind.SHORT_WINDOW }?.let { UsageCard(it) }
                        snapshot.windows.firstOrNull { it.kind == UsageMetricKind.LONG_WINDOW }?.let { UsageCard(it) }
                        CreditsCard(snapshot.credits?.availableCount, snapshot.credits?.earliestExpiryEpochSeconds)
                        Text(
                            "Updated ${ago(snapshot.fetchedAtEpochMillis)}",
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
            }
        }
    }

    state.authSession?.let { session ->
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { onEvent(DashboardEvent.CancelAuthentication) },
            title = { Text("Connect Codex") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text("Enter this code in the verification page:")
                    Text(session.userCode, fontSize = 30.sp, fontWeight = FontWeight.Bold)
                    Text("Device code login URL", style = MaterialTheme.typography.labelMedium)
                    SelectionContainer {
                        Text(
                            text = session.verificationUrl,
                            color = MaterialTheme.colorScheme.primary,
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                    CircularProgressIndicator(Modifier.size(24.dp))
                }
            },
            confirmButton = {
                Button(onClick = { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(session.verificationUrl))) }) {
                    Text("Open verification")
                }
            },
            dismissButton = { TextButton(onClick = { onEvent(DashboardEvent.CancelAuthentication) }) { Text("Cancel") } },
        )
    }

    state.accountBeingRenamed?.let { account ->
        var displayName by rememberSaveable(account.id.value) { mutableStateOf(account.displayName) }
        val normalizedName = displayName.trim()
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { onEvent(DashboardEvent.CancelRenamingAccount) },
            title = { Text("Rename account") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextField(
                        value = displayName,
                        onValueChange = { displayName = it },
                        label = { Text("Account name") },
                        singleLine = true,
                        enabled = !state.renamingAccount,
                        isError = state.renameError != null || normalizedName.length > MAX_ACCOUNT_DISPLAY_NAME_LENGTH,
                        supportingText = {
                            Text(
                                state.renameError
                                    ?: "${normalizedName.length}/$MAX_ACCOUNT_DISPLAY_NAME_LENGTH characters"
                            )
                        },
                    )
                }
            },
            confirmButton = {
                Button(
                    enabled = !state.renamingAccount && normalizedName.isNotEmpty() &&
                        normalizedName.length <= MAX_ACCOUNT_DISPLAY_NAME_LENGTH,
                    onClick = { onEvent(DashboardEvent.RenameAccount(displayName)) },
                ) {
                    Text(if (state.renamingAccount) "Saving…" else "Save")
                }
            },
            dismissButton = {
                TextButton(
                    enabled = !state.renamingAccount,
                    onClick = { onEvent(DashboardEvent.CancelRenamingAccount) },
                ) { Text("Cancel") }
            },
        )
    }
}

@Composable private fun AccountSelector(state: DashboardState, onEvent: (DashboardEvent) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        state.accounts.forEach { account ->
            AccountChip(
                account = account,
                selected = account.id == state.selectedAccount?.id,
                onClick = { onEvent(DashboardEvent.SelectAccount(account)) },
                onLongClick = { onEvent(DashboardEvent.StartRenamingAccount(account)) },
            )
        }
        FilterChip(selected = false, onClick = { onEvent(DashboardEvent.AddAccount) }, label = { Text("+ Account") })
    }
}

@Composable
private fun AccountChip(
    account: ProviderAccount,
    selected: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
) {
    Surface(
        modifier = Modifier
            .height(48.dp)
            .semantics { this.selected = selected }
            .combinedClickable(
                role = Role.RadioButton,
                onClick = onClick,
                onLongClickLabel = "Rename ${account.displayName}",
                onLongClick = onLongClick,
            ),
        shape = RoundedCornerShape(8.dp),
        color = if (selected) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface,
        contentColor = if (selected) MaterialTheme.colorScheme.onSecondaryContainer else MaterialTheme.colorScheme.onSurfaceVariant,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outline),
    ) {
        Box(Modifier.padding(horizontal = 16.dp), contentAlignment = Alignment.Center) {
            Text(account.displayName, style = MaterialTheme.typography.labelLarge)
        }
    }
}

@Composable private fun UsageCard(window: UsageWindow) {
    val remaining = window.remainingPercent ?: 0.0
    Card(Modifier.fillMaxWidth(), shape = RoundedCornerShape(28.dp)) {
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(window.label, style = MaterialTheme.typography.labelLarge)
                Spacer(Modifier.weight(1f))
                Text(resetText(window.resetsAtEpochSeconds), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text("${remaining.roundToInt()}% left", style = MaterialTheme.typography.headlineLarge, fontWeight = FontWeight.Bold)
            LinearProgressIndicator({ (remaining / 100).toFloat() }, Modifier.fillMaxWidth().height(8.dp))
        }
    }
}

@Composable private fun CreditsCard(count: Int?, expiry: Long?) {
    Card(Modifier.fillMaxWidth(), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.secondaryContainer)) {
        Row(Modifier.padding(20.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text("Reset credits", style = MaterialTheme.typography.labelLarge)
                Text(expiry?.let { "Earliest expires ${resetText(it)}" } ?: "No expiry reported", style = MaterialTheme.typography.bodySmall)
            }
            if (count != null && count > 0) {
                Text(count.toString(), fontSize = 36.sp, fontWeight = FontWeight.Bold)
            }
        }
    }
}

private fun resetText(epochSeconds: Long?): String {
    if (epochSeconds == null) return "Reset unknown"
    val duration = Duration.between(Instant.now(), Instant.ofEpochSecond(epochSeconds))
    if (duration.isNegative || duration.isZero) return "Refresh needed"
    val days = duration.toDays()
    val hours = duration.minusDays(days).toHours()
    return if (days > 0) "Resets in ${days}d ${hours}h" else "Resets in ${duration.toHours()}h"
}

private fun ago(epochMillis: Long): String {
    val minutes = Duration.between(Instant.ofEpochMilli(epochMillis), Instant.now()).toMinutes().coerceAtLeast(0)
    return if (minutes < 1) "just now" else "${minutes}m ago"
}
