package dev.benyamin.kumiho.ui.guide

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * App-scoped visibility for the reusable Blocked region guide. Setup, Settings
 * and the dashboard's region error notice all open the same screen, at any
 * point in the wizard, so visibility cannot belong to one route's view model.
 */
class GuideController {
    private val visibleState = MutableStateFlow(false)

    /** True while the guide covers the shell. */
    val visible: StateFlow<Boolean> = visibleState.asStateFlow()

    fun open() {
        visibleState.value = true
    }

    fun close() {
        visibleState.value = false
    }
}
