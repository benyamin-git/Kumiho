package dev.benyamin.kumiho.engine

import dev.benyamin.kumiho.go.mobile.DeviceBridge
import dev.benyamin.kumiho.go.mobile.Listener
import dev.benyamin.kumiho.go.mobile.Mobile

/**
 * The production [EngineTransport]: a direct pass-through to the generated
 * gomobile bindings. gomobile appends the Go package name to `-javapkg`, so the
 * classes live in `dev.benyamin.kumiho.go.mobile`.
 *
 * Every method may throw a `java.lang.Exception` from Go; [EngineClient]
 * translates those into [EngineException]s.
 */
class GomobileEngineTransport : EngineTransport {
    override fun start(pathsJson: String, bridge: DeviceBridge, listener: Listener) {
        Mobile.start(pathsJson, bridge, listener)
    }

    override fun command(requestJson: String): String = Mobile.command(requestJson)

    override fun close() {
        Mobile.close()
    }
}
