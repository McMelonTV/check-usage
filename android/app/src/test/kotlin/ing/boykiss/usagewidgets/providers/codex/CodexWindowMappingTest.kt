package ing.boykiss.usagewidgets.providers.codex

import ing.boykiss.usagewidgets.domain.UsageMetricKind
import org.junit.Assert.assertEquals
import org.junit.Test

class CodexWindowMappingTest {
    @Test fun fiveHourWindowMapsToShortLimit() {
        assertEquals(UsageMetricKind.SHORT_WINDOW, codexWindowKind(18_000, UsageMetricKind.LONG_WINDOW))
    }

    @Test fun weeklyPrimaryMapsToLongLimit() {
        assertEquals(UsageMetricKind.LONG_WINDOW, codexWindowKind(604_800, UsageMetricKind.SHORT_WINDOW))
    }

    @Test fun missingDurationUsesPayloadPositionFallback() {
        assertEquals(UsageMetricKind.SHORT_WINDOW, codexWindowKind(null, UsageMetricKind.SHORT_WINDOW))
        assertEquals(UsageMetricKind.LONG_WINDOW, codexWindowKind(null, UsageMetricKind.LONG_WINDOW))
    }
}
