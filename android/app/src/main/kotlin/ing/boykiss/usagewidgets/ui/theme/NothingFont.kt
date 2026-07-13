package ing.boykiss.usagewidgets.ui.theme

import java.io.File

object NothingFont {
    private val knownSystemFiles = listOf(
        "/system/fonts/NDot55Caps.otf",
        "/system/fonts/NDot57Caps.otf",
        "/system/fonts/Ndot-55.otf",
    )

    fun isAvailable(): Boolean = knownSystemFiles.any { File(it).canRead() }
}
