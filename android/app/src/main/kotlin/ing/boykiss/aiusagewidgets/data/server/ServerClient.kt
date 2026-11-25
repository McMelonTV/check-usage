package ing.boykiss.aiusagewidgets.data.server

import ing.boykiss.aiusagewidgets.domain.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.*
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL

@Serializable data class ServerAccount(
    val id: String, val name: String, val provider: String,
    val email: String = "", @SerialName("plan_type") val plan: String = "",
) {
    fun toDomain() = ProviderAccount(ProviderAccountId(id), ProviderId(provider), name,
        email.takeIf(String::isNotBlank), plan.takeIf(String::isNotBlank))
}
@Serializable data class ServerMetric(
    val slot: String, val label: String, val scope: String = "",
    @SerialName("used_percent") val used: Double? = null,
    @SerialName("reset_at") val resetAt: Long? = null,
) {
    fun toDomain() = UsageWindow(
        when (slot) {
            "session" -> UsageMetricKind.SHORT_WINDOW
            "weekly" -> UsageMetricKind.LONG_WINDOW
            "monthly" -> UsageMetricKind.MONTHLY_WINDOW
            else -> error("Server reported an unsupported usage window")
        }, when {
            scope.isBlank() -> label
            label.equals("$scope $slot", true) -> "$scope ${if (slot == "session") "5-hour" else slot}"
            else -> scope
        }, used, used.remainingPercent(), resetAt, null, scope,
    )
}
@Serializable data class ServerCreditSummary(
    @SerialName("available_count") val available: Int? = null,
    @SerialName("total_earned_count") val total: Int? = null,
    @SerialName("earliest_expiry_epoch_seconds") val expiry: Long? = null,
)
@Serializable data class ServerSnapshot(val credits: ServerCreditSummary? = null)
@Serializable data class ServerUsage(
    val account: ServerAccount, val metrics: List<ServerMetric> = emptyList(), val cached: Boolean = false,
    val error: String? = null, val snapshot: ServerSnapshot? = null,
    @SerialName("fetched_at_epoch_millis") val fetchedAt: Long = 0,
    @SerialName("retry_at_epoch_millis") val retryAt: Long = 0,
) {
    fun toDomain() = ProviderUsageSnapshot(ProviderId(account.provider), ProviderAccountId(account.id),
        metrics.map(ServerMetric::toDomain), snapshot?.credits?.let { CreditMetric(it.available, it.total, it.expiry) }, fetchedAt,
        if (error != null) DataFreshness.ERROR else if (fetchedAt == 0L || System.currentTimeMillis() - fetchedAt > 30 * 60_000L) DataFreshness.STALE else DataFreshness.FRESH,
        error, planLabel = account.plan.takeIf(String::isNotBlank), retryAtEpochMillis = retryAt)
}
@Serializable data class ServerResetCredits(
    @SerialName("available_count") val available: Int,
    @SerialName("total_earned_count") val total: Int? = null,
    val credits: List<ResetCredit> = emptyList(),
)
@Serializable private data class ServerResetResult(val credits: ServerResetCredits)

interface UsageServer {
    val connections: ConnectionStore
    suspend fun pair(address: String, code: String): ServerConnection
    suspend fun accounts(connection: ServerConnection = checkNotNull(connections.get())): List<ServerAccount>
    suspend fun usage(account: String = "", refresh: Boolean = true, connection: ServerConnection = checkNotNull(connections.get())): List<ServerUsage>
    suspend fun rename(account: String, name: String)
    suspend fun remove(account: String)
    suspend fun resets(account: String): ServerResetCredits
}

class ServerClient(override val connections: ConnectionStore, private val json: Json) : UsageServer {
    val configured: Boolean get() = connections.get() != null
    val address: String get() = connections.get()?.url.orEmpty()

    override suspend fun pair(address: String, code: String): ServerConnection {
        val origin = validateServerURL(address)
        val result = request(origin, "", "", "/v1/pair", buildJsonObject { put("code", code.trim()) }).jsonObject
        val connection = ServerConnection(origin, result["token"]?.jsonPrimitive?.content ?: error("Server returned no device token"),
            result["server_id"]?.jsonPrimitive?.content ?: error("Server returned no identity"))
        val info = request(origin, connection.token, connection.serverId, "/v1/info", null).jsonObject
        require(info["server_id"]?.jsonPrimitive?.content == connection.serverId) { "Server identity changed" }
        require(info["protocol_version"]?.jsonPrimitive?.content?.startsWith("2.") == true) { "Server protocol is incompatible" }
        return connection.copy(access = info["device"]?.jsonObject?.get("access")?.jsonPrimitive?.content ?: "read")
    }

    override suspend fun accounts(connection: ServerConnection): List<ServerAccount> =
        json.decodeFromJsonElement(call(connection, "accounts.list", buildJsonObject {}))

    override suspend fun usage(account: String, refresh: Boolean, connection: ServerConnection): List<ServerUsage> =
        json.decodeFromJsonElement(call(connection, "usage.get", buildJsonObject { put("account", account); put("refresh", refresh) }))

    override suspend fun rename(account: String, name: String) {
        call(requireConnection(), "accounts.rename", buildJsonObject { put("account", account); put("new_name", name) })
    }

