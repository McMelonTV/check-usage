package ing.boykiss.aiusagewidgets.widget

import android.content.Context
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import ing.boykiss.aiusagewidgets.UsageWidgetsApplication
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

class UsageWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = UsageWidget()

    override fun onDeleted(context: Context, appWidgetIds: IntArray) {
        super.onDeleted(context, appWidgetIds)
        val app = context.applicationContext as UsageWidgetsApplication
        CoroutineScope(Dispatchers.IO).launch {
            app.container.database.dao().deleteWidgetConfigurations(appWidgetIds.toList())
        }
    }
}
