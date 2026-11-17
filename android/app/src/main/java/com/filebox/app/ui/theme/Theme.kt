package com.mounvix.app.ui.theme

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

private val LightColors = lightColorScheme(
    primary = Color(0xFF3B6EF6),
    onPrimary = Color.White,
    primaryContainer = Color(0xFFDDE3FF),
    onPrimaryContainer = Color(0xFF001974),
    secondary = Color(0xFF5A5D66),
    onSecondary = Color.White,
    background = Color(0xFFFAFAFA),
    surface = Color(0xFFFAFAFA),
    onSurface = Color(0xFF1A1B1E),
    onSurfaceVariant = Color(0xFF41464D),
    outline = Color(0xFF71787E)
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFFBCC3FF),
    onPrimary = Color(0xFF002A69),
    primaryContainer = Color(0xFF274CB1),
    onPrimaryContainer = Color(0xFFDDE3FF),
    secondary = Color(0xFFC3C6D0),
    onSecondary = Color(0xFF2F323A),
    background = Color(0xFF121316),
    surface = Color(0xFF121316),
    onSurface = Color(0xFFE3E2E6),
    onSurfaceVariant = Color(0xFFBEC0C6),
    outline = Color(0xFF8E9099)
)

@Composable
fun MounvixTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit
) {
    val scheme = when {
        // Lint NewApi требует проверку версии непосредственно в условии ветки.
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S ->
            if (darkTheme) dynamicDarkColorScheme(LocalContext.current)
            else dynamicLightColorScheme(LocalContext.current)
        darkTheme -> DarkColors
        else -> LightColors
    }
    MaterialTheme(colorScheme = scheme, content = content)
}
