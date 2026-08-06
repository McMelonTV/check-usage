package ing.boykiss.aiusagewidgets.providers.codex

import ing.boykiss.aiusagewidgets.domain.UsageMetricKind
import org.junit.Assert.assertEquals
import org.junit.Test

class CodexWindowMappingTest {
    @Test fun shortGoWindowMapsToShortLimit() {
        assertEquals(UsageMetricKind.SHORT_WINDOW, goUsageMetricKind("short_window"))
    }

    @Test fun longGoWindowMapsToLongLimit() {
        assertEquals(UsageMetricKind.LONG_WINDOW, goUsageMetricKind("long_window"))
    }
}
