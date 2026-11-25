package ing.boykiss.aiusagewidgets.ui.dashboard

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import ing.boykiss.aiusagewidgets.AppContainer
import ing.boykiss.aiusagewidgets.domain.*
import ing.boykiss.aiusagewidgets.providers.api.AuthenticationProgress
import ing.boykiss.aiusagewidgets.providers.api.AuthenticationSession
import ing.boykiss.aiusagewidgets.sync.UsageSyncWorker
import ing.boykiss.aiusagewidgets.widget.WidgetUpdater
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

data class DashboardState(
    val accounts: List<ProviderAccount> = emptyList(),
    val providers: List<ProviderDescriptor> = emptyList(),
    val resetAccount: ProviderAccount? = null,
    val snapshots: Map<String, ProviderUsageSnapshot> = emptyMap(),
    val loading: Boolean = true,
    val refreshing: Boolean = false,
    val refreshingResets: Boolean = false,
    val choosingProvider: Boolean = false,
    val connectingProvider: ProviderDescriptor? = null,
    val enteringAPIKey: Boolean = false,
    val authenticating: Boolean = false,
    val authSession: AuthenticationSession? = null,
    val authError: String? = null,
    val accountBeingRenamed: ProviderAccount? = null,
    val renamingAccount: Boolean = false,
    val renameError: String? = null,
    val accountBeingRemoved: ProviderAccount? = null,
    val notice: String? = null,
    val serverConfigured: Boolean = false,
    val serverAddress: String = "",
    val serverDialog: Boolean = false,
    val pendingServerAddress: String? = null,
    val pendingPairCode: String? = null,
    val inviteFromLink: Boolean = false,
    val pairingServer: Boolean = false,
    val serverError: String? = null,
    val canManageAccounts: Boolean = false,
) {
    val snapshot: ProviderUsageSnapshot? get() = resetAccount?.let { snapshots[it.id.value] }
}

sealed interface DashboardEvent {
    data object ConfigureServer : DashboardEvent
    data object CloseServerDialog : DashboardEvent
    data class ReceivePairingInvite(val url: String, val code: String) : DashboardEvent
    data class PairServer(val url: String, val code: String) : DashboardEvent
    data object AddAccount : DashboardEvent
    data object CancelAuthentication : DashboardEvent
    data class ConnectProvider(val id: ProviderId, val useAPIKey: Boolean = false) : DashboardEvent
    data class SubmitAPIKey(val key: String) : DashboardEvent
    data class SubmitCode(val code: String) : DashboardEvent
    data class Reauthenticate(val account: ProviderAccount) : DashboardEvent
    data class OpenResets(val account: ProviderAccount) : DashboardEvent
    data object CloseResets : DashboardEvent
    data object RefreshResets : DashboardEvent
    data class SetForeground(val active: Boolean) : DashboardEvent
    data class StartRenamingAccount(val account: ProviderAccount) : DashboardEvent
    data object CancelRenamingAccount : DashboardEvent
    data class RenameAccount(val displayName: String) : DashboardEvent
    data class StartRemovingAccount(val account: ProviderAccount) : DashboardEvent
    data object CancelRemovingAccount : DashboardEvent
    data object RemoveAccount : DashboardEvent
    data object Refresh : DashboardEvent
    data object DismissNotice : DashboardEvent
}

class DashboardViewModel(private val container: AppContainer, private val context: Context) : ViewModel() {
    private val mutable = MutableStateFlow(DashboardState(
        providers = container.providers.all().map { it.descriptor },
        serverConfigured = container.server.configured,
        serverAddress = container.server.address,
        serverDialog = !container.server.configured,
        canManageAccounts = container.server.connections.get()?.access == "write",
    ))
    val state = mutable.asStateFlow()
    private var refreshJob: Job? = null
    private var pairingJob: Job? = null
    private var resetsJob: Job? = null
    private var timerJob: Job? = null
    private var foreground = false
    private val scheduled = mutableMapOf<String, ProviderAccount>()

    init {
        viewModelScope.launch {
            container.repository.observeAccounts().collect { accounts ->
                val ids = accounts.map { it.id.value }.toSet()
                scheduled.values.filter { it.id.value !in ids }.forEach { UsageSyncWorker.cancel(context, it.providerId.value, it.id.value) }
                scheduled.keys.retainAll(ids)
                accounts.filter { it.id.value !in scheduled }.forEach {
                    scheduled[it.id.value] = it
                    if (container.server.configured) UsageSyncWorker.schedule(context, it.providerId.value, it.id.value)
                }
                val selected = mutable.value.resetAccount?.let { old -> accounts.firstOrNull { it.id == old.id } }
                mutable.update { it.copy(accounts = accounts, resetAccount = selected, loading = false) }
            }
        }
        viewModelScope.launch {
            container.repository.observeSnapshots().collect { snapshots -> mutable.update { it.copy(snapshots = snapshots) } }
        }
    }

