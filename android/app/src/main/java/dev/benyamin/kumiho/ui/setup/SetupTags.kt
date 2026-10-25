package dev.benyamin.kumiho.ui.setup

/**
 * Stable test tags for the setup flow. The instrumented tests and the
 * screenshot capture both address nodes through these, so a copy change never
 * breaks a test and a test never depends on a visible sentence.
 */
object SetupTags {
    const val STEP_INDICATOR = "setup_step_indicator"
    const val TOP_BAR = "setup_top_bar"
    const val BACK = "setup_back"
    const val CONTINUE = "setup_continue"
    const val FINISH = "setup_finish"
    const val CONNECT_NOW = "setup_connect_now"
    const val ERROR = "setup_error"

    const val CONSENT_ACCEPT = "setup_consent_accept"
    const val CONSENT_STEP = "setup_step_consent"

    const val SIGN_IN_STEP = "setup_step_sign_in"
    const val SIGN_IN_EMAIL = "setup_sign_in_email"
    const val SIGN_IN_EMAIL_SUBMIT = "setup_sign_in_email_submit"
    const val SIGN_IN_PASSWORD = "setup_sign_in_password"
    const val SIGN_IN_PASSWORD_SUBMIT = "setup_sign_in_password_submit"
    const val SIGN_IN_CODE = "setup_sign_in_code"
    const val SIGN_IN_CODE_SUBMIT = "setup_sign_in_code_submit"
    const val SIGN_IN_RECHECK = "setup_sign_in_recheck"
    const val SIGN_IN_ERROR = "setup_sign_in_error"
    const val SIGN_IN_WAITING = "setup_sign_in_waiting"
    const val SIGN_IN_BROWSER = "setup_sign_in_browser"
    const val SIGN_IN_BROWSER_WAITING = dev.benyamin.kumiho.ui.account.BrowserWaitingTags.CARD
    const val SIGN_IN_BROWSER_OPEN = dev.benyamin.kumiho.ui.account.BrowserWaitingTags.OPEN
    const val SIGN_IN_BROWSER_PASTE_TOGGLE =
        dev.benyamin.kumiho.ui.account.BrowserWaitingTags.PASTE_TOGGLE
    const val SIGN_IN_BROWSER_PASTE_FIELD =
        dev.benyamin.kumiho.ui.account.BrowserWaitingTags.PASTE_FIELD
    const val SIGN_IN_BROWSER_PASTE_SUBMIT =
        dev.benyamin.kumiho.ui.account.BrowserWaitingTags.PASTE_SUBMIT
    const val SIGN_IN_BROWSER_USE_PASSWORD =
        dev.benyamin.kumiho.ui.account.BrowserWaitingTags.USE_PASSWORD
    const val SIGN_IN_BROWSER_CANCEL = dev.benyamin.kumiho.ui.account.BrowserWaitingTags.CANCEL
    const val SIGN_IN_ACCOUNT = "setup_sign_in_account"
    const val SIGN_IN_NOTICE = "setup_sign_in_notice"

    const val PERMISSIONS_STEP = "setup_step_permissions"
    const val PERMISSIONS_VPN_ACTION = "setup_permissions_vpn_action"
    const val PERMISSIONS_VPN_STATUS = "setup_permissions_vpn_status"
    const val PERMISSIONS_NOTIFICATIONS_ACTION = "setup_permissions_notifications_action"
    const val PERMISSIONS_NOTIFICATIONS_SETTINGS = "setup_permissions_notifications_settings"
    const val PERMISSIONS_NOTIFICATIONS_STATUS = "setup_permissions_notifications_status"

    const val ALWAYS_ON_STEP = "setup_step_always_on"
    const val ALWAYS_ON_STATUS = "setup_always_on_status"
    const val LOCKDOWN_STATUS = "setup_lockdown_status"
    const val ALWAYS_ON_SETTINGS = "setup_always_on_settings"
    const val ALWAYS_ON_HINT = "setup_always_on_connect_hint"
}
