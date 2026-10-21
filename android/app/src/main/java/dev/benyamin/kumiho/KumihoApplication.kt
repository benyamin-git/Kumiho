package dev.benyamin.kumiho

import android.app.Application

/**
 * Process-wide entry point. It builds the [AppContainer] and starts the
 * app-scoped work (the theme load) so the shell can render before first paint.
 * The crash handler is installed in T15.
 */
class KumihoApplication : Application() {
    /** The application-scoped container. */
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        container.start()
    }
}
