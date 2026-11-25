package ing.boykiss.aiusagewidgets.domain

data class UsageGroup(val name: String, val windows: List<UsageWindow>) {
    fun window(kind: UsageMetricKind): UsageWindow? = windows.firstOrNull { it.kind == kind }

    fun isBlocked(window: UsageWindow): Boolean = window.usedPercent != null &&
        window.kind == UsageMetricKind.SHORT_WINDOW &&
        windows.any { it.kind == UsageMetricKind.LONG_WINDOW && (it.usedPercent ?: 0.0) >= 100.0 }
}

/** Independent families/pools with one account identity. Also upgrades saved pre-scope snapshots. */
fun groupedUsage(provider: ProviderId, windows: List<UsageWindow>): List<UsageGroup> {
    if (provider.value !in setOf("antigravity", "cursor")) return emptyList()
    val normalized = windows.map { window ->
        val scope = window.scope.ifBlank {
            when (provider.value) {
                "cursor" -> window.label.takeIf { it.equals("Cursor models", true) || it.equals("Other models", true) }.orEmpty()
                else -> listOf("Gemini", "Claude/GPT").firstOrNull { window.label.startsWith("$it ", true) }.orEmpty()
            }
        }
        window.copy(scope = scope, kind = if (provider.value == "cursor" && scope.isNotBlank()) UsageMetricKind.MONTHLY_WINDOW else window.kind)
    }
    if (normalized.isEmpty() || normalized.any { it.scope.isBlank() }) return emptyList()
    return normalized.groupBy { it.scope }.map { (name, values) -> UsageGroup(name, values) }
}

fun List<UsageGroup>.windowKinds(): List<UsageMetricKind> =
    listOf(UsageMetricKind.SHORT_WINDOW, UsageMetricKind.LONG_WINDOW, UsageMetricKind.MONTHLY_WINDOW)
        .filter { kind -> any { group -> group.window(kind) != null } }
