package web

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	i18nLiteralRE   = regexp.MustCompile(`\bt\(\s*"([^"]+)"`)
	i18nPluralRE    = regexp.MustCompile(`\btp\(\s*"([^"]+)"`)
	i18nKeyStringRE = regexp.MustCompile(`"([a-z][a-z0-9]*(?:\.[a-z0-9_]+)+)"`)
	dataI18nRE      = regexp.MustCompile(`data-i18n(?:-aria-label|-placeholder|-title)?="([^"]+)"`)
	placeholderRE   = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)
	dynamicPrefix   = []string{"state.", "settings.", "notice."}
	catalogLocales  = []string{"en", "fa"}
)

func readCatalog(t *testing.T, lang string) map[string]string {
	t.Helper()
	data, err := fs.ReadFile(StaticFS(), "i18n/"+lang+".json")
	if err != nil {
		t.Fatalf("read i18n/%s.json: %v", lang, err)
	}
	var catalog map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("parse i18n/%s.json: %v", lang, err)
	}
	return catalog
}

func TestCatalogsHaveMatchingKeysAndPlaceholders(t *testing.T) {
	en := readCatalog(t, "en")
	fa := readCatalog(t, "fa")

	enKeys := make([]string, 0, len(en))
	for key := range en {
		enKeys = append(enKeys, key)
	}
	sort.Strings(enKeys)
	for _, key := range enKeys {
		if strings.TrimSpace(en[key]) == "" {
			t.Errorf("en %q is empty", key)
		}
		value, ok := fa[key]
		if !ok {
			t.Errorf("fa is missing key %q", key)
			continue
		}
		if strings.TrimSpace(value) == "" {
			t.Errorf("fa %q is empty", key)
		}
		enParams := placeholderSet(en[key])
		faParams := placeholderSet(value)
		if !sameSet(enParams, faParams) {
			t.Errorf("%q placeholders: en %v, fa %v", key, enParams, faParams)
		}
	}
	for key := range fa {
		if _, ok := en[key]; !ok {
			t.Errorf("fa has extra key %q", key)
		}
	}
}

func placeholderSet(value string) []string {
	seen := make(map[string]struct{})
	for _, match := range placeholderRE.FindAllStringSubmatch(value, -1) {
		seen[match[1]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCatalogKeysAreReferencedOrDynamic(t *testing.T) {
	en := readCatalog(t, "en")
	used := make(map[string]struct{})

	err := fs.WalkDir(StaticFS(), "js", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(name, ".js") {
			return nil
		}
		data, err := fs.ReadFile(StaticFS(), name)
		if err != nil {
			return err
		}
		collectLiteralKeys(string(data), used)
		// Keys stored in arrays/maps (the step and consent tables) are
		// referenced too; only exact catalog keys count, so a stray string
		// can never invent a key.
		collectCatalogMentions(string(data), en, used)
		return nil
	})
	if err != nil {
		t.Fatalf("walk js: %v", err)
	}
	index, err := fs.ReadFile(StaticFS(), "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	collectLiteralKeys(string(index), used)
	collectCatalogMentions(string(index), en, used)

	for key := range used {
		if _, ok := en[key]; !ok {
			t.Errorf("referenced key %q is missing from en.json", key)
		}
	}
	for key := range en {
		if _, ok := used[key]; ok {
			continue
		}
		if hasDynamicPrefix(key) {
			continue
		}
		t.Errorf("catalog key %q is neither referenced nor a declared dynamic family", key)
	}
}

func collectLiteralKeys(source string, used map[string]struct{}) {
	for _, re := range []*regexp.Regexp{i18nLiteralRE, dataI18nRE} {
		for _, match := range re.FindAllStringSubmatch(source, -1) {
			used[match[1]] = struct{}{}
		}
	}
	// A tp() call owns the optional singular/plural variants; Persian
	// carries both keys with the same form so the key sets stay identical.
	// The base key itself is not a catalog entry.
	for _, match := range i18nPluralRE.FindAllStringSubmatch(source, -1) {
		used[match[1]+".one"] = struct{}{}
		used[match[1]+".other"] = struct{}{}
	}
}

func collectCatalogMentions(source string, catalog map[string]string, used map[string]struct{}) {
	for _, match := range i18nKeyStringRE.FindAllStringSubmatch(source, -1) {
		if _, ok := catalog[match[1]]; ok {
			used[match[1]] = struct{}{}
		}
	}
}

func hasDynamicPrefix(key string) bool {
	for _, prefix := range dynamicPrefix {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
