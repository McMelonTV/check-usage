package ing.boykiss.aiusagewidgets

import android.app.Application
import ing.boykiss.aiusagewidgets.widget.WidgetUpdater
import ing.boykiss.aiusagewidgets.widget.WidgetRenderWorker
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger

class UsageWidgetsApplication : Application() {
    val container: AppContainer by lazy { AppContainer(this) }

    private val applicationScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val renderJobs = ConcurrentHashMap<Int, Job>()
    private val renderGenerations = ConcurrentHashMap<Int, AtomicInteger>()

    fun renderWidgetAfterConfiguration(appWidgetId: Int) {
        val generation = renderGenerations
            .computeIfAbsent(appWidgetId) { AtomicInteger() }
            .incrementAndGet()
        val job = applicationScope.launch(start = CoroutineStart.LAZY) {
            var renderedAtLeastOnce = false
            // Nothing Launcher can apply its initial RemoteViews after an early successful
            // update. Repaint across the whole commit window instead of stopping at success.
            for (delayMillis in listOf(250L, 500L, 750L, 1_000L, 1_500L)) {
                delay(delayMillis)
                if (renderGenerations[appWidgetId]?.get() != generation) return@launch
                val rendered = withTimeoutOrNull(2_000L) {
                    runCatching {
                        WidgetUpdater.update(this@UsageWidgetsApplication, appWidgetId)
                    }.isSuccess
                } == true
                renderedAtLeastOnce = renderedAtLeastOnce || rendered
            }
            if (!renderedAtLeastOnce && renderGenerations[appWidgetId]?.get() == generation) {
                WidgetRenderWorker.enqueue(this@UsageWidgetsApplication, appWidgetId)
            }
        }
        renderJobs.put(appWidgetId, job)?.cancel()
        job.invokeOnCompletion { renderJobs.remove(appWidgetId, job) }
        job.start()
    }
}
