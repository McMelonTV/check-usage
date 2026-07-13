package ing.boykiss.aiusagewidgets.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

private val Light = lightColorScheme(
    primary = Color(0xFF355F8A),
    secondary = Color(0xFF526070),
    tertiary = Color(0xFF705575),
    surface = Color(0xFFF8F9FF),
)
private val Dark = darkColorScheme(
    primary = Color(0xFFA4C9F7),
    secondary = Color(0xFFBAC8DA),
    tertiary = Color(0xFFDDBBE1),
    surface = Color(0xFF101419),
)

@Composable
fun UsageWidgetsTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val context = LocalContext.current
    val colors = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && dark -> dynamicDarkColorScheme(context)
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> dynamicLightColorScheme(context)
        dark -> Dark
        else -> Light
    }
    MaterialTheme(colorScheme = colors, content = content)
}