    override suspend fun remove(account: String) {
        call(requireConnection(), "accounts.remove", buildJsonObject { put("account", account) })
    }

    override suspend fun resets(account: String): ServerResetCredits = json.decodeFromJsonElement<ServerResetResult>(
        call(requireConnection(), "resets.get", buildJsonObject { put("account", account); put("refresh", true) })).credits

    private fun requireConnection() = connections.get() ?: error("Connect to your usage server first")

    private suspend fun call(connection: ServerConnection, method: String, params: JsonObject): JsonElement {
        val result = request(connection.url, connection.token, connection.serverId, "/v1/rpc", buildJsonObject {
            put("jsonrpc", "2.0"); put("id", 1); put("method", method); put("params", params)
        }).jsonObject
        require(result["jsonrpc"]?.jsonPrimitive?.content == "2.0" && result["id"]?.jsonPrimitive?.int == 1) { "Invalid server response" }
        result["error"]?.takeUnless { it is JsonNull }?.jsonObject?.let { error ->
            throw IOException((error["data"] as? JsonPrimitive)?.content ?: "Server operation failed")
        }
        return result["result"] ?: error("Server returned no result")
    }

    private suspend fun request(origin: String, token: String, serverId: String, path: String, body: JsonElement?): JsonElement = withContext(Dispatchers.IO) {
        val connection = URL(origin + path).openConnection() as HttpURLConnection
        try {
            connection.connectTimeout = 15_000
            connection.readTimeout = 120_000
            connection.instanceFollowRedirects = false
            connection.requestMethod = if (body == null) "GET" else "POST"
            connection.setRequestProperty("Accept", "application/json")
            if (token.isNotEmpty()) connection.setRequestProperty("Authorization", "Bearer $token")
            if (body != null) {
                connection.setRequestProperty("Content-Type", "application/json")
                connection.doOutput = true
                connection.outputStream.use { it.write(json.encodeToString(body).toByteArray(Charsets.UTF_8)) }
            }
            val status = connection.responseCode
            if (serverId.isNotEmpty() && connection.getHeaderField("X-Check-Usage-Server-ID") != serverId) error("Server identity changed; reconnect explicitly")
            when (status) {
                401 -> error("Device authorization rejected. Pair again or check whether this device was revoked.")
                403 -> error("This device has read-only access. Enable account changes when pairing to rename or remove shared accounts.")
                200 -> Unit
                else -> error("Usage server returned HTTP $status")
            }
            val bytes = connection.inputStream.use { input ->
                val output = java.io.ByteArrayOutputStream()
                val buffer = ByteArray(8192)
                while (output.size() <= 4 * 1024 * 1024) {
                    val count = input.read(buffer)
                    if (count < 0) break
                    output.write(buffer, 0, count)
                }
                output.toByteArray()
            }
            require(bytes.size <= 4 * 1024 * 1024) { "Server response is too large" }
            json.parseToJsonElement(bytes.toString(Charsets.UTF_8))
        } finally { connection.disconnect() }
    }
}

internal fun validateServerURL(value: String): String {
    val url = URI(value.trim())
    require(url.host != null && url.rawUserInfo == null && url.rawQuery == null && url.rawFragment == null && url.rawPath in listOf("", "/")) { "Enter the server origin without a path or credentials" }
    if (url.scheme != "https") {
        val host = url.host.removePrefix("[").removeSuffix("]").lowercase()
        val v4 = host.split('.').mapNotNull(String::toIntOrNull)
        val privateTailnet = v4.size == 4 && v4.all { it in 0..255 } && v4[0] == 100 && v4[1] in 64..127
        require(url.scheme == "http" && (host == "localhost" || host == "::1" || (v4.size == 4 && v4.all { it in 0..255 } && v4[0] == 127) || host.startsWith("fd7a:115c:a1e0:") || privateTailnet)) { "Use Tailscale HTTPS or an explicit Tailscale IP" }
    }
    return value.trim().trimEnd('/')
}

data class PairingInvite(val url: String, val code: String)

internal fun parsePairingInvite(raw: String): PairingInvite? {
    val trimmed = raw.trim()
    if (!trimmed.startsWith("check-usage://", ignoreCase = true)) return null
    val uri = runCatching { URI(trimmed) }.getOrNull() ?: return null
    if (!uri.scheme.equals("check-usage", ignoreCase = true) || !uri.host.equals("pair", ignoreCase = true)) return null
    val params = (uri.rawQuery ?: return null)
        .split('&')
        .mapNotNull { part ->
            val idx = part.indexOf('=')
            if (idx <= 0) null
            else java.net.URLDecoder.decode(part.substring(0, idx), "UTF-8") to
                java.net.URLDecoder.decode(part.substring(idx + 1), "UTF-8")
        }
        .toMap()
    val rawUrl = params["url"]?.trim().orEmpty()
    val code = params["code"]?.trim().orEmpty()
    if (rawUrl.isEmpty() || code.isEmpty()) return null
    val validUrl = runCatching { validateServerURL(rawUrl) }.getOrNull() ?: return null
    return PairingInvite(validUrl, code)
}
