package dev.benyamin.kumiho.vpn

/**
 * The restart/Always-on decision, kept pure so it can be tested without an
 * Android runtime.
 *
 * Rules (D51): auto-connect only when Always-on is enabled, the account is
 * signed in, and the engine is in a settled non-connected state. Lockdown is
 * the OS's business: it is displayed, never fought, and a signed-out restart
 * always offers sign-in instead of a doomed connect.
 */
object AlwaysOnPolicy {
    fun decide(
        alwaysOn: Boolean,
        authenticated: Boolean,
        state: String?,
        freshStart: Boolean,
    ): AlwaysOnDecision {
        if (!alwaysOn) return AlwaysOnDecision.NONE
        if (!authenticated) return AlwaysOnDecision.SIGN_IN_NOTIFICATION
        return when (state) {
            // A settled state: nothing is up, so a fresh start may connect.
            EngineStates.IDLE,
            EngineStates.WAITING_NETWORK,
            -> AlwaysOnDecision.AUTO_CONNECT
            // A fatal previous attempt is retried only once per fresh start;
            // a later restart loop would otherwise hammer the edge.
            EngineStates.FATAL -> if (freshStart) {
                AlwaysOnDecision.AUTO_CONNECT
            } else {
                AlwaysOnDecision.NONE
            }
            // CONNECTED, CONNECTING, RECONNECTING, PROXY_ONLY and anything
            // unknown: the tunnel (or a connect) is already in flight.
            else -> AlwaysOnDecision.NONE
        }
    }
}
