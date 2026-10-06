package ing.boykiss.aiusagewidgets.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

class ModelsTest {
    @Test fun remainingPercentageIsCalculatedAndClamped() {
        assertEquals(100.0, 0.0.remainingPercent()!!, 0.0)
        assertEquals(2.0, 98.0.remainingPercent()!!, 0.0)
        assertEquals(0.0, 140.0.remainingPercent()!!, 0.0)
        assertEquals(100.0, (-10.0).remainingPercent()!!, 0.0)
    }

    @Test fun severityMatchesTuiThresholdsAndMarksCachedDataAsWarning() {
        assertEquals(UsageSeverity.GOOD, usageSeverity(49.0))
        assertEquals(UsageSeverity.WARNING, usageSeverity(50.0))
        assertEquals(UsageSeverity.BAD, usageSeverity(65.0))
        assertEquals(UsageSeverity.WARNING, usageSeverity(10.0, cached = true))
    }

    @Test fun missingPercentageRemainsMissing() {
        assertNull((null as Double?).remainingPercent())
    }

    @Test fun accountDisplayNameIsTrimmed() {
        assertEquals("Work", normalizedAccountDisplayName("  Work  "))
    }

    @Test fun accountDisplayNameCannotBeBlankOrTooLong() {
        assertThrows(IllegalArgumentException::class.java) { normalizedAccountDisplayName("   ") }
        assertThrows(IllegalArgumentException::class.java) {
            normalizedAccountDisplayName("a".repeat(MAX_ACCOUNT_DISPLAY_NAME_LENGTH + 1))
        }
    }

    @Test fun legacyWidgetStyleNamesMapToRenamedStyles() {
        assertEquals(WidgetVisualStyle.TONAL, WidgetVisualStyle.fromStoredName("MATERIAL_YOU"))
        assertEquals(WidgetVisualStyle.FROSTED, WidgetVisualStyle.fromStoredName("GLASS"))
        assertEquals(WidgetVisualStyle.DOT_MATRIX, WidgetVisualStyle.fromStoredName("NOTHING"))
    }

    @Test fun currentAndUnknownWidgetStyleNamesResolve() {
        WidgetVisualStyle.entries.forEach { assertEquals(it, WidgetVisualStyle.fromStoredName(it.name)) }
        assertEquals(WidgetVisualStyle.Default, WidgetVisualStyle.fromStoredName("NOT_A_STYLE"))
        assertEquals(WidgetVisualStyle.Default, WidgetVisualStyle.fromStoredName(null))
    }
}
