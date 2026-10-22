package dev.benyamin.kumiho.engine

/**
 * Error codes the engine uses. The first six mirror `internal/api`; the last
 * three describe Kotlin-side failures that never cross the JSON protocol.
 */
object EngineErrorCodes {
    const val BAD_REQUEST = "bad_request"
    const val NOT_IMPLEMENTED = "not_implemented"
    const val NOT_AUTHENTICATED = "not_authenticated"
    const val FORBIDDEN = "forbidden"
    const val UNSUPPORTED_SETTING = "unsupported_setting"
    const val INTERNAL = "internal"

    /** A malformed response envelope, event or JSON document. */
    const val PROTOCOL = "protocol"

    /** The gomobile facade itself failed (not started, panic, bad request). */
    const val ENGINE = "engine"

    /** The client was closed before the request was issued. */
    const val CLOSED = "closed"
}

/**
 * A domain failure reported by the engine. The engine encodes these as
 * `{"error":{"code":…,"message":…}}` envelopes so the UI can keep the code and
 * react to it; a Go facade failure is mapped to [EngineErrorCodes.ENGINE].
 */
open class EngineException(
    val code: String,
    message: String,
    cause: Throwable? = null,
) : Exception(message, cause)

/**
 * The engine sent something the contract does not allow: invalid JSON, an
 * envelope without a type, an `error` that is not an object, or a payload that
 * is not an object. This is a client bug or a protocol-version mismatch, never
 * a user-facing engine answer, so it carries [EngineErrorCodes.PROTOCOL].
 */
class EngineProtocolException(
    message: String,
    cause: Throwable? = null,
) : EngineException(EngineErrorCodes.PROTOCOL, message, cause)