    fun onEvent(event: DashboardEvent) {
        when (event) {
            DashboardEvent.ConfigureServer -> mutable.update {
                it.copy(serverDialog = true, serverError = null, pendingServerAddress = null, pendingPairCode = null, inviteFromLink = false)
            }
            DashboardEvent.CloseServerDialog -> if (!mutable.value.pairingServer) mutable.update {
                it.copy(serverDialog = false, pendingServerAddress = null, pendingPairCode = null, inviteFromLink = false)
            }
            is DashboardEvent.ReceivePairingInvite -> mutable.update {
                it.copy(serverDialog = true, pendingServerAddress = event.url, pendingPairCode = event.code, inviteFromLink = true, serverError = null)
            }
            is DashboardEvent.PairServer -> pair(event.url, event.code)
            DashboardEvent.AddAccount, is DashboardEvent.Reauthenticate, is DashboardEvent.ConnectProvider,
            is DashboardEvent.SubmitAPIKey, is DashboardEvent.SubmitCode -> mutable.update { it.copy(notice = "Manage provider logins on your homelab server, then refresh here to synchronize them.") }
            DashboardEvent.CancelAuthentication -> Unit
            is DashboardEvent.SetForeground -> {
                foreground = event.active
                timerJob?.cancel()
                if (foreground) {
                    refresh()
                    timerJob = viewModelScope.launch { while (isActive) { delay(60_000); refresh() } }
                }
            }
            DashboardEvent.Refresh -> refresh()
            is DashboardEvent.OpenResets -> {
                resetsJob?.cancel()
                mutable.update { it.copy(resetAccount = event.account, refreshingResets = false) }
                refreshResets()
            }
            DashboardEvent.CloseResets -> { resetsJob?.cancel(); mutable.update { it.copy(resetAccount = null, refreshingResets = false) } }
            DashboardEvent.RefreshResets -> refreshResets()
            is DashboardEvent.StartRenamingAccount -> if (mutable.value.canManageAccounts) mutable.update { it.copy(accountBeingRenamed = event.account, renameError = null) }
            DashboardEvent.CancelRenamingAccount -> if (!mutable.value.renamingAccount) mutable.update { it.copy(accountBeingRenamed = null) }
            is DashboardEvent.RenameAccount -> rename(event.displayName)
            is DashboardEvent.StartRemovingAccount -> if (mutable.value.canManageAccounts) mutable.update { it.copy(accountBeingRemoved = event.account) }
            DashboardEvent.CancelRemovingAccount -> mutable.update { it.copy(accountBeingRemoved = null) }
            DashboardEvent.RemoveAccount -> remove()
            DashboardEvent.DismissNotice -> mutable.update { it.copy(notice = null) }
        }
    }

    private fun pair(url: String, code: String) {
        if (pairingJob?.isActive == true) return
        pairingJob = viewModelScope.launch(Dispatchers.IO) {
            mutable.update { it.copy(pairingServer = true, serverError = null) }
            try {
                refreshJob?.cancelAndJoin()
                container.repository.connectToServer(url, code)
                UsageSyncWorker.scheduleCatalog(context)
                mutable.update { it.copy(serverConfigured = true, serverAddress = container.server.address,
                    serverDialog = false, pendingServerAddress = null, pendingPairCode = null, inviteFromLink = false,
                    canManageAccounts = container.server.connections.get()?.access == "write") }
                refresh()
                updateWidgets()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(serverError = error.message ?: "Could not pair with server") }
            } finally { mutable.update { it.copy(pairingServer = false) } }
        }
    }

    private fun refresh() {
        if (!container.server.configured || refreshJob?.isActive == true) return
        refreshJob = viewModelScope.launch(Dispatchers.IO) {
            mutable.update { it.copy(refreshing = true) }
            try {
                container.repository.syncServerAccounts()
                coroutineScope {
                    container.repository.accounts().map { account -> async { container.repository.refresh(account.id.value) } }.awaitAll()
                }
                updateWidgets()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(notice = error.message ?: "Could not reach usage server. Saved usage is still available.") }
            } finally { mutable.update { it.copy(refreshing = false) } }
        }
    }

    private fun rename(name: String) {
        val account = mutable.value.accountBeingRenamed ?: return
        mutable.update { it.copy(renamingAccount = true, renameError = null) }
        viewModelScope.launch(Dispatchers.IO) {
            try {
                container.repository.renameAccount(account.id.value, name)
                mutable.update { it.copy(accountBeingRenamed = null) }
                updateWidgets()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(renameError = error.message ?: "Could not rename shared account") }
            } finally { mutable.update { it.copy(renamingAccount = false) } }
        }
    }

    private fun remove() {
        val account = mutable.value.accountBeingRemoved ?: return
        mutable.update { it.copy(accountBeingRemoved = null) }
        viewModelScope.launch(Dispatchers.IO) {
            try { container.repository.removeAccount(account); updateWidgets() }
            catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(notice = error.message ?: "Could not remove shared account") }
            }
        }
    }

    private fun refreshResets() {
        val account = mutable.value.resetAccount ?: return
        if (resetsJob?.isActive == true) return
        resetsJob = viewModelScope.launch(Dispatchers.IO) {
            mutable.update { it.copy(refreshingResets = true) }
            try {
                container.repository.refreshResets(account.id.value).getOrThrow()
                updateWidgets()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(notice = error.message ?: "Could not read reset credits") }
            } finally { mutable.update { it.copy(refreshingResets = false) } }
        }
    }

    private suspend fun updateWidgets() = WidgetUpdater.updateAll(context)

    companion object {
        fun factory(container: AppContainer, context: Context): ViewModelProvider.Factory = object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>): T = DashboardViewModel(container, context) as T
        }
    }
}
