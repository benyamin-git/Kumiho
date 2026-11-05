package dev.benyamin.kumiho.ui.dashboard

/**
 * The one successful-connect moment: the gauge pulses and the phone ticks once.
 * A reconnect wait that lands on CONNECTED counts; a fresh app start that is
 * already connected does not.
 */
object ConnectPulse {
    fun shouldPulse(previous: DashboardPhase, current: DashboardPhase): Boolean =
        current == DashboardPhase.CONNECTED &&
            (
                previous == DashboardPhase.CONNECTING ||
                    previous == DashboardPhase.RECONNECTING ||
                    previous == DashboardPhase.WAITING
                )
}
