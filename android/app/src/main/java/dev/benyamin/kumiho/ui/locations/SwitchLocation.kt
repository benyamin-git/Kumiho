package dev.benyamin.kumiho.ui.locations

import dev.benyamin.kumiho.engine.EngineStatus
import dev.benyamin.kumiho.vpn.EngineStates
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeoutOrNull

/** One step of the switch choreography, as the dialog shows it. */
sealed interface SwitchStep {
    data object Idle : SwitchStep

    data object Disconnecting : SwitchStep

    data class Connecting(val target: String) : SwitchStep

    data class Failed(val message: String) : SwitchStep
}

/** The row a confirmed switch will persist and connect to. */
data class SwitchTarget(
    val countryCode: String,
    val cityCode: String?,
    val server: String,
    val label: String,
)

private const val TIMEOUT_MESSAGE = "The session did not stop in time."

/** The engine did not settle out of the connected family before the deadline. */
class SwitchTimeoutException : Exception(TIMEOUT_MESSAGE)

/**
 * The one choreography for changing location while a session is up: end the
 * session, wait until the engine reports it is no longer in the connected
 * family, then persist the new row and connect again. It never retries on its
 * own; the dialog's Retry calls [run] again.
 */
class SwitchLocation(
    private val gateway: LocationsGateway,
    private val timeoutMillis: Long = 15_000,
    private val pollMillis: Long = 250,
) {
    suspend fun run(target: SwitchTarget, onStep: (SwitchStep) -> Unit): Result<Unit> {
        onStep(SwitchStep.Disconnecting)
        try {
            gateway.disconnect()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            return Result.failure(error)
        }

        val settled = try {
            withTimeoutOrNull(timeoutMillis) {
                while (EngineStates.isActive(gateway.status().state)) {
                    delay(pollMillis)
                }
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            return Result.failure(error)
        }
        if (settled == null) return Result.failure(SwitchTimeoutException())

        try {
            gateway.selectLocation(target.countryCode, target.cityCode, target.server)
            onStep(SwitchStep.Connecting(target.label))
            gateway.connect()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            return Result.failure(error)
        }
        return Result.success(Unit)
    }

}

/** True between the confirm tap and the last step; the dialog locks then. */
val SwitchStep.isRunning: Boolean
    get() = this is SwitchStep.Disconnecting || this is SwitchStep.Connecting
