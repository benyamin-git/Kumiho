package serverlist

import (
	"encoding/json"
	"testing"
)

// fixture mirrors the live collection shape (verified 2026-10-05): direct
// country records, one server per city, hostname+port only, plus a REC
// pseudo-country and an excluded CatchAll record.
const fixture = `{
  "data": [
    {
      "code": "REC",
      "name": "Recomended Location",
      "cities": [
        {"code": "p", "name": "Recommended", "servers": [{"hostname": "p.m1.fastly-masque.net", "port": 2499}]}
      ]
    },
    {
      "code": "DE",
      "name": "Germany",
      "cities": [
        {"code": "FRA", "name": "Frankfurt", "servers": [{"hostname": "fra1.example.net", "port": 2499}]}
      ]
    },
    {
      "code": "US",
      "name": "CatchAll Anycast",
      "cities": [
        {"code": "any", "name": "Anycast", "servers": [{"hostname": "p.m1.fastly-masque.net", "port": 2499}]}
      ]
    },
    {
      "code": "NL",
      "name": "Netherlands",
      "country": {
        "code": "NL",
        "name": "Netherlands",
        "cities": [
          {
            "code": "AMS",
            "name": "Amsterdam",
            "servers": [
              {"hostname": "ams1.example.net", "port": 2499},
              {"hostname": "ams2.example.net", "port": 2499, "quarantined": true},
              {
                "hostname": "ams-fallback.example.net",
                "port": 443,
                "protocols": [
                  {"name": "connect", "host": "connect.example.net", "port": 443, "scheme": "https"}
                ]
              },
              {
                "hostname": "ams-no-connect.example.net",
                "port": 2499,
                "protocols": [{"name": "masque", "host": "masque.example.net", "port": 443}]
              }
            ]
          }
        ]
      }
    },
    {
      "code": "XX",
      "name": "NoCities"
    },
    {
      "code": "",
      "name": "NoCode",
      "cities": [{"code": "A", "name": "A", "servers": [{"hostname": "a.example.net", "port": 1}]}]
    }
  ]
}`

func TestParse(t *testing.T) {
	countries, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	// XX (no cities), empty-code, and CatchAll must be dropped.
	if len(countries) != 3 {
		t.Fatalf("countries = %d, want 3: %+v", len(countries), countries)
	}
	codes := []string{countries[0].Code, countries[1].Code, countries[2].Code}
	want := map[string]bool{"REC": true, "DE": true, "NL": true}
	for _, c := range codes {
		if !want[c] {
			t.Fatalf("unexpected country %q in %v", c, codes)
		}
	}
}

func TestParseNestedCountry(t *testing.T) {
	countries, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	var nl *Country
	for i := range countries {
		if countries[i].Code == "NL" {
			nl = &countries[i]
		}
	}
	if nl == nil {
		t.Fatal("nested country record not parsed")
	}
	if len(nl.Cities) != 1 || nl.Cities[0].Code != "AMS" {
		t.Fatalf("NL cities = %+v", nl.Cities)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte(`{"data": "not-an-array"}`)); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Fatal("expected parse error")
	}
	countries, err := Parse([]byte(`{"data": [{"code": "DE", "name": "Germany", "cities": []}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(countries) != 0 {
		t.Fatalf("empty cities should be skipped, got %+v", countries)
	}
}

func TestConnectTarget(t *testing.T) {
	withProtocols := Server{
		Hostname: "fallback.example.net",
		Port:     2499,
		Protocols: []Protocol{
			{Name: "connect", Host: "connect.example.net", Port: 443},
		},
	}
	host, port, ok := withProtocols.ConnectTarget()
	if !ok || host != "connect.example.net" || port != 443 {
		t.Fatalf("connect protocol target = %s:%d ok=%v", host, port, ok)
	}

	noProtocols := Server{Hostname: "h.example.net", Port: 2499}
	host, port, ok = noProtocols.ConnectTarget()
	if !ok || host != "h.example.net" || port != 2499 {
		t.Fatalf("fallback target = %s:%d ok=%v", host, port, ok)
	}

	otherProtocols := Server{Hostname: "h.example.net", Port: 2499, Protocols: []Protocol{{Name: "masque"}}}
	if _, _, ok := otherProtocols.ConnectTarget(); ok {
		t.Fatal("server without connect protocol must be rejected")
	}

	empty := Server{}
	if _, _, ok := empty.ConnectTarget(); ok {
		t.Fatal("empty server must be rejected")
	}
}

func TestCandidates(t *testing.T) {
	countries, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	all := Candidates(countries, "", "")
	// REC 1 + DE 1 + NL: ams1 + connect-protocol server (quarantined and
	// no-connect excluded) = 4.
	if len(all) != 4 {
		t.Fatalf("all candidates = %d, want 4: %+v", len(all), all)
	}

	nl := Candidates(countries, "nl", "")
	if len(nl) != 2 {
		t.Fatalf("NL candidates = %+v", nl)
	}
	for _, c := range nl {
		if c.CountryCode != "NL" || c.CityCode != "AMS" {
			t.Fatalf("bad candidate: %+v", c)
		}
	}
	if nl[1].Address() != "connect.example.net:443" {
		t.Fatalf("connect protocol candidate = %s", nl[1].Address())
	}

	if got := Candidates(countries, "FR", ""); len(got) != 0 {
		t.Fatalf("unknown country should yield nothing, got %+v", got)
	}
}

func TestSortCountriesPutsRECFirst(t *testing.T) {
	countries := []Country{{Code: "DE", Name: "Germany"}, {Code: "REC", Name: "Recommended"}, {Code: "AR", Name: "Argentina"}}
	SortCountries(countries)
	if countries[0].Code != "REC" || countries[1].Code != "AR" || countries[2].Code != "DE" {
		t.Fatalf("order = %s, %s, %s", countries[0].Code, countries[1].Code, countries[2].Code)
	}
}

func TestParseRoundTripsWireJSON(t *testing.T) {
	// The daemon ships []Country in IPC envelopes; ensure JSON tags survive.
	countries, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(countries)
	if err != nil {
		t.Fatal(err)
	}
	var back []Country
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != len(countries) {
		t.Fatalf("round trip length %d != %d", len(back), len(countries))
	}
}
