package ing.boykiss.aiusagewidgets.data.credentials

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

@Serializable
data class ProviderCredentials(
    val accessToken: String,
    val refreshToken: String,
    val idToken: String,
    val remoteAccountId: String? = null,
    val apiKey: String = "",
    val expiresAt: Long = 0,
    val planCheckedAt: Long = 0,
)

class CredentialStore(context: Context) {
    private val json = Json { ignoreUnknownKeys = true }
    private val preferences = EncryptedSharedPreferences.create(
        context,
        "provider_credentials",
        MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    fun get(accountId: String): ProviderCredentials? = preferences.getString(accountId, null)?.let {
        runCatching { json.decodeFromString<ProviderCredentials>(it) }.getOrNull()
    }

    fun put(accountId: String, credentials: ProviderCredentials) {
        check(preferences.edit().putString(accountId, json.encodeToString(credentials)).commit())
    }

    fun move(fromAccountId: String, toAccountId: String) {
        val stored = checkNotNull(preferences.getString(fromAccountId, null)) { "Sign-in credentials missing" }
        check(preferences.edit().putString(toAccountId, stored).remove(fromAccountId).commit())
    }

    fun remove(accountId: String) {
        check(preferences.edit().remove(accountId).commit())
    }
}
