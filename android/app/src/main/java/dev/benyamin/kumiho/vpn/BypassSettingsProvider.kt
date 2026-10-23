package dev.benyamin.kumiho.vpn

/**
 * The allow-bypass seam. The container wires the DataStore-backed
 * `AllowBypassRepository`; the bridge reads a snapshot at `acquireFD` time and
 * records the value only after the interface is established.
 */
interface BypassSettingsProvider {
    /** The cached value, synchronously; Off until the first load. */
    fun snapshot(): Boolean

    /** Commits the value used by the interface that was just established. */
    fun markApplied()
}
