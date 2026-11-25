package ing.boykiss.aiusagewidgets.widget

import androidx.compose.ui.unit.dp
import org.junit.Assert.assertEquals
import org.junit.Test

class GroupedWindowPlanTest {
    @Test fun smallSquareKeepsEveryFamilyAndWindow() {
        assertEquals(GroupedWindowDetail.FIGURE, planGroupedWindows(2, 100.dp, true))
    }

    @Test fun largeWidgetsAddBarsAndResetTimes() {
        assertEquals(GroupedWindowDetail.RESET, planGroupedWindows(2, 200.dp, false))
        assertEquals(GroupedWindowDetail.BAR, planGroupedWindows(2, 100.dp, false))
    }

    @Test fun onlyVeryShortWidgetsSummarizeOneLimitPerFamily() {
        assertEquals(GroupedWindowDetail.SUMMARY, planGroupedWindows(2, 40.dp, true))
    }
}
