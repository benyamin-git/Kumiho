package serverlist

import "testing"

func TestAlternateCandidates(t *testing.T) {
	countries := []Country{
		{
			Code: "DE", Name: "Germany",
			Cities: []City{
				{Code: "FRA", Name: "Frankfurt", Servers: []Server{
					{Hostname: "fra1.example.net", Port: 2499},
					{Hostname: "fra2.example.net", Port: 2499},
				}},
				{Code: "BER", Name: "Berlin", Servers: []Server{
					{Hostname: "ber1.example.net", Port: 2499},
				}},
			},
		},
		{
			Code: "REC", Name: "Recomended Location",
			Cities: []City{{Code: "p", Name: "Recommended", Servers: []Server{
				{Hostname: "rec1.example.net", Port: 2499},
			}}},
		},
		{
			Code: "AR", Name: "Argentina",
			Cities: []City{{Code: "EZE", Name: "Buenos Aires", Servers: []Server{
				{Hostname: "eze1.example.net", Port: 2499},
			}}},
		},
	}

	cur := Candidate{CountryCode: "DE", CountryName: "Germany", CityCode: "FRA", Host: "fra1.example.net", Port: 2499}
	got := AlternateCandidates(countries, cur, 3)
	want := []string{"fra2.example.net:2499", "ber1.example.net:2499", "rec1.example.net:2499"}
	if len(got) != len(want) {
		t.Fatalf("alternates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Address() != want[i] {
			t.Fatalf("alternates = %v, want %v", got, want)
		}
	}

	// max is respected and the current host is never repeated.
	if one := AlternateCandidates(countries, cur, 1); len(one) != 1 || one[0].Host != "fra2.example.net" {
		t.Fatalf("max=1 alternates = %v", one)
	}

	// Without a REC pool the alternates stop at the home country.
	sameCountry := AlternateCandidates([]Country{countries[0]}, cur, 3)
	if len(sameCountry) != 2 || sameCountry[0].Host != "fra2.example.net" || sameCountry[1].Host != "ber1.example.net" {
		t.Fatalf("same-country alternates = %v", sameCountry)
	}
}
