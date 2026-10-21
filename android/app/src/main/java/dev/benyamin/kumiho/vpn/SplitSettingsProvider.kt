package dev.benyamin.kumiho.vpn

/**
 * The split-tunnel seam. T9 always runs Off; T14 replaces the container's
 * provider with one fed by the settings screen. The bridge reads a snapshot at
 * `acquireFD` time so a selection can never change mid-build.
 */
interface SplitSettingsProvider {
    fun snapshot(): SplitSettings
}

/** The default: no split tunnel; only Kumiho itself stays outside the VPN. */
object OffSplitSettingsProvider : SplitSettingsProvider {
    override fun snapshot(): SplitSettings = SplitSettings()
}
