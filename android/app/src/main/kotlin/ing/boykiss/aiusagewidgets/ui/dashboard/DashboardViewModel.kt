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
) {
    val snapshot: ProviderUsageSnapshot? get() = resetAccount?.let { snapshots[it.id.value] }
}

sealed interface DashboardEvent {
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
    private val mutable = MutableStateFlow(DashboardState(providers = container.providers.all().map { it.descriptor }))
    val state = mutable.asStateFlow()
    private var authJob: Job? = null
    private var refreshJob: Job? = null
    private var resetsJob: Job? = null
    private var timerJob: Job? = null
    private var foreground = false
    private val scheduledAccounts = java.util.concurrent.ConcurrentHashMap.newKeySet<String>()
    private var reauthTarget: ProviderAccount? = null

    init {
        viewModelScope.launch {
            container.repository.observeAccounts().collect { accounts ->
                val selected = mutable.value.resetAccount?.let { old -> accounts.firstOrNull { it.id == old.id } }
                mutable.update { it.copy(accounts = accounts, resetAccount = selected, loading = false) }
                accounts.filter { scheduledAccounts.add(it.id.value) }.forEach {
                    UsageSyncWorker.schedule(context, it.providerId.value, it.id.value)
                }
                if (foreground && accounts.any { it.id.value !in mutable.value.snapshots }) refresh()
            }
        }
        viewModelScope.launch {
            container.repository.observeSnapshots().collect { snapshots -> mutable.update { it.copy(snapshots = snapshots) } }
        }

    }

    fun onEvent(event: DashboardEvent) {
        when (event) {
            DashboardEvent.AddAccount -> {
                reauthTarget = null
                mutable.update { it.copy(choosingProvider = true, authError = null) }
            }
            DashboardEvent.CancelAuthentication -> cancelAuthentication()
            is DashboardEvent.ConnectProvider -> connect(event.id, event.useAPIKey)
            is DashboardEvent.SubmitAPIKey -> completeAuthentication {
                container.providers.require(mutable.value.connectingProvider!!.id).authenticator.addAPIKey(event.key)
            }
            is DashboardEvent.SubmitCode -> {
                val session = mutable.value.authSession ?: return
                completeAuthentication { container.providers.require(session.providerId).authenticator.completeCode(session, event.code) }
            }
            is DashboardEvent.Reauthenticate -> {
                cancelAuthentication()
                reauthTarget = event.account
                connect(event.account.providerId)
            }
            is DashboardEvent.OpenResets -> {
                if (event.account.providerId.value != "codex") return
                resetsJob?.cancel()
                mutable.update { it.copy(resetAccount = event.account, refreshingResets = false) }
                refreshResets()
            }
            DashboardEvent.CloseResets -> {
                resetsJob?.cancel()
                mutable.update { it.copy(resetAccount = null, refreshingResets = false) }
            }
            DashboardEvent.RefreshResets -> refreshResets()
            is DashboardEvent.SetForeground -> {
                foreground = event.active
                scheduleTimer()
                if (foreground) refresh()
            }
            is DashboardEvent.StartRenamingAccount -> mutable.update { it.copy(accountBeingRenamed = event.account, renameError = null) }
            DashboardEvent.CancelRenamingAccount -> if (!mutable.value.renamingAccount) mutable.update { it.copy(accountBeingRenamed = null, renameError = null) }
            is DashboardEvent.RenameAccount -> rename(event.displayName)
            is DashboardEvent.StartRemovingAccount -> mutable.update { it.copy(accountBeingRemoved = event.account) }
            DashboardEvent.CancelRemovingAccount -> mutable.update { it.copy(accountBeingRemoved = null) }
            DashboardEvent.RemoveAccount -> remove()
            DashboardEvent.Refresh -> refresh()
            DashboardEvent.DismissNotice -> mutable.update { it.copy(notice = null) }
        }
    }

    private fun scheduleTimer() {
        timerJob?.cancel()
        if (!foreground) return
        timerJob = viewModelScope.launch {
            while (isActive) {
                delay(60_000L)
                refresh()
            }
        }
    }

