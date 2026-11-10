package dev.benyamin.kumiho.ui.i18n

import dev.benyamin.kumiho.engine.EngineErrorCodes
import org.junit.Assert.assertEquals
import org.junit.Test

/** Every wire code gets a stable summary; unknown codes never leak raw text. */
class ErrorMessagesTest {

    @Test
    fun `every known engine code maps to its summary`() {
        val expected = mapOf(
            EngineErrorCodes.BAD_REQUEST to ErrorSummary.BAD_REQUEST,
            EngineErrorCodes.NOT_IMPLEMENTED to ErrorSummary.NOT_IMPLEMENTED,
            EngineErrorCodes.NOT_AUTHENTICATED to ErrorSummary.NOT_AUTHENTICATED,
            EngineErrorCodes.FORBIDDEN to ErrorSummary.FORBIDDEN,
            EngineErrorCodes.UNSUPPORTED_SETTING to ErrorSummary.UNSUPPORTED_SETTING,
            EngineErrorCodes.INTERNAL to ErrorSummary.INTERNAL,
            EngineErrorCodes.PROTOCOL to ErrorSummary.PROTOCOL,
            EngineErrorCodes.ENGINE to ErrorSummary.ENGINE,
            EngineErrorCodes.CLOSED to ErrorSummary.CLOSED,
        )
        expected.forEach { (code, summary) ->
            assertEquals(code, summary, ErrorMessages.summary(code))
        }
    }

    @Test
    fun `an unknown code is generic`() {
        assertEquals(ErrorSummary.GENERIC, ErrorMessages.summary("not_a_code"))
        assertEquals(ErrorSummary.GENERIC, ErrorMessages.summary(""))
    }
}
