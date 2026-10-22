package dev.benyamin.kumiho

import android.app.Application
import dev.benyamin.kumiho.crash.CrashHandler

/**
 * Process-wide entry point. It installs the local crash handler first, then
 * builds the [AppContainer] and starts the app-scoped work (the theme load) so
 * the shell can render before first paint.
 */
class KumihoApplication : Application() {
    /** The application-scoped container. */
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        CrashHandler.install(this)
        container = AppContainer(this)
        container.start()
    }
}
