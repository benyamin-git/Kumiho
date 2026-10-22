package dev.benyamin.kumiho.engine

import java.io.File
import org.json.JSONObject

/**
 * The app-private layout the engine opens. The Go side requires exactly five
 * absolute paths: `config`, `state` and `tokens` are **files** (their parent
 * directories are created 0700), `runtime` and `logs` are **directories**.
 *
 * [forAppDirs] owns the filenames so the same layout is used everywhere:
 *
 * | Key | Location | Kind |
 * |---|---|---|
 * | `config` | `filesDir/config.toml` | file |
 * | `state` | `filesDir/state.json` | file |
 * | `tokens` | `noBackupFilesDir/tokens.json` | file |
 * | `runtime` | `cacheDir/runtime` | directory |
 * | `logs` | `filesDir/logs` | directory |
 *
 * Paths are pure `java.io.File`s: this class has no Android dependency, and T9
 * passes the real application directories. A relative root is rejected here
 * rather than silently resolved against the process working directory.
 */
class EnginePaths private constructor(
    val config: File,
    val state: File,
    val tokens: File,
    val runtime: File,
    val logs: File,
) {
    init {
        val entries = listOf(
            "config" to config,
            "state" to state,
            "tokens" to tokens,
            "runtime" to runtime,
            "logs" to logs,
        )
        for ((name, path) in entries) {
            require(path.isAbsolute) { "engine paths.$name must be absolute, got ${path.path}" }
        }
    }

    /** The exact five-key JSON object the engine decodes. */
    fun toJson(): JSONObject = JSONObject()
        .put(KEY_CONFIG, config.path)
        .put(KEY_STATE, state.path)
        .put(KEY_TOKENS, tokens.path)
        .put(KEY_RUNTIME, runtime.path)
        .put(KEY_LOGS, logs.path)

    /** The exact five-key JSON text passed to `Mobile.start`. */
    fun toJsonString(): String = toJson().toString()

    companion object {
        const val KEY_CONFIG = "config"
        const val KEY_STATE = "state"
        const val KEY_TOKENS = "tokens"
        const val KEY_RUNTIME = "runtime"
        const val KEY_LOGS = "logs"

        /** The config filename; seeded with `mtu = 1500` by the engine. */
        const val CONFIG_FILE = "config.toml"

        /** The engine state filename. */
        const val STATE_FILE = "state.json"

        /** The token-cache filename, kept out of Android backups. */
        const val TOKENS_FILE = "tokens.json"

        /** The runtime directory; sockets and ephemeral files live here. */
        const val RUNTIME_DIR = "runtime"

        /** The log directory; the rotated `kumiho.log` lives here. */
        const val LOGS_DIR = "logs"

        /** Builds the standard layout from the three app-private roots. */
        fun forAppDirs(filesDir: File, noBackupFilesDir: File, cacheDir: File): EnginePaths {
            require(filesDir.isAbsolute) { "filesDir must be absolute, got ${filesDir.path}" }
            require(noBackupFilesDir.isAbsolute) {
                "noBackupFilesDir must be absolute, got ${noBackupFilesDir.path}"
            }
            require(cacheDir.isAbsolute) { "cacheDir must be absolute, got ${cacheDir.path}" }
            return EnginePaths(
                config = File(filesDir, CONFIG_FILE),
                state = File(filesDir, STATE_FILE),
                tokens = File(noBackupFilesDir, TOKENS_FILE),
                runtime = File(cacheDir, RUNTIME_DIR),
                logs = File(filesDir, LOGS_DIR),
            )
        }

        /** Builds the layout from five explicit absolute paths. */
        fun of(
            config: File,
            state: File,
            tokens: File,
            runtime: File,
            logs: File,
        ): EnginePaths = EnginePaths(config, state, tokens, runtime, logs)
    }
}
