package prefs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempPath points the package at a scratch file under t.TempDir() and returns
// its path, so tests never touch the real per-user preferences.
func tempPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gui.toml")
	t.Setenv(envPrefs, path)
	return path
}

// forceTheme pins the OS-theme seam for the test.
func forceTheme(t *testing.T, name string) {
	t.Helper()
	old := osThemeDefault
	osThemeDefault = func() string { return name }
	t.Cleanup(func() { osThemeDefault = old })
}

func TestLoadMissingFileDefaults(t *testing.T) {
	for _, themeName := range []string{themeLight, themeDark} {
		t.Run(themeName, func(t *testing.T) {
			tempPath(t)
			forceTheme(t, themeName)

			got, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			want := Prefs{Theme: themeName, Accent: defaultAccent, Autostart: true}
			if got != want {
				t.Errorf("Load() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := tempPath(t)
	forceTheme(t, themeLight)

	want := Prefs{
		Theme:          "oled",
		Accent:         "rose",
		Autostart:      false,
		Onboarded:      true,
		CloseHintShown: true,
	}
	want.Window.X, want.Window.Y = -300, 40
	want.Window.Width, want.Window.Height = 1280, 800

	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, key := range []string{
		`theme = "oled"`, `accent = "rose"`, `autostart = false`,
		`onboarded = true`, `close_hint_shown = true`, "[window]",
		"x = -300", "y = 40", "width = 1280", "height = 800",
	} {
		if !strings.Contains(string(data), key) {
			t.Errorf("saved file missing %q:\n%s", key, data)
		}
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadExactTags(t *testing.T) {
	path := tempPath(t)
	forceTheme(t, themeLight)

	const doc = `
theme = "light"
accent = "teal"
autostart = false
onboarded = true
close_hint_shown = true

[window]
x = -320
y = 48
width = 1234
height = 500
`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Prefs{Theme: "light", Accent: "teal", Onboarded: true, CloseHintShown: true}
	want.Window.X, want.Window.Y = -320, 48
	want.Window.Width, want.Window.Height = 1234, 500
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadUnknownValuesFallBack(t *testing.T) {
	cases := map[string]string{
		"unknown": "theme = \"banana\"\naccent = \"chartreuse\"\n",
		"empty":   "theme = \"\"\naccent = \"\"\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			path := tempPath(t)
			forceTheme(t, themeDark)
			if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}

			got, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			want := Prefs{Theme: themeDark, Accent: defaultAccent, Autostart: true}
			if got != want {
				t.Errorf("Load() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadMalformedFileErrors(t *testing.T) {
	path := tempPath(t)
	if err := os.WriteFile(path, []byte("theme = [broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded on malformed TOML, want error")
	}
}

func TestEnvOverridePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom", "prefs.toml")
	t.Setenv(envPrefs, path)
	forceTheme(t, themeLight)

	got, err := resolvePath()
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	if got != path {
		t.Fatalf("resolvePath() = %q, want %q", got, path)
	}

	if err := Save(Prefs{Theme: themeDark, Accent: defaultAccent}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("prefs file not written to override path: %v", err)
	}
}

func TestResolvePathDefault(t *testing.T) {
	t.Setenv(envPrefs, "")
	want := defaultPrefsPath()
	if want == "" {
		if _, err := resolvePath(); err == nil {
			t.Error("resolvePath() succeeded with unknown config dir")
		}
		return
	}
	got, err := resolvePath()
	if err != nil {
		t.Fatalf("resolvePath: %v", err)
	}
	if got != want {
		t.Errorf("resolvePath() = %q, want %q", got, want)
	}
}

func TestSaveAtomicNoResidue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "gui.toml")
	t.Setenv(envPrefs, path)
	forceTheme(t, themeLight)

	if err := Save(Prefs{Theme: themeLight, Accent: defaultAccent, Autostart: true}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	if len(names) != 1 || names[0] != "gui.toml" {
		t.Errorf("prefs dir entries = %v, want [gui.toml]", names)
	}

	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(path); err != nil {
			t.Error(err)
		} else if perm := fi.Mode().Perm(); perm != 0o644 {
			t.Errorf("prefs file mode = %o, want 644", perm)
		}
		if fi, err := os.Stat(filepath.Dir(path)); err != nil {
			t.Error(err)
		} else if perm := fi.Mode().Perm(); perm != 0o700 {
			t.Errorf("prefs dir mode = %o, want 700", perm)
		}
	}
}

func TestPlatformThemeDefault(t *testing.T) {
	switch got := platformThemeDefault(); got {
	case themeLight, themeDark:
	default:
		t.Errorf("platformThemeDefault() = %q, want %q or %q", got, themeLight, themeDark)
	}
}
