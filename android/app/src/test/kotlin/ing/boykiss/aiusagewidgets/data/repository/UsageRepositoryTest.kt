package ing.boykiss.aiusagewidgets.data.repository

import ing.boykiss.aiusagewidgets.data.database.*
import ing.boykiss.aiusagewidgets.domain.*
import ing.boykiss.aiusagewidgets.providers.api.*
import ing.boykiss.aiusagewidgets.providers.codex.AuthenticationRequiredException
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import org.junit.Assert.*
import org.junit.Test

class UsageRepositoryTest {
    private val account = ProviderAccount(ProviderAccountId("account"), ProviderId("fixture"), "Fixture", null, "Plan")
    private fun snapshot() = ProviderUsageSnapshot(account.providerId, account.id, listOf(
        UsageWindow(UsageMetricKind.SHORT_WINDOW, "Session", 10.0, 90.0, 123, null),
        UsageWindow(UsageMetricKind.LONG_WINDOW, "Cursor models", null, null, 456, null),
        UsageWindow(UsageMetricKind.MONTHLY_WINDOW, "Fable weekly", 30.0, 70.0, 789, null),
    ), CreditMetric(0, 3, null), System.currentTimeMillis(), DataFreshness.FRESH,
        resetDetails = emptyList(), planLabel = "USD 12.34")

    private fun fixture(fetch: suspend () -> ProviderUsageSnapshot): Triple<UsageRepository, FakeDao, FakeProvider> {
        val dao = FakeDao()
        val provider = FakeProvider(fetch)
        return Triple(UsageRepository(dao, ProviderRegistry(listOf(provider))), dao, provider)
    }

    @Test fun cachedSnapshotRetainsMonthlyScopeMissingValuesAndZeroResets() = runBlocking {
        val expected = snapshot()
        val (repository, dao) = fixture { expected }
        repository.saveAccount(account)
        repository.refresh(account.id.value).getOrThrow()
        val reopened = UsageRepository(dao, ProviderRegistry(emptyList()))
        assertEquals(expected, reopened.snapshot(account.id.value))
        assertTrue(dao.snapshot(account.id.value)!!.snapshotJson!!.contains("Fable weekly"))
    }

    @Test fun legacyCodexSnapshotsSurviveTheSchemaUpgrade() = runBlocking {
        val dao = FakeDao()
        dao.upsertSnapshot(SnapshotEntity("legacy", "codex", 12.0, 123, 18000,
            34.0, 456, 604800, 0, 7, null, System.currentTimeMillis(), null))
        val cached = UsageRepository(dao, ProviderRegistry(emptyList())).snapshot("legacy")!!
        assertEquals(listOf("5H", "7D"), cached.windows.map { it.label })
        assertEquals(0, cached.credits!!.availableCount)
        assertEquals(88.0, cached.windows.first().remainingPercent!!, 0.0)
    }

    @Test fun failedFetchPreservesCachedValuesAndMarksSignInRequired() = runBlocking {
        var fail = false
        val (repository, dao) = fixture {
            if (fail) throw AuthenticationRequiredException("Sign in again")
            snapshot()
        }
        repository.saveAccount(account)
        val before = repository.refresh(account.id.value).getOrThrow()
        fail = true
        assertTrue(repository.refresh(account.id.value).isFailure)
        val after = repository.snapshot(account.id.value)!!
        assertEquals(before.windows, after.windows)
        assertEquals(before.fetchedAtEpochMillis, after.fetchedAtEpochMillis)
        assertEquals(DataFreshness.ERROR, after.freshness)
        assertEquals("Sign in again", after.errorMessage)
        assertEquals(AuthenticationState.SIGN_IN_REQUIRED, dao.account(account.id.value)!!.toDomain().authenticationState)
    }

    @Test fun firstFetchFailureHasAnErrorSnapshotInsteadOfAnEndlessLoader() = runBlocking {
        val (repository) = fixture { error("Offline") }
        repository.saveAccount(account)
        assertTrue(repository.refresh(account.id.value).isFailure)
        val cached = repository.snapshot(account.id.value)!!
        assertEquals("Offline", cached.errorMessage)
        assertTrue(cached.windows.isEmpty())
        assertEquals(0, cached.fetchedAtEpochMillis)
    }

    @Test fun providerBackoffPreservesCacheAndDelaysFurtherRequests() = runBlocking {
        var calls = 0
        val expected = snapshot()
        val (repository) = fixture {
            calls++
            if (calls == 1) expected else expected.copy(windows = emptyList(),
                freshness = DataFreshness.ERROR, errorMessage = "Rate limited", retryAtEpochMillis = System.currentTimeMillis() + 300_000)
        }
        repository.saveAccount(account)
        repository.refresh(account.id.value)
        repository.refresh(account.id.value)
        repository.refresh(account.id.value)
        assertEquals(2, calls)
        assertEquals(expected.windows, repository.snapshot(account.id.value)!!.windows)
        assertEquals("Rate limited", repository.snapshot(account.id.value)!!.errorMessage)
        repository.clearRefreshDelay(account.id.value)
        repository.refresh(account.id.value)
        assertEquals(3, calls)
    }

    @Test fun removingAccountDeletesCredentialsSnapshotsAndWidgetConfigurations() = runBlocking {
        val (repository, dao, provider) = fixture { snapshot() }
        repository.saveAccount(account)
        repository.refresh(account.id.value)
        dao.upsertWidgetConfiguration(WidgetConfigurationEntity(12, "fixture", account.id.value))
        repository.removeAccount(account)
        assertTrue(provider.credentialsRemoved)
        assertNull(repository.snapshot(account.id.value))
        assertNull(dao.account(account.id.value))
        assertNull(dao.widgetConfiguration(12))
    }

