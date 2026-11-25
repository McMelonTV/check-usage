package ing.boykiss.aiusagewidgets

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import ing.boykiss.aiusagewidgets.data.server.PairingInvite
import ing.boykiss.aiusagewidgets.data.server.parsePairingInvite
import ing.boykiss.aiusagewidgets.ui.dashboard.DashboardEvent
import ing.boykiss.aiusagewidgets.ui.dashboard.DashboardScreen
import ing.boykiss.aiusagewidgets.ui.dashboard.DashboardViewModel
import ing.boykiss.aiusagewidgets.ui.theme.UsageWidgetsTheme
import kotlinx.coroutines.flow.MutableStateFlow

class MainActivity : ComponentActivity() {
    private val incomingInvite = MutableStateFlow<PairingInvite?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        consumePairingIntent(intent)
        val app = application as UsageWidgetsApplication
        setContent {
            UsageWidgetsTheme {
                val vm: DashboardViewModel = viewModel(factory = DashboardViewModel.factory(app.container, applicationContext))
                val state by vm.state.collectAsStateWithLifecycle()
                val invite by incomingInvite.collectAsStateWithLifecycle()
                LaunchedEffect(invite) {
                    val pending = invite ?: return@LaunchedEffect
                    vm.onEvent(DashboardEvent.ReceivePairingInvite(pending.url, pending.code))
                    incomingInvite.value = null
                }
                DashboardScreen(state, vm::onEvent)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        consumePairingIntent(intent)
    }

    private fun consumePairingIntent(intent: Intent?) {
        val raw = intent?.dataString ?: return
        val invite = parsePairingInvite(raw) ?: return
        incomingInvite.value = invite
        intent.data = null
    }
}
