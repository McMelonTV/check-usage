package ing.boykiss.usagewidgets.data.database

import androidx.room.Dao
import androidx.room.Delete
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

@Dao
interface UsageWidgetsDao {
    @Query("SELECT * FROM provider_accounts ORDER BY displayName COLLATE NOCASE")
    fun observeAccounts(): Flow<List<AccountEntity>>

    @Query("SELECT * FROM provider_accounts ORDER BY displayName COLLATE NOCASE")
    suspend fun accounts(): List<AccountEntity>

    @Query("SELECT * FROM provider_accounts WHERE id = :id")
    suspend fun account(id: String): AccountEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAccount(account: AccountEntity)

    @Query("UPDATE provider_accounts SET displayName = :displayName WHERE id = :id")
    suspend fun renameAccount(id: String, displayName: String): Int

    @Query("DELETE FROM provider_accounts WHERE id = :id")
    suspend fun deleteAccount(id: String)

    @Query("SELECT * FROM usage_snapshots WHERE accountId = :accountId")
    fun observeSnapshot(accountId: String): Flow<SnapshotEntity?>

    @Query("SELECT * FROM usage_snapshots WHERE accountId = :accountId")
    suspend fun snapshot(accountId: String): SnapshotEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertSnapshot(snapshot: SnapshotEntity)

    @Query("SELECT * FROM widget_configurations WHERE appWidgetId = :id")
    suspend fun widgetConfiguration(id: Int): WidgetConfigurationEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertWidgetConfiguration(configuration: WidgetConfigurationEntity)

    @Query("DELETE FROM widget_configurations WHERE appWidgetId IN (:ids)")
    suspend fun deleteWidgetConfigurations(ids: List<Int>)

    @Query("SELECT appWidgetId FROM widget_configurations WHERE accountId = :accountId")
    suspend fun widgetIdsForAccount(accountId: String): List<Int>
}
