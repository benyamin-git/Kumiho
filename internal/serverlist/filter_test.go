package serverlist

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"151.0a1", "151.0a2", -1},
		{"151.0a2", "151.0b1", -1},
		{"151.0b1", "151.0rc1", -1},
		{"151.0rc1", "151.0", -1},
		{"151.0", "151.0.1", -1},
		{"151.0.1", "158.0", -1},
		{"158.0", "151.0a1", 1},
		{"151.0a1", "151.0", -1},
		{"151.0rc1", "151.0b9", 1},
		{"9.0", "10.0", -1},
		{"151.0", "151.0.0", 0},
		{"151.0a1", "151.0a01", 0},
		{"151.0a", "151.0a0", 0},
		{"151.0b", "151.0b0", 0},
		{"151.0rc2", "151.0rc1", 1},
		{"151.0", "151.0a1", 1},
		{"2.0", "2.0.0.0", 0},
	}
	for _, tc := range tests {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			if got := compareVersions(tc.a, tc.b); got != tc.want {
				t.Fatalf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			if tc.want != 0 {
				if got := compareVersions(tc.b, tc.a); got != -tc.want {
					t.Fatalf("compareVersions(%q, %q) = %d, want %d", tc.b, tc.a, got, -tc.want)
				}
			}
		})
	}
}

func TestCompareVersionStringsRejectsInvalid(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"", "151.0"},
		{"151.0", "bogus"},
		{"1.0a1.2", "1.0"},
	} {
		if _, err := compareVersionStrings(tc.a, tc.b); err == nil {
			t.Fatalf("compareVersionStrings(%q, %q) error = nil, want error", tc.a, tc.b)
		}
	}
}

func filterFixture(records ...map[string]any) []byte {
	doc := map[string]any{"data": records}
	data, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return data
}

func filterRecord(code string, locked bool, expr string) map[string]any {
	record := map[string]any{
		"code": code,
		"name": code,
		"cities": []any{
			map[string]any{
				"code": "C1",
				"name": "City",
				"servers": []any{
					map[string]any{"hostname": strings.ToLower(code) + ".example.net", "port": 2499},
				},
			},
		},
	}
	if locked {
		record["locked"] = true
	}
	if expr != "" {
		record["filter_expression"] = expr
	}
	return record
}

