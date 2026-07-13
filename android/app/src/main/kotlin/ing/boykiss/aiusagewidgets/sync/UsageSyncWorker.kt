package ing.boykiss.aiusagewidgets.sync

import android.content.Context
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import ing.boykiss.aiusagewidgets.UsageWidgetsApplication
import ing.boykiss.aiusagewidgets.widget.WidgetUpdater
import java.util.concurrent.TimeUnit

class UsageSyncWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val accountId = inputData.getString(KEY_ACCOUNT_ID) ?: return Result.failure()
        val app = applicationContext as UsageWidgetsApplication
        return app.container.repository.refresh(accountId).fold(
            onSuccess = {
                WidgetUpdater.updateAll(applicationContext)
                Result.success()
            },
            onFailure = { if (runAttemptCount < 3) Result.retry() else Result.failure() },
        )
    }

    companion object {
        const val KEY_ACCOUNT_ID = "account_id"

        fun schedule(context: Context, providerId: String, accountId: String) {
            val request = PeriodicWorkRequestBuilder<UsageSyncWorker>(15, TimeUnit.MINUTES)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .setInputData(androidx.work.workDataOf(KEY_ACCOUNT_ID to accountId))
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(
                "provider-account-sync-$providerId-$accountId",
                ExistingPeriodicWorkPolicy.UPDATE,
                request,
            )
        }
    }
}
