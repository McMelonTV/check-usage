package ing.boykiss.aiusagewidgets.widget

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.OutOfQuotaPolicy
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf

class WidgetRenderWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val appWidgetId = inputData.getInt(KEY_APP_WIDGET_ID, -1)
        if (appWidgetId < 0) return Result.failure()

        return runCatching {
            WidgetUpdater.update(applicationContext, appWidgetId)
        }.fold(
            onSuccess = { Result.success() },
            onFailure = { if (runAttemptCount < 3) Result.retry() else Result.failure() },
        )
    }

    companion object {
        private const val KEY_APP_WIDGET_ID = "app_widget_id"

        fun enqueue(context: Context, appWidgetId: Int) {
            val request = OneTimeWorkRequestBuilder<WidgetRenderWorker>()
                .setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
                .setInputData(workDataOf(KEY_APP_WIDGET_ID to appWidgetId))
                .build()
            WorkManager.getInstance(context).enqueueUniqueWork(
                "render-widget-$appWidgetId",
                ExistingWorkPolicy.REPLACE,
                request,
            )
        }
    }
}
