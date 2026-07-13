package ing.boykiss.aiusagewidgets.data.database

import androidx.room.Entity
import androidx.room.PrimaryKey
import ing.boykiss.aiusagewidgets.domain.AuthenticationState
import ing.boykiss.aiusagewidgets.domain.ProviderAccount
import ing.boykiss.aiusagewidgets.domain.ProviderAccountId
import ing.boykiss.aiusagewidgets.domain.ProviderId
import ing.boykiss.aiusagewidgets.domain.WidgetVisualStyle

@Entity(tableName = "provider_accounts")
data class AccountEntity(
    @PrimaryKey val id: String,
    val providerId: String,
    val displayName: String,
    val identityLabel: String?,
    val planLabel: String?,
    val authenticationState: String,
) {
    fun toDomain() = ProviderAccount(
        ProviderAccountId(id), ProviderId(providerId), displayName, identityLabel, planLabel,
        AuthenticationState.valueOf(authenticationState),
    )
}

@Entity(tableName = "usage_snapshots")
data class SnapshotEntity(
    @PrimaryKey val accountId: String,
    val providerId: String,
    val shortUsed: Double?,
    val shortResetAt: Long?,
    val shortWindowSeconds: Int?,
    val longUsed: Double?,
    val longResetAt: Long?,
    val longWindowSeconds: Int?,
    val availableCredits: Int?,
    val totalCredits: Int?,
    val earliestCreditExpiry: Long?,
    val fetchedAt: Long,
    val errorMessage: String?,
)

@Entity(tableName = "widget_configurations")
data class WidgetConfigurationEntity(
    @PrimaryKey val appWidgetId: Int,
    val providerId: String,
    val accountId: String,
    val visualStyle: String = WidgetVisualStyle.MATERIAL_YOU.name,
)