    private fun connect(id: ProviderId, apiKey: Boolean = false) {
        if (authJob?.isActive == true) return
        val provider = container.providers.require(id)
        val usesKey = apiKey || id.value in listOf("opencode-go", "deepseek")
        mutable.update { it.copy(choosingProvider = false, connectingProvider = provider.descriptor,
            enteringAPIKey = usesKey, authenticating = !usesKey, authError = null) }
        if (usesKey) return
        authJob = viewModelScope.launch(Dispatchers.IO) {
            try {
                val session = provider.authenticator.beginAuthentication()
                mutable.update { it.copy(authSession = session, authenticating = false) }
                if (session.requiresCode) return@launch
                repeat(600) {
                    delay(session.pollIntervalSeconds * 1000L)
                    when (val progress = provider.authenticator.pollAuthentication(session)) {
                        AuthenticationProgress.Pending -> Unit
                        is AuthenticationProgress.Complete -> { saveLogin(progress.account); return@launch }
                    }
                }
                error("Sign-in timed out; start again")
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(authSession = null, authenticating = false, authError = error.message ?: "Sign-in failed") }
            }
        }
    }

    private fun completeAuthentication(login: suspend () -> ProviderAccount) {
        if (authJob?.isActive == true) return
        mutable.update { it.copy(authenticating = true, authError = null) }
        authJob = viewModelScope.launch(Dispatchers.IO) {
            try { saveLogin(login()) } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(authenticating = false, authError = error.message ?: "Sign-in failed") }
            }
        }
    }

    private suspend fun saveLogin(account: ProviderAccount) {
        val saved = container.repository.saveAuthenticatedAccount(account, reauthTarget)
        reauthTarget = null
        mutable.update { it.copy(authSession = null, connectingProvider = null,
            enteringAPIKey = false, authenticating = false, authError = null) }
        UsageSyncWorker.schedule(context, saved.providerId.value, saved.id.value)
        container.repository.refresh(saved.id.value)
        updateWidgets()
    }

    private fun cancelAuthentication() {
        authJob?.cancel()
        reauthTarget = null
        mutable.update { it.copy(choosingProvider = false, connectingProvider = null, authSession = null,
            enteringAPIKey = false, authenticating = false, authError = null) }
    }

    private fun rename(name: String) {
        val account = mutable.value.accountBeingRenamed ?: return
        if (mutable.value.renamingAccount) return
        mutable.update { it.copy(renamingAccount = true) }
        viewModelScope.launch(Dispatchers.IO) {
            try {
                container.repository.renameAccount(account.id.value, name)
                mutable.update { it.copy(accountBeingRenamed = null, renamingAccount = false) }
                updateWidgets()
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                mutable.update { it.copy(renamingAccount = false, renameError = error.message) }
            }
        }
    }

    private fun remove() {
        val account = mutable.value.accountBeingRemoved ?: return
        mutable.update { it.copy(accountBeingRemoved = null) }
        viewModelScope.launch(Dispatchers.IO) {
            try {
                container.repository.removeAccount(account)
                scheduledAccounts.remove(account.id.value)
                UsageSyncWorker.cancel(context, account.providerId.value, account.id.value)
                updateWidgets()
            } catch (error: Exception) { if (error is CancellationException) throw error; showError(error) }
        }
    }

    private fun refresh() {
        if (refreshJob?.isActive == true) return
        val accounts = mutable.value.accounts
        if (accounts.isEmpty()) return
        refreshJob = viewModelScope.launch(Dispatchers.IO) {
            mutable.update { it.copy(refreshing = true) }
            try {
                coroutineScope { accounts.map { account -> async { container.repository.refresh(account.id.value) } }.awaitAll() }
                updateWidgets()
            } finally { mutable.update { it.copy(refreshing = false) } }
        }
    }

    private fun refreshResets() {
        val account = mutable.value.resetAccount ?: return
        if (account.providerId.value != "codex") return
        if (resetsJob?.isActive == true) return
        resetsJob = viewModelScope.launch(Dispatchers.IO) {
            mutable.update { it.copy(refreshingResets = true) }
            try {
                container.repository.refreshResets(account.id.value)
                updateWidgets()
            } finally {
                mutable.update { if (it.resetAccount?.id == account.id) it.copy(refreshingResets = false) else it }
            }
        }
    }

    private suspend fun updateWidgets() {
        try { WidgetUpdater.updateAll(context) } catch (error: Exception) {
            if (error is CancellationException) throw error
            showError(error)
        }
    }

    private fun showError(error: Exception) { mutable.update { it.copy(notice = error.message ?: "Operation failed") } }

    class Factory(private val container: AppContainer, private val context: Context) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = DashboardViewModel(container, context) as T
    }
}