func parseOneFilter(t *testing.T, expr string, ctx Context, allowLocked bool) ([]Country, []string) {
	t.Helper()
	var logs []string
	got, err := ParseWithOptions(filterFixture(filterRecord("DE", false, expr)), Options{
		Context:     ctx,
		AllowLocked: allowLocked,
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		t.Fatalf("ParseWithOptions: %v", err)
	}
	return got, logs
}

func TestParseWithOptionsLiveUSSplit(t *testing.T) {
	modern := `env.version|versionCompare("151.0a1") >= 0`
	legacy := `env.version|versionCompare("151.0a1") < 0`
	fixture := filterFixture(
		filterRecord("US", false, modern),
		filterRecord("US", false, legacy),
	)

	got, err := ParseWithOptions(fixture, Options{Context: Context{FirefoxVersion: "158.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilterExpression != modern {
		t.Fatalf("158.0 records = %+v, want only the modern expression", got)
	}

	got, err = ParseWithOptions(fixture, Options{Context: Context{FirefoxVersion: "150.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilterExpression != legacy {
		t.Fatalf("150.0 records = %+v, want only the legacy expression", got)
	}

	got, err = ParseWithOptions(fixture, Options{Context: Context{FirefoxVersion: "151.0a1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilterExpression != modern {
		t.Fatalf("151.0a1 records = %+v, want the modern boundary", got)
	}
}

func TestParseWithOptionsBooleanFilters(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ctx  Context
		want bool
	}{
		{"country equality", `env.country == "US"`, Context{Country: "US"}, true},
		{"country inequality", `env.country != "US"`, Context{Country: "DE"}, true},
		{"and true", `env.country == "US" && env.version|versionCompare("151.0a1") >= 0`, Context{Country: "US", FirefoxVersion: "158.0"}, true},
		{"and false version", `env.country == "US" && env.version|versionCompare("151.0a1") >= 0`, Context{Country: "US", FirefoxVersion: "150.0"}, false},
		{"and false country", `env.country == "US" && env.version|versionCompare("151.0a1") >= 0`, Context{Country: "DE", FirefoxVersion: "158.0"}, false},
		{"or first", `env.country == "US" || env.country == "CA"`, Context{Country: "US"}, true},
		{"or second", `env.country == "US" || env.country == "CA"`, Context{Country: "CA"}, true},
		{"or false", `env.country == "US" || env.country == "CA"`, Context{Country: "DE"}, false},
		{"not", `!(env.country == "US")`, Context{Country: "DE"}, true},
		{"not false", `!(env.country == "US")`, Context{Country: "US"}, false},
		{"parentheses", `(env.country == "US" || env.country == "CA") && env.version|versionCompare("151.0") >= 0`, Context{Country: "CA", FirefoxVersion: "158.0"}, true},
		{"single quotes", `env.country == 'US'`, Context{Country: "US"}, true},
		{"literal true", `true`, Context{}, true},
		{"literal false", `false`, Context{}, false},
		{"not literal", `!false`, Context{}, true},
		{"version equality", `env.version == "158.0"`, Context{FirefoxVersion: "158.0"}, true},
		{"integer equality", `1 == 1`, Context{}, true},
		{"integer inequality", `1 != 2`, Context{}, true},
		{"comparison negation", `!(env.version|versionCompare("151.0a1") >= 0)`, Context{FirefoxVersion: "150.0"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, logs := parseOneFilter(t, tc.expr, tc.ctx, false)
			if len(logs) != 0 {
				t.Fatalf("unexpected filter log: %v", logs)
			}
			if included := len(got) == 1; included != tc.want {
				t.Fatalf("included = %v, want %v (records %+v)", included, tc.want, got)
			}
		})
	}
}

func TestParseWithOptionsFilterErrorsFailOpen(t *testing.T) {
	tests := []struct {
		name string
		expr string
		ctx  Context
	}{
		{"unknown identifier", `env.platform == "x"`, Context{}},
		{"unknown transform", `env.version|unknown("151.0") >= 0`, Context{FirefoxVersion: "158.0"}},
		{"syntax error", `env.country == "US" &&`, Context{Country: "US"}},
		{"unterminated string", `env.country == "US`, Context{Country: "US"}},
		{"missing version", `env.version|versionCompare("151.0a1") >= 0`, Context{Country: "US"}},
		{"missing country", `env.country == "US"`, Context{}},
		{"missing in short circuit", `false && env.version|versionCompare("151.0a1") >= 0`, Context{}},
		{"non-boolean result", `env.country`, Context{Country: "US"}},
		{"type mismatch", `env.country == 1`, Context{Country: "US"}},
		{"string ordering", `env.country < "US"`, Context{Country: "DE"}},
		{"invalid target version", `env.version|versionCompare("not-a-version") >= 0`, Context{FirefoxVersion: "158.0"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, logs := parseOneFilter(t, tc.expr, tc.ctx, false)
			if len(got) != 1 {
				t.Fatalf("included = %d, want 1 (fail open)", len(got))
			}
			if len(logs) != 1 {
				t.Fatalf("logs = %v, want one warning", logs)
			}
			if !strings.Contains(logs[0], fmt.Sprintf("%q", tc.expr)) {
				t.Fatalf("log %q does not include expression %q", logs[0], tc.expr)
			}
			if !strings.Contains(logs[0], "DE") {
				t.Fatalf("log %q does not include the record", logs[0])
			}
		})
	}
}

func TestParseWithOptionsLockedFilter(t *testing.T) {
	records := make([]map[string]any, 0, 35)
	for i := 0; i < 35; i++ {
		records = append(records, filterRecord(fmt.Sprintf("C%02d", i), i >= 5, ""))
	}
	fixture := filterFixture(records...)

	got, err := ParseWithOptions(fixture, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("default visible = %d, want 5", len(got))
	}
	for _, c := range got {
		if c.Locked {
			t.Fatalf("default list includes locked record %s", c.Code)
		}
	}

	got, err = ParseWithOptions(fixture, Options{AllowLocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 35 {
		t.Fatalf("allow_locked visible = %d, want 35", len(got))
	}
	locked := 0
	for _, c := range got {
		if c.Locked {
			locked++
		}
	}
	if locked != 30 {
		t.Fatalf("locked records = %d, want 30", locked)
	}
}

func TestParseWithOptionsWrapperMetadata(t *testing.T) {
	fixture := []byte(`{"data":[
		{"filter_expression":"false","locked":true,"country":{
			"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra.example.net","port":2499}]}]
		}}
	]}`)
	got, err := ParseWithOptions(fixture, Options{AllowLocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("wrapper filter false returned %+v", got)
	}

	fixture = []byte(`{"data":[
		{"filter_expression":"true","locked":true,"country":{
			"code":"DE","name":"Germany","cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra.example.net","port":2499}]}]
		}}
	]}`)
	got, err = ParseWithOptions(fixture, Options{AllowLocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Locked || got[0].FilterExpression != "true" {
		t.Fatalf("wrapper metadata = %+v", got)
	}

	fixture = []byte(`{"data":[
		{"country":{
			"code":"DE","name":"Germany","locked":true,"filter_expression":"true",
			"cities":[{"code":"FRA","name":"Frankfurt","servers":[{"hostname":"fra.example.net","port":2499}]}]
		}}
	]}`)
	got, err = ParseWithOptions(fixture, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("inner locked metadata returned %+v", got)
	}
	got, err = ParseWithOptions(fixture, Options{AllowLocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Locked || got[0].FilterExpression != "true" {
		t.Fatalf("inner metadata = %+v", got)
	}
}

func TestParseFailsOpenWithNoContext(t *testing.T) {
	fixture := filterFixture(filterRecord("US", false, `env.version|versionCompare("151.0a1") >= 0`))
	got, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Parse without a context returned %d records, want fail-open inclusion", len(got))
	}
}

func TestParseWithOptionsCatchAllExcluded(t *testing.T) {
	fixture := filterFixture(
		filterRecord("US", false, ""),
		map[string]any{
			"code": "US",
			"name": "CatchAll Anycast",
			"cities": []any{
				map[string]any{
					"code": "C1",
					"name": "City",
					"servers": []any{
						map[string]any{"hostname": "anycast.example.net", "port": 2499},
					},
				},
			},
			"filter_expression": `env.version|versionCompare("151.0a1") < 0`,
		},
	)
	got, err := ParseWithOptions(fixture, Options{Context: Context{FirefoxVersion: "150.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "US" {
		t.Fatalf("CatchAll Anycast was not excluded: %+v", got)
	}
}
