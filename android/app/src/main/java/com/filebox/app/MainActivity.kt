package com.mounvix.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.mounvix.app.ui.MounvixRoot
import com.mounvix.app.ui.theme.MounvixTheme

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MounvixTheme {
                MounvixRoot()
            }
        }
    }
}
