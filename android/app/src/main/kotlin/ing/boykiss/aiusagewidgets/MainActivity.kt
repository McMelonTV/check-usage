package ing.boykiss.aiusagewidgets

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import ing.boykiss.aiusagewidgets.ui.dashboard.DashboardScreen
import ing.boykiss.aiusagewidgets.ui.dashboard.DashboardViewModel
import ing.boykiss.aiusagewidgets.ui.theme.UsageWidgetsTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val app = application as UsageWidgetsApplication
        setContent {
            UsageWidgetsTheme {
                val vm: DashboardViewModel = viewModel(factory = DashboardViewModel.Factory(app.container, applicationContext))
                val state by vm.state.collectAsStateWithLifecycle()
                DashboardScreen(state, vm::onEvent)
            }
        }
    }
}
