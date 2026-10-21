package dev.benyamin.kumiho.engine

import dev.benyamin.kumiho.go.mobile.DeviceBridge
import dev.benyamin.kumiho.go.mobile.Listener

/**
 * The narrow seam over the gomobile facade. JVM tests implement a fake; the
 * only production implementation is [GomobileEngineTransport].
 */
interface EngineTransport {
    /** Opens the engine; see `mobile.Start`. */
    fun start(pathsJson: String, bridge: DeviceBridge, listener: Listener)

    /** Dispatches one request envelope and returns one response envelope. */
    fun command(requestJson: String): String

    /** Shuts the engine down; idempotent on the Go side. */
    fun close()
}
