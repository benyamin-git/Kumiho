package gui

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/benyamin-git/kumiho/internal/gui/web"
)

var (
	catalogStateCodes = map[string]bool{
		"IDLE":            true,
		"CONNECTED":       true,
		"PROXY_ONLY":      true,
		"CONNECTING":      true,
		"RECONNECTING":    true,
		"WAITING_NETWORK": true,
		"UNAUTHENTICATED": true,
		"FATAL":           true,
	}
	catalogUIFamilies = map[string]bool{
		"accent":         true,
		"blocked_region": true,
		"cleanup":        true,
		"group":          true,
		"language":       true,
		"relay":          true,
		"rotation":       true,
		"service":        true,
		"theme":          true,
		"update":         true,
		"version":        true,
	}
	noticeCodeLiteralRE = regexp.MustCompile(`"(?:dashboard|locations|settings|account|logs|diagnostics|onboarding)\.[a-z0-9_.]+"`)
)

// TestDynamicCatalogFamilies checks the catalog keys the literal scanner
// deliberately leaves to the controller tables: raw state codes, engine
// setting keys and emitted notice codes. It grows with the screens; dead
// literal keys are rejected by the web catalog test instead.
func TestDynamicCatalogFamilies(t *testing.T) {
	data, err := fs.ReadFile(web.StaticFS(), "i18n/en.json")
	if err != nil {
		t.Fatalf("read en.json: %v", err)
	}
	var catalog map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("parse en.json: %v", err)
	}

	noticeCodes := controllerNoticeCodes(t)
	for key := range catalog {
		switch {
		case strings.HasPrefix(key, "state."):
			code := strings.TrimPrefix(key, "state.")
			if !catalogStateCodes[code] {
				t.Errorf("catalog state key %q is not a raw api.Status state", key)
			}
		case strings.HasPrefix(key, "settings."):
			checkSettingCatalogKey(t, key)
		case strings.HasPrefix(key, "notice."):
			code := strings.TrimPrefix(key, "notice.")
			if !noticeCodes[code] {
				t.Errorf("catalog notice key %q has no emitted notice code", key)
			}
		}
	}
}

func checkSettingCatalogKey(t *testing.T, key string) {
	t.Helper()
	rest := strings.TrimPrefix(key, "settings.")
	first, tail, ok := strings.Cut(rest, ".")
	if !ok || first == "" || tail == "" {
		t.Errorf("catalog setting key %q must be settings.<key>.<field>", key)
		return
	}
	if catalogUIFamilies[first] {
		return
	}
	if _, ok := settingLabels[first]; !ok {
		t.Errorf("catalog setting key %q names unknown engine setting %q", key, first)
		return
	}
	switch {
	case tail == "label", tail == "hint", tail == "unit":
	case strings.HasPrefix(tail, "option."):
	default:
		t.Errorf("catalog setting key %q has unknown field %q", key, tail)
	}
}

// controllerNoticeCodes collects the stable code literals emitted by the
// controller and the Windows host. It reads the source instead of keeping a
// second hand-maintained list that could drift from setNoticeCode calls.
func controllerNoticeCodes(t *testing.T) map[string]bool {
	t.Helper()
	codes := make(map[string]bool)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob .go: %v", err)
	}
	files = append(files, filepath.Join("..", "..", "cmd", "kumiho-gui-web", "main.go"))
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, match := range noticeCodeLiteralRE.FindAllString(string(data), -1) {
			codes[strings.Trim(match, `"`)] = true
		}
	}
	return codes
}
