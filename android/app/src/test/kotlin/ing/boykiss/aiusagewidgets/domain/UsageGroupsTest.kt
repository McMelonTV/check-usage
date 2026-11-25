package ing.boykiss.aiusagewidgets.domain

import org.junit.Assert.*
import org.junit.Test

class UsageGroupsTest {
    private fun window(kind: UsageMetricKind, scope: String, used: Double?) =
        UsageWindow(kind, "$scope $kind", used, used.remainingPercent(), null, null, scope)

    @Test fun fourLimitsStayGroupedAndExhaustionDoesNotCrossFamilies() {
        val groups = groupedUsage(ProviderId("antigravity"), listOf(
            window(UsageMetricKind.SHORT_WINDOW, "Gemini", 38.0),
            window(UsageMetricKind.LONG_WINDOW, "Gemini", 100.0),
            window(UsageMetricKind.SHORT_WINDOW, "Claude/GPT", null),
            window(UsageMetricKind.LONG_WINDOW, "Claude/GPT", 52.0),
        ))
        assertEquals(listOf("Gemini", "Claude/GPT"), groups.map { it.name })
        assertEquals(listOf(UsageMetricKind.SHORT_WINDOW, UsageMetricKind.LONG_WINDOW), groups.windowKinds())
        assertEquals(4, groups.sumOf { it.windows.size })
        assertTrue(groups[0].isBlocked(groups[0].windows[0]))
        assertFalse(groups[1].isBlocked(groups[1].windows[0]))
        assertNull(groups[1].windows[0].remainingPercent)
    }

    @Test fun unknownShortAllowanceIsNotMarkedBlocked() {
        val group = UsageGroup("Gemini", listOf(window(UsageMetricKind.SHORT_WINDOW, "Gemini", null),
            window(UsageMetricKind.LONG_WINDOW, "Gemini", 100.0)))
        assertFalse(group.isBlocked(group.windows.first()))
    }

    @Test fun oldCursorCacheUpgradesBothIndependentPoolsToMonthly() {
        val groups = groupedUsage(ProviderId("cursor"), listOf(
            UsageWindow(UsageMetricKind.LONG_WINDOW, "Cursor models", 0.0, 100.0, null, null),
            UsageWindow(UsageMetricKind.MONTHLY_WINDOW, "Other models", 100.0, 0.0, null, null),
        ))
        assertEquals(listOf(UsageMetricKind.MONTHLY_WINDOW), groups.windowKinds())
        assertEquals(listOf("Cursor models", "Other models"), groups.map { it.name })
        assertFalse(groups.any { group -> group.windows.any(group::isBlocked) })
        assertEquals(100.0, groups.first().windows.first().remainingPercent!!, 0.0)
    }
}