    @Test fun concurrentRefreshAndRemovalCannotResurrectAccountData() = runBlocking {
        val entered = CompletableDeferred<Unit>()
        val finish = CompletableDeferred<Unit>()
        val (repository, dao) = fixture { entered.complete(Unit); finish.await(); snapshot() }
        repository.saveAccount(account)
        val refresh = async { repository.refresh(account.id.value) }
        entered.await()
        val remove = async { repository.removeAccount(account) }
        finish.complete(Unit)
        refresh.await()
        remove.await()
        assertNull(dao.account(account.id.value))
        assertNull(repository.snapshot(account.id.value))
    }

    @Test fun reauthenticationPreservesLatestNameIdAndWidgetAssignment() = runBlocking {
        val (repository, dao, provider) = fixture { snapshot().copy(retryAtEpochMillis = Long.MAX_VALUE) }
        repository.saveAccount(account)
        repository.refresh(account.id.value)
        dao.upsertWidgetConfiguration(WidgetConfigurationEntity(12, "fixture", account.id.value))
        repository.renameAccount(account.id.value, "Renamed during login")
        val authenticated = account.copy(id = ProviderAccountId("temporary-login"), displayName = "New identity", identityLabel = "new@example.com")
        val saved = repository.saveAuthenticatedAccount(authenticated, account)
        assertEquals(account.id, saved.id)
        assertEquals("Renamed during login", saved.displayName)
        assertEquals("new@example.com", saved.identityLabel)
        assertEquals("temporary-login" to "account", provider.replacedCredentials)
        assertEquals(account.id.value, dao.widgetConfiguration(12)!!.accountId)
        assertEquals(0L, repository.snapshot(account.id.value)!!.retryAtEpochMillis)
    }

    @Test fun coroutineCancellationPropagatesWithoutBecomingAnErrorSnapshot() = runBlocking {
        val (repository) = fixture { throw CancellationException("Cancelled") }
        repository.saveAccount(account)
        try { repository.refresh(account.id.value); fail("Cancellation swallowed") } catch (_: CancellationException) { }
        assertNull(repository.snapshot(account.id.value))
    }
}

private class FakeProvider(private val fetch: suspend () -> ProviderUsageSnapshot) : UsageProvider, ProviderAuthenticator, ProviderUsageSource {
    override val descriptor = ProviderDescriptor(ProviderId("fixture"), "Fixture", true, UsageMetricKind.entries.toSet())
    override val authenticator get() = this
    override val usageSource get() = this
    var credentialsRemoved = false
    var replacedCredentials: Pair<String, String>? = null
    override suspend fun replaceCredentials(fromAccountId: String, toAccountId: String) { replacedCredentials = fromAccountId to toAccountId }
    override suspend fun fetchUsage(account: ProviderAccount) = fetch()
    override suspend fun beginAuthentication(): AuthenticationSession = error("unused")
    override suspend fun pollAuthentication(session: AuthenticationSession): AuthenticationProgress = error("unused")
    override suspend fun refreshCredentials(account: ProviderAccount) = Unit
    override suspend fun removeCredentials(accountId: String) { credentialsRemoved = true }
}

private class FakeDao : UsageWidgetsDao {
    private val accountRows = MutableStateFlow<List<AccountEntity>>(emptyList())
    private val snapshotRows = MutableStateFlow<List<SnapshotEntity>>(emptyList())
    private val widgets = mutableMapOf<Int, WidgetConfigurationEntity>()
    override fun observeAccounts() = accountRows
    override suspend fun accounts() = accountRows.value
    override suspend fun account(id: String) = accountRows.value.firstOrNull { it.id == id }
    override suspend fun upsertAccount(account: AccountEntity) { accountRows.value = accountRows.value.filterNot { it.id == account.id } + account }
    override suspend fun renameAccount(id: String, displayName: String): Int {
        val row = account(id) ?: return 0
        upsertAccount(row.copy(displayName = displayName))
        return 1
    }
    override suspend fun deleteAccount(id: String) { accountRows.value = accountRows.value.filterNot { it.id == id } }
    override fun observeSnapshot(accountId: String) = snapshotRows.map { rows -> rows.firstOrNull { it.accountId == accountId } }
    override fun observeSnapshots() = snapshotRows
    override suspend fun snapshot(accountId: String) = snapshotRows.value.firstOrNull { it.accountId == accountId }
    override suspend fun upsertSnapshot(snapshot: SnapshotEntity) { snapshotRows.value = snapshotRows.value.filterNot { it.accountId == snapshot.accountId } + snapshot }
    override suspend fun deleteSnapshot(id: String) { snapshotRows.value = snapshotRows.value.filterNot { it.accountId == id } }
    override suspend fun deleteWidgetsForAccount(id: String) { widgets.entries.removeAll { it.value.accountId == id } }
    override suspend fun widgetConfiguration(id: Int) = widgets[id]
    override suspend fun upsertWidgetConfiguration(configuration: WidgetConfigurationEntity) { widgets[configuration.appWidgetId] = configuration }
    override suspend fun deleteWidgetConfigurations(ids: List<Int>) { ids.forEach(widgets::remove) }
    override suspend fun widgetIdsForAccount(accountId: String) = widgets.values.filter { it.accountId == accountId }.map { it.appWidgetId }
}
