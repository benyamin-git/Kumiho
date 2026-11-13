package dev.benyamin.kumiho.update

/** The two small facts the update manager persists across launches. */
interface UpdatePreferences {
    suspend fun lastUpdateCheck(): Long

    suspend fun setLastUpdateCheck(value: Long)

    /** Empty means the user has not dismissed a version. */
    suspend fun dismissedUpdateVersion(): String

    suspend fun setDismissedUpdateVersion(value: String)
}
