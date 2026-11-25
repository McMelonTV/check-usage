package ing.boykiss.aiusagewidgets.providers.shared

import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import org.junit.Assert.*
import org.junit.Test
import kotlinx.serialization.json.Json

class SharedMetricTest {
    @Test fun antigravityKeepsBothGroupsAndTheirWindowLabels() {
        val windows = listOf(
            SharedMetric("session", "GEMINI SESSION", 25.0, scope = "Gemini"),
            SharedMetric("weekly", "GEMINI WEEKLY", 50.0, scope = "Gemini"),
            SharedMetric("session", "CLAUDE/GPT SESSION", 75.0, scope = "Claude/GPT"),
            SharedMetric("weekly", "CLAUDE/GPT WEEKLY", scope = "Claude/GPT"),
        ).map { it.toDomain() }
        assertEquals(listOf("Gemini 5-hour", "Gemini weekly", "Claude/GPT 5-hour", "Claude/GPT weekly"), windows.map { it.label })
        assertEquals(listOf(75.0, 50.0, 25.0, null), windows.map { it.remainingPercent })
        assertEquals(2, windows.count { it.kind == UsageMetricKind.SHORT_WINDOW })
        assertEquals(2, windows.count { it.kind == UsageMetricKind.LONG_WINDOW })
    }
    @Test fun cursorPoolKeepsItsNameAndMonthlyReset() {
        val metric = Json.decodeFromString<SharedMetric>("""{"slot":"weekly","label":"CURSOR MODELS","scope":"Cursor models","used_percent":12.0,"reset_at":1790812800}""")
        val window = metric.toDomain()
        assertEquals("Cursor models", window.label)
        assertEquals(88.0, window.remainingPercent!!, 0.0)
        assertEquals(1790812800L, window.resetsAtEpochSeconds)
    }
    @Test fun scopedClaudeWindowDoesNotBecomeAGenericMonthlyLimit() {
        val window = SharedMetric("monthly", "FABLE", 20.0, scope = "Fable weekly").toDomain()
        assertEquals(UsageMetricKind.MONTHLY_WINDOW, window.kind)
        assertEquals("Fable weekly", window.label)
    }
    @Test fun missingPoolStaysUnknown() {
        val window = SharedMetric("monthly", "OTHER MODELS", scope = "Other models").toDomain()
        assertNull(window.usedPercent)
        assertNull(window.remainingPercent)
    }
}
