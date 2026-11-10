//go:build windows

package gui

import "testing"

func TestTrayCatalogParity(t *testing.T) {
	en := trayStrings["en"]
	fa := trayStrings["fa"]
	if len(en) == 0 || len(fa) == 0 {
		t.Fatal("tray catalogs are empty")
	}
	for key, value := range en {
		if value == "" {
			t.Errorf("en %q is empty", key)
		}
		if fa[key] == "" {
			t.Errorf("fa is missing %q", key)
		}
	}
	for key, value := range fa {
		if value == "" {
			t.Errorf("fa %q is empty", key)
		}
		if _, ok := en[key]; !ok {
			t.Errorf("fa has extra key %q", key)
		}
	}
	for _, code := range []string{
		"IDLE", "CONNECTED", "PROXY_ONLY", "CONNECTING", "RECONNECTING",
		"WAITING_NETWORK", "UNAUTHENTICATED", "FATAL",
	} {
		if !trayHas("state." + code) {
			t.Errorf("state.%s is missing", code)
		}
	}
}

func TestTrayTipTextUsesLanguage(t *testing.T) {
	tray := NewTray(TrayProviders{
		Language: func() string { return "fa" },
		Tooltip:  func() string { return "متصل" },
	}, nil)
	if got := tray.tipText(); got != "Kumiho — متصل" {
		t.Errorf("tipText() = %q", got)
	}
	tray.p.Tooltip = nil
	if got := tray.tipText(); got != "Kumiho — در حال بارگذاری..." {
		t.Errorf("empty tooltip = %q", got)
	}
}

func TestTrayText(t *testing.T) {
	if got := trayText("fa", "connect"); got != "اتصال" {
		t.Errorf("trayText(fa, connect) = %q", got)
	}
	if got := trayText("de", "connect"); got != "Connect" {
		t.Errorf("unknown language = %q, want English", got)
	}
	if got := trayText("en", "missing"); got != "missing" {
		t.Errorf("missing key = %q, want the key", got)
	}
	if got := trayLanguage("fa-IR"); got != "en" {
		t.Errorf("trayLanguage(fa-IR) = %q, want en", got)
	}
}
