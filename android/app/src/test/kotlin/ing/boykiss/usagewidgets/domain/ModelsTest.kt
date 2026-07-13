package ing.boykiss.usagewidgets.domain

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
}
