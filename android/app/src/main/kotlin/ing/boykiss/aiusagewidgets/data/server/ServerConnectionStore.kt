package ing.boykiss.aiusagewidgets.data.server

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

data class ServerConnection(val url: String, val token: String, val serverId: String, val access: String = "read")

interface ConnectionStore {
    fun get(): ServerConnection?
    fun put(connection: ServerConnection)
}

class ServerConnectionStore(context: Context) : ConnectionStore {
    private val preferences = EncryptedSharedPreferences.create(
        context, "usage_server_connection",
        MasterKey.Builder(context).setKeyScheme(MasterKey.KeyScheme.AES256_GCM).build(),
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    override fun get(): ServerConnection? {
        val url = preferences.getString("url", null) ?: return null
        val token = preferences.getString("token", null) ?: return null
        val serverId = preferences.getString("server_id", null) ?: return null
        return ServerConnection(url, token, serverId, preferences.getString("access", "read") ?: "read")
    }

    override fun put(connection: ServerConnection) {
        check(preferences.edit().putString("url", connection.url).putString("token", connection.token)
            .putString("server_id", connection.serverId).putString("access", connection.access).commit()) { "Could not save server connection" }
    }
}
