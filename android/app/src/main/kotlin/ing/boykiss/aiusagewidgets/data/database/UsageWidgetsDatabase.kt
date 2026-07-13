package ing.boykiss.aiusagewidgets.data.database

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(
    entities = [AccountEntity::class, SnapshotEntity::class, WidgetConfigurationEntity::class],
    version = 1,
    exportSchema = true,
)
abstract class UsageWidgetsDatabase : RoomDatabase() {
    abstract fun dao(): UsageWidgetsDao

    companion object {
        fun create(context: Context): UsageWidgetsDatabase = Room.databaseBuilder(
            context.applicationContext,
            UsageWidgetsDatabase::class.java,
            "usage_widgets.db",
        ).build()
    }
}
