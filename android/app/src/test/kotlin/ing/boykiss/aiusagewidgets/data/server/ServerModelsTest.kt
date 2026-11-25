package ing.boykiss.aiusagewidgets.data.server

import ing.boykiss.aiusagewidgets.domain.*
import kotlinx.serialization.json.Json
import org.junit.Assert.*
import org.junit.Test

class ServerModelsTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test fun serverUsageKeepsAllFourScopesAndUnknownValues() {
        val response = json.decodeFromString<ServerUsage>("""{
          "account":{"id":"google","name":"Personal","provider":"antigravity"},
          "metrics":[
            {"slot":"session","label":"GEMINI SESSION","scope":"Gemini","used_percent":0},
            {"slot":"weekly","label":"GEMINI WEEKLY","scope":"Gemini","used_percent":25},
            {"slot":"session","label":"CLAUDE/GPT SESSION","scope":"Claude/GPT","used_percent":100},
            {"slot":"weekly","label":"CLAUDE/GPT WEEKLY","scope":"Claude/GPT"}],
          "fetched_at_epoch_millis":1795564800000,"retry_at_epoch_millis":1795565100000
        }""")
        val snapshot = response.toDomain()
        assertEquals(listOf("Gemini 5-hour", "Gemini weekly", "Claude/GPT 5-hour", "Claude/GPT weekly"), snapshot.windows.map { it.label })
        assertEquals(listOf(100.0, 75.0, 0.0, null), snapshot.windows.map { it.remainingPercent })
        assertEquals(1795564800000, snapshot.fetchedAtEpochMillis)
        assertEquals(1795565100000, snapshot.retryAtEpochMillis)
    }

    @Test fun missingNativeResetTotalStaysUnknown() {
        val credits = json.decodeFromString<ServerResetCredits>("""{"available_count":1,"credits":[]}""")
        assertEquals(1, credits.available)
        assertNull(credits.total)
    }

    @Test fun endpointValidationDoesNotSendTokensToUnprotectedPublicHTTP() {
        for (url in listOf("https://homelab.example.ts.net", "http://100.101.102.103:8787", "http://127.0.0.1:8787")) assertEquals(url, validateServerURL(url))
        for (url in listOf("http://public.example.com", "http://127.attacker.example", "https://user:secret@example.com", "https://server/path", "https://server?token=secret", "https://server/#fragment")) {
            assertTrue("Accepted an unsafe endpoint", runCatching { validateServerURL(url) }.isFailure)
        }
    }

    @Test fun pairingInviteParsesValidLinksAndRejectsUnsafeURLs() {
        val invite = parsePairingInvite("check-usage://pair?url=https%3A%2F%2Fhomelab.tail123.ts.net%2F&code=abc123XYZ")
        assertEquals(PairingInvite("https://homelab.tail123.ts.net", "abc123XYZ"), invite)

        val webInvite = parsePairingInvite("https://kube.tail23234d.ts.net:8787/pair#code=xyz789ABC")
        assertEquals(PairingInvite("https://kube.tail23234d.ts.net:8787", "xyz789ABC"), webInvite)

        assertNull(parsePairingInvite("https://homelab.tail123.ts.net"))
        assertNull(parsePairingInvite("https://homelab.tail123.ts.net/pair"))
        assertNull(parsePairingInvite("http://public.example.com/pair#code=abc123XYZ"))
        assertNull(parsePairingInvite("check-usage://pair?url=http%3A%2F%2Fpublic.example.com&code=abc123XYZ"))
        assertNull(parsePairingInvite("check-usage://pair?url=https%3A%2F%2Fhomelab.tail123.ts.net"))
        assertNull(parsePairingInvite("check-usage://other?url=https%3A%2F%2Fhomelab.tail123.ts.net&code=abc"))
    }
}
