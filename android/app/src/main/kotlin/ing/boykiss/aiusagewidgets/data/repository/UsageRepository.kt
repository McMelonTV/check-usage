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
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.CancellationException
import java.util.concurrent.ConcurrentHashMap
import ing.boykiss.aiusagewidgets.providers.codex.CodexUsageProvider
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map

class UsageRepository(
    private val dao: UsageWidgetsDao,
    private val providers: ProviderRegistry,
) {
    private val json = Json { ignoreUnknownKeys = true }
    private val refreshLocks = ConcurrentHashMap<String, Mutex>()
    fun observeSnapshots(): Flow<Map<String, ProviderUsageSnapshot>> = dao.observeSnapshots().map { rows ->
        rows.associate { it.accountId to it.toDomain() }
    }
    fun observeAccounts(): Flow<List<ProviderAccount>> = dao.observeAccounts().map { rows -> rows.map(AccountEntity::toDomain) }

    suspend fun accounts(): List<ProviderAccount> = dao.accounts().map(AccountEntity::toDomain)

    suspend fun saveAccount(account: ProviderAccount) = dao.upsertAccount(account.toEntity())

    suspend fun saveAuthenticatedAccount(account: ProviderAccount, target: ProviderAccount?): ProviderAccount =
        refreshLocks.computeIfAbsent(target?.id?.value ?: account.id.value) { Mutex() }.withLock {
            if (target == null) {
                saveAccount(account)
                return@withLock account
            }
            val authenticator = providers.require(account.providerId).authenticator
            val current = dao.account(target.id.value)?.toDomain()
            if (current == null) {
                authenticator.removeCredentials(account.id.value)
                error("Account was removed")
            }
            check(current.providerId == account.providerId) { "Sign-in provider does not match this account" }
            authenticator.replaceCredentials(account.id.value, current.id.value)
            val saved = account.copy(id = current.id, displayName = current.displayName)
            saveAccount(saved)
            snapshot(saved.id.value)?.let {
                dao.upsertSnapshot(it.copy(retryAtEpochMillis = 0, errorMessage = null,
                    freshness = DataFreshness.STALE).toEntity())
            }
            saved
        }

    suspend fun clearRefreshDelay(accountId: String) = refreshLocks.computeIfAbsent(accountId) { Mutex() }.withLock {
        snapshot(accountId)?.let { dao.upsertSnapshot(it.copy(retryAtEpochMillis = 0).toEntity()) }
    }

    suspend fun renameAccount(accountId: String, displayName: String) = refreshLocks.computeIfAbsent(accountId) { Mutex() }.withLock {
        val normalizedName = normalizedAccountDisplayName(displayName)
        check(dao.renameAccount(accountId, normalizedName) == 1) { "Account not found" }
    }

    suspend fun removeAccount(account: ProviderAccount) = refreshLocks.computeIfAbsent(account.id.value) { Mutex() }.withLock {
        providers.require(account.providerId).authenticator.removeCredentials(account.id.value)
        dao.removeAccountData(account.id.value)
    }

    fun observeSnapshot(accountId: String): Flow<ProviderUsageSnapshot?> = dao.observeSnapshot(accountId).map { it?.toDomain() }

    suspend fun snapshot(accountId: String): ProviderUsageSnapshot? = dao.snapshot(accountId)?.toDomain()

    suspend fun refresh(accountId: String): Result<ProviderUsageSnapshot> =
        refreshLocks.computeIfAbsent(accountId) { Mutex() }.withLock {
            val account = dao.account(accountId)?.toDomain()
                ?: return@withLock Result.failure(IllegalArgumentException("Account not found"))
            val cached = snapshot(accountId)
            if (cached != null && cached.retryAtEpochMillis > System.currentTimeMillis()) {
                return@withLock Result.success(cached)
            }
            try {
                val fetched = providers.require(account.providerId).usageSource.fetchUsage(account)
                val saved = if (fetched.freshness == DataFreshness.ERROR && cached != null) {
                    cached.copy(errorMessage = fetched.errorMessage, freshness = DataFreshness.ERROR,
                        retryAtEpochMillis = fetched.retryAtEpochMillis)
                } else fetched.copy(resetDetails = fetched.resetDetails ?: cached?.resetDetails,
                    planLabel = fetched.planLabel?.takeIf(String::isNotBlank) ?: cached?.planLabel ?: account.planLabel)
                // An account may have been removed while its request was in flight.
                if (dao.account(accountId) != null) {
                    dao.upsertSnapshot(saved.toEntity())
                    dao.upsertAccount(account.copy(authenticationState = AuthenticationState.CONNECTED,
                        planLabel = saved.planLabel?.takeIf(String::isNotBlank) ?: account.planLabel).toEntity())
                }
                Result.success(saved)
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                if (dao.account(accountId) != null) {
                    if (error is AuthenticationRequiredException) {
                        dao.upsertAccount(account.copy(authenticationState = AuthenticationState.SIGN_IN_REQUIRED).toEntity())
                    }
                    val failed = (cached ?: ProviderUsageSnapshot(account.providerId, account.id, emptyList(), null,
                        0, DataFreshness.ERROR)).copy(freshness = DataFreshness.ERROR,
                            errorMessage = error.message ?: "Could not refresh usage")
                    dao.upsertSnapshot(failed.toEntity())
                }
                Result.failure(error)
            }
        }

    suspend fun refreshResets(accountId: String): Result<Unit> =
        refreshLocks.computeIfAbsent(accountId) { Mutex() }.withLock {
            val account = dao.account(accountId)?.toDomain()
                ?: return@withLock Result.failure(IllegalArgumentException("Account not found"))
            val provider = providers.require(account.providerId) as? CodexUsageProvider
                ?: return@withLock Result.failure(IllegalArgumentException("Reset credits are unavailable for this provider"))
            val cached = snapshot(accountId) ?: ProviderUsageSnapshot(account.providerId, account.id,
                emptyList(), null, 0, DataFreshness.STALE)
            try {
                val details = provider.fetchResetDetails(account)
                if (dao.account(accountId) != null) dao.upsertSnapshot(cached.copy(
                    resetDetails = details.credits,
                    credits = CreditMetric(details.availableCount, details.totalEarnedCount,
                        details.credits.filter { it.status.equals("available", true) }
                            .mapNotNull { runCatching { java.time.Instant.parse(it.expiresAt).epochSecond }.getOrNull() }.minOrNull()),
                    resetsError = null,
                ).toEntity())
                Result.success(Unit)
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                if (dao.account(accountId) != null) {
                    dao.upsertSnapshot(cached.copy(resetsError = error.message ?: "Could not refresh resets").toEntity())
                    if (error is AuthenticationRequiredException) dao.upsertAccount(
                        account.copy(authenticationState = AuthenticationState.SIGN_IN_REQUIRED).toEntity())
                }
                Result.failure(error)
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
            fetchedAtEpochMillis, errorMessage, json.encodeToString(this),
        )
    }

    private fun SnapshotEntity.toDomain(): ProviderUsageSnapshot {
        val stored = snapshotJson?.let { runCatching { json.decodeFromString<ProviderUsageSnapshot>(it) }.getOrNull() }
        if (stored != null) return stored.copy(freshness = when {
            stored.errorMessage != null -> DataFreshness.ERROR
            System.currentTimeMillis() - stored.fetchedAtEpochMillis > 30 * 60_000L -> DataFreshness.STALE
            else -> stored.freshness
        })
        return ProviderUsageSnapshot(
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
}
