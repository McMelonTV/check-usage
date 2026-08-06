package ing.boykiss.aiusagewidgets.providers.codex

import ing.boykiss.aiusagewidgets.data.credentials.ProviderCredentials
import ing.boykiss.aiusagewidgets.gobridge.codexlogic.Codexlogic
import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

class AuthenticationRequiredException(message: String, cause: Throwable? = null) : IOException(message, cause)
class ProviderCompatibilityException(message: String, cause: Throwable? = null) : IOException(message, cause)

class CodexApiClient(private val json: Json) {
    val verificationUrl: String get() = Codexlogic.verificationURL()

    suspend fun requestDeviceCode(): DeviceCodeResponse = goCall {
        decode(Codexlogic.requestDeviceCode())
    }

    suspend fun pollDeviceCode(deviceAuthId: String, userCode: String): DevicePollResponse? = goCall {
        Codexlogic.pollDeviceCode(deviceAuthId, userCode).takeIf(String::isNotEmpty)?.let(::decode)
    }

    suspend fun exchangeCode(code: String, verifier: String): TokenResponse = goCall {
        decode(Codexlogic.exchangeCode(code, verifier))
    }

    suspend fun identity(idToken: String): IdentityResponse = goCall {
        decode(Codexlogic.identity(idToken))
    }

    suspend fun refreshCredentials(credentials: ProviderCredentials): RefreshCredentialsResponse = goCall {
        val request = GoCredentials(
            credentials.accessToken,
            credentials.refreshToken,
            credentials.idToken,
            credentials.remoteAccountId,
        )
        decode(Codexlogic.refreshCredentials(json.encodeToString(request)))
    }

    suspend fun snapshot(accessToken: String, accountId: String?): GoUsageSnapshot = goCall {
        decode(Codexlogic.fetchSnapshot(accessToken, accountId.orEmpty()))
    }

    private suspend fun <T> goCall(block: () -> T): T = withContext(Dispatchers.IO) {
        try {
            block()
        } catch (error: ProviderCompatibilityException) {
            throw error
        } catch (error: Exception) {
            val message = error.message.orEmpty()
            if (message.startsWith("authentication required:")) {
                throw AuthenticationRequiredException("Sign in again", error)
            }
            if (message.startsWith("compatibility error:")) {
                throw ProviderCompatibilityException("Integration needs an update", error)
            }
            throw IOException(message.ifBlank { "Codex provider request failed" }, error)
        }
    }

    private inline fun <reified T> decode(body: String): T = try {
        json.decodeFromString(body)
    } catch (error: Exception) {
        throw ProviderCompatibilityException("Integration needs an update", error)
    }
}
