package dev.benyamin.kumiho.vpn

/**
 * The split-tunnel seam. The container wires the DataStore-backed
 * `SplitTunnelRepository`; the bridge reads a snapshot at `acquireFD` time so
 * a selection can never change mid-build.
 */
interface SplitSettingsProvider {
    fun snapshot(): SplitSettings
}
