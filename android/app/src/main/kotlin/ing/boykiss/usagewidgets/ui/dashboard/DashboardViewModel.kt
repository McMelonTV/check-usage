package ing.boykiss.usagewidgets.ui.dashboard

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import ing.boykiss.usagewidgets.AppContainer
import ing.boykiss.usagewidgets.domain.ProviderAccount
import ing.boykiss.usagewidgets.domain.ProviderUsageSnapshot
import ing.boykiss.usagewidgets.providers.api.AuthenticationProgress
import ing.boykiss.usagewidgets.providers.api.AuthenticationSession
import ing.boykiss.usagewidgets.sync.UsageSyncWorker
import ing.boykiss.usagewidgets.widget.WidgetRenderWorker
import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class DashboardState(
    val accounts: List<ProviderAccount> = emptyList(),
    val selectedAccount: ProviderAccount? = null,
    val snapshot: ProviderUsageSnapshot? = null,
    val loading: Boolean = true,
    val refreshing: Boolean = false,
    val authSession: AuthenticationSession? = null,
    val authError: String? = null,
    val accountBeingRenamed: ProviderAccount? = null,
    val renamingAccount: Boolean = false,
    val renameError: String? = null,
)

sealed interface DashboardEvent {
    data object AddAccount : DashboardEvent
    data object CancelAuthentication : DashboardEvent
    data class SelectAccount(val account: ProviderAccount) : DashboardEvent
    data class StartRenamingAccount(val account: ProviderAccount) : DashboardEvent
    data object CancelRenamingAccount : DashboardEvent
    data class RenameAccount(val displayName: String) : DashboardEvent
    data object Refresh : DashboardEvent
}

class DashboardViewModel(
    private val container: AppContainer,
    private val context: Context,
) : ViewModel() {
    private val _state = MutableStateFlow(DashboardState())
    val state: StateFlow<DashboardState> = _state.asStateFlow()
    private var snapshotJob: Job? = null
    private var authJob: Job? = null

    init {
        viewModelScope.launch {
            container.repository.observeAccounts().collectLatest { accounts ->
                val selected = _state.value.selectedAccount?.let { old -> accounts.firstOrNull { it.id == old.id } }
                    ?: accounts.firstOrNull()
                _state.update { it.copy(accounts = accounts, selectedAccount = selected, loading = false) }
                observeSnapshot(selected)
            }
        }
    }

    fun onEvent(event: DashboardEvent) {
        when (event) {
            DashboardEvent.AddAccount -> authenticate()
            DashboardEvent.CancelAuthentication -> cancelAuthentication()
            is DashboardEvent.SelectAccount -> {
                _state.update { it.copy(selectedAccount = event.account, snapshot = null) }
                observeSnapshot(event.account)
            }
            is DashboardEvent.StartRenamingAccount -> _state.update {
                it.copy(accountBeingRenamed = event.account, renameError = null)
            }
            DashboardEvent.CancelRenamingAccount -> {
                if (!_state.value.renamingAccount) {
                    _state.update { it.copy(accountBeingRenamed = null, renameError = null) }
                }
            }
            is DashboardEvent.RenameAccount -> renameAccount(event.displayName)
            DashboardEvent.Refresh -> refresh()
        }
    }

    private fun renameAccount(displayName: String) {
        val account = _state.value.accountBeingRenamed ?: return
        if (_state.value.renamingAccount) return
        viewModelScope.launch(Dispatchers.IO) {
            _state.update { it.copy(renamingAccount = true, renameError = null) }
            runCatching {
                container.repository.renameAccount(account.id.value, displayName)
                runCatching {
                    container.database.dao().widgetIdsForAccount(account.id.value).forEach { widgetId ->
                        WidgetRenderWorker.enqueue(context, widgetId)
                    }
                }
            }.fold(
                onSuccess = {
                    _state.update {
                        it.copy(accountBeingRenamed = null, renamingAccount = false, renameError = null)
                    }
                },
                onFailure = { error ->
                    _state.update {
                        it.copy(renamingAccount = false, renameError = error.message ?: "Could not rename account")
                    }
                },
            )
        }
    }

    private fun observeSnapshot(account: ProviderAccount?) {
        snapshotJob?.cancel()
        if (account == null) {
            _state.update { it.copy(snapshot = null) }
            return
        }
        snapshotJob = viewModelScope.launch {
            container.repository.observeSnapshot(account.id.value).collectLatest { snapshot ->
                _state.update { it.copy(snapshot = snapshot) }
            }
        }
        if (_state.value.snapshot == null) refresh()
    }

    private fun authenticate() {
        if (authJob?.isActive == true) return
        authJob = viewModelScope.launch(Dispatchers.IO) {
            runCatching {
                val provider = container.providers.require(ing.boykiss.usagewidgets.domain.ProviderId("codex"))
                val session = provider.authenticator.beginAuthentication()
                _state.update { it.copy(authSession = session, authError = null) }
                repeat(180) {
                    delay(session.pollIntervalSeconds * 1000L)
                    when (val progress = provider.authenticator.pollAuthentication(session)) {
                        AuthenticationProgress.Pending -> Unit
                        is AuthenticationProgress.Complete -> {
                            container.repository.saveAccount(progress.account)
                            UsageSyncWorker.schedule(context, progress.account.providerId.value, progress.account.id.value)
                            container.repository.refresh(progress.account.id.value)
                            _state.update { it.copy(authSession = null) }
                            return@runCatching
                        }
                    }
                }
                error("Sign-in timed out")
            }.onFailure { error ->
                _state.update { it.copy(authSession = null, authError = error.message ?: "Sign-in failed") }
            }
        }
    }

    private fun cancelAuthentication() {
        authJob?.cancel()
        _state.update { it.copy(authSession = null) }
    }

    private fun refresh() {
        val account = _state.value.selectedAccount ?: return
        viewModelScope.launch(Dispatchers.IO) {
            _state.update { it.copy(refreshing = true) }
            container.repository.refresh(account.id.value)
            _state.update { it.copy(refreshing = false) }
        }
    }

    class Factory(private val container: AppContainer, private val context: Context) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T = DashboardViewModel(container, context) as T
    }
}
