//go:build windows

package gui

// trayStrings is the tray's small Go catalog. It is separate from the web
// catalog because the tray renders Win32 menu items and notifications on the
// shell thread, outside the WebView; the keys and wording stay aligned with
// the JS catalog's state and action names.
var trayStrings = map[string]map[string]string{
	"en": {
		"connect":               "Connect",
		"disconnect":            "Disconnect",
		"location":              "Location",
		"no_location":           "Location: (none)",
		"show":                  "Show Kumiho",
		"diagnostics":           "Diagnostics",
		"quit":                  "Quit",
		"hint_title":            "Kumiho",
		"hint_text":             "Kumiho is still running. Right-click the tray icon to connect or quit.",
		"loading":               "Loading...",
		"unreachable":           "Daemon unreachable",
		"unavailable":           "Status unavailable",
		"state.IDLE":            "Off",
		"state.CONNECTED":       "Connected",
		"state.PROXY_ONLY":      "Proxy only",
		"state.CONNECTING":      "Connecting",
		"state.RECONNECTING":    "Reconnecting",
		"state.WAITING_NETWORK": "Waiting for network",
		"state.UNAUTHENTICATED": "Not signed in",
		"state.FATAL":           "Error",
	},
	"fa": {
		"connect":               "اتصال",
		"disconnect":            "قطع اتصال",
		"location":              "مکان",
		"no_location":           "مکان: (هیچ)",
		"show":                  "نمایش Kumiho",
		"diagnostics":           "عیب‌یابی",
		"quit":                  "خروج",
		"hint_title":            "Kumiho",
		"hint_text":             "Kumiho هنوز در حال اجراست. برای اتصال یا خروج روی نماد سینی کلیک راست کنید.",
		"loading":               "در حال بارگذاری...",
		"unreachable":           "دیمن در دسترس نیست",
		"unavailable":           "وضعیت در دسترس نیست",
		"state.IDLE":            "خاموش",
		"state.CONNECTED":       "متصل",
		"state.PROXY_ONLY":      "فقط پروکسی",
		"state.CONNECTING":      "در حال اتصال",
		"state.RECONNECTING":    "در حال اتصال دوباره",
		"state.WAITING_NETWORK": "در انتظار شبکه",
		"state.UNAUTHENTICATED": "وارد نشده‌اید",
		"state.FATAL":           "خطا",
	},
}

// trayLanguage normalizes a language tag to the two tray catalogs.
func trayLanguage(language string) string {
	if language == "fa" {
		return "fa"
	}
	return "en"
}

// trayHas reports whether either catalog defines key.
func trayHas(key string) bool {
	_, en := trayStrings["en"][key]
	_, fa := trayStrings["fa"][key]
	return en || fa
}

// trayText returns the localized tray string, falling back to English and
// finally to the key itself so a missing entry is visible but never panics.
func trayText(language, key string) string {
	language = trayLanguage(language)
	if value, ok := trayStrings[language][key]; ok {
		return value
	}
	if value, ok := trayStrings["en"][key]; ok {
		return value
	}
	return key
}
