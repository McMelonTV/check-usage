package ing.boykiss.aiusagewidgets.ui.dashboard

import org.junit.Assert.assertEquals
import org.junit.Test

class CountdownTest {
    @Test fun countdownShowsMinutesBeforeResetAndRefreshNeededAfterward() {
        val now = 1_000_000L
        assertEquals("Resets in 5m", resetText(1300, now))
        assertEquals("Resets in 1m", resetText(1001, now))
        assertEquals("Refresh needed", resetText(999, now))
        assertEquals("Reset time unknown", resetText(null, now))
    }
}
