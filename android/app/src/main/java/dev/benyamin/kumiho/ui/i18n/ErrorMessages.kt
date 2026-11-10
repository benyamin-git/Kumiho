package dev.benyamin.kumiho.ui.i18n

import dev.benyamin.kumiho.engine.EngineErrorCodes

/**
 * The one-line, non-technical explanation of an engine error. The screen keeps
 * the raw engine message in a Latin details block; this summary is translated.
 */
enum class ErrorSummary {
    BAD_REQUEST,
    NOT_IMPLEMENTED,
    NOT_AUTHENTICATED,
    FORBIDDEN,
    UNSUPPORTED_SETTING,
    INTERNAL,
    PROTOCOL,
    ENGINE,
    CLOSED,
    GENERIC,
}

/** Maps the stable wire code to a translatable summary; unknown codes fall back. */
object ErrorMessages {
    fun summary(code: String): ErrorSummary = when (code) {
        EngineErrorCodes.BAD_REQUEST -> ErrorSummary.BAD_REQUEST
        EngineErrorCodes.NOT_IMPLEMENTED -> ErrorSummary.NOT_IMPLEMENTED
        EngineErrorCodes.NOT_AUTHENTICATED -> ErrorSummary.NOT_AUTHENTICATED
        EngineErrorCodes.FORBIDDEN -> ErrorSummary.FORBIDDEN
        EngineErrorCodes.UNSUPPORTED_SETTING -> ErrorSummary.UNSUPPORTED_SETTING
        EngineErrorCodes.INTERNAL -> ErrorSummary.INTERNAL
        EngineErrorCodes.PROTOCOL -> ErrorSummary.PROTOCOL
        EngineErrorCodes.ENGINE -> ErrorSummary.ENGINE
        EngineErrorCodes.CLOSED -> ErrorSummary.CLOSED
        else -> ErrorSummary.GENERIC
    }
}
