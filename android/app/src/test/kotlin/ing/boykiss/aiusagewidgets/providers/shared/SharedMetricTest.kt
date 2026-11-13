package ing.boykiss.aiusagewidgets.providers.shared

import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import org.junit.Assert.*
import org.junit.Test
import kotlinx.serialization.json.Json

class SharedMetricTest {
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
