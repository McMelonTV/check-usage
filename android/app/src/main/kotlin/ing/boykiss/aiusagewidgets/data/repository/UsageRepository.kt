package ing.boykiss.aiusagewidgets.data.repository

import ing.boykiss.aiusagewidgets.data.database.AccountEntity
import ing.boykiss.aiusagewidgets.data.database.SnapshotEntity
import ing.boykiss.aiusagewidgets.data.database.UsageWidgetsDao
import ing.boykiss.aiusagewidgets.domain.AuthenticationState
import ing.boykiss.aiusagewidgets.domain.CreditMetric
import ing.boykiss.aiusagewidgets.domain.DataFreshness
import ing.boykiss.aiusagewidgets.domain.ProviderAccount
import ing.boykiss.aiusagewidgets.domain.ProviderAccountId
import ing.boykiss.aiusagewidgets.domain.ProviderId
import ing.boykiss.aiusagewidgets.domain.ProviderUsageSnapshot
import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import ing.boykiss.aiusagewidgets.domain.UsageWindow
import ing.boykiss.aiusagewidgets.domain.normalizedAccountDisplayName
import ing.boykiss.aiusagewidgets.domain.remainingPercent
import ing.boykiss.aiusagewidgets.providers.api.ProviderRegistry
import ing.boykiss.aiusagewidgets.providers.codex.AuthenticationRequiredException
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

class UsageRepository(
    private val dao: UsageWidgetsDao,
    private val providers: ProviderRegistry,
) {
    fun observeAccounts(): Flow<List<ProviderAccount>> = dao.observeAccounts().map { rows -> rows.map(AccountEntity::toDomain) }

    suspend fun accounts(): List<ProviderAccount> = dao.accounts().map(AccountEntity::toDomain)

    suspend fun saveAccount(account: ProviderAccount) = dao.upsertAccount(account.toEntity())

    suspend fun renameAccount(accountId: String, displayName: String) {
        val normalizedName = normalizedAccountDisplayName(displayName)
        check(dao.renameAccount(accountId, normalizedName) == 1) { "Account not found" }
    }

    suspend fun removeAccount(account: ProviderAccount) {
        providers.require(account.providerId).authenticator.removeCredentials(account.id.value)
        dao.deleteAccount(account.id.value)
    }

    fun observeSnapshot(accountId: String): Flow<ProviderUsageSnapshot?> = dao.observeSnapshot(accountId).map { it?.toDomain() }

    suspend fun snapshot(accountId: String): ProviderUsageSnapshot? = dao.snapshot(accountId)?.toDomain()

    suspend fun refresh(accountId: String): Result<ProviderUsageSnapshot> {
        val accountEntity = dao.account(accountId) ?: return Result.failure(IllegalArgumentException("Account not found"))
        val account = accountEntity.toDomain()
        return runCatching {
            providers.require(account.providerId).usageSource.fetchUsage(account).also {
                dao.upsertSnapshot(it.toEntity())
                if (account.authenticationState != AuthenticationState.CONNECTED) {
                    dao.upsertAccount(account.copy(authenticationState = AuthenticationState.CONNECTED).toEntity())
                }
            }
        }.onFailure { error ->
            if (error is AuthenticationRequiredException) {
                dao.upsertAccount(account.copy(authenticationState = AuthenticationState.SIGN_IN_REQUIRED).toEntity())
            }
        }
    }

    private fun ProviderAccount.toEntity() = AccountEntity(
        id.value, providerId.value, displayName, identityLabel, planLabel, authenticationState.name,
    )

    private fun ProviderUsageSnapshot.toEntity(): SnapshotEntity {
        val short = windows.firstOrNull { it.kind == UsageMetricKind.SHORT_WINDOW }
        val long = windows.firstOrNull { it.kind == UsageMetricKind.LONG_WINDOW }
        return SnapshotEntity(
            accountId.value, providerId.value,
            short?.usedPercent, short?.resetsAtEpochSeconds, short?.windowSeconds,
            long?.usedPercent, long?.resetsAtEpochSeconds, long?.windowSeconds,
            credits?.availableCount, credits?.totalEarnedCount, credits?.earliestExpiryEpochSeconds,
            fetchedAtEpochMillis, errorMessage,
        )
    }

    private fun SnapshotEntity.toDomain(): ProviderUsageSnapshot = ProviderUsageSnapshot(
        ProviderId(providerId), ProviderAccountId(accountId),
        listOfNotNull(
            shortUsed?.let { UsageWindow(UsageMetricKind.SHORT_WINDOW, "5H", it, it.remainingPercent(), shortResetAt, shortWindowSeconds) },
            longUsed?.let { UsageWindow(UsageMetricKind.LONG_WINDOW, "7D", it, it.remainingPercent(), longResetAt, longWindowSeconds) },
        ),
        if (availableCredits != null || totalCredits != null) CreditMetric(availableCredits, totalCredits, earliestCreditExpiry) else null,
        fetchedAt,
        if (System.currentTimeMillis() - fetchedAt > 30 * 60_000L) DataFreshness.STALE else DataFreshness.FRESH,
        errorMessage,
    )
}
