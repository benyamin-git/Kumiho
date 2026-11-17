package gui

import "testing"

func TestNavigationAllowed(t *testing.T) {
	const base = "http://127.0.0.1:41234/tok-1/"
	cases := []struct {
		uri  string
		want bool
	}{
		{"about:blank", true},
		{base, true},
		{base + "index.html", true},
		{base + "js/app.js", true},
		{"http://127.0.0.1:41234/other/", false},
		{"http://127.0.0.1:41234/tok-2/", false},
		{"http://example.com/", false},
		{"file:///home/user/dropped.html", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := navigationAllowed(tc.uri, base); got != tc.want {
			t.Errorf("navigationAllowed(%q) = %v; want %v", tc.uri, got, tc.want)
		}
	}
	if got := navigationAllowed("about:blank", ""); !got {
		t.Error("about:blank is the initial document and must stay allowed")
	}
}

func TestMessageSourceAllowed(t *testing.T) {
	const base = "http://127.0.0.1:41234/tok-1/"
	for _, tc := range []struct {
		source string
		want   bool
	}{
		{base, true},
		{base + "index.html", true},
		{"http://127.0.0.1:41234/other/", false},
		{"http://127.0.0.1:41234/tok-1/../evil", false},
		{"file:///tmp/dropped.html", false},
		{"", false},
	} {
		if got := messageSourceAllowed(tc.source, base); got != tc.want {
			t.Errorf("messageSourceAllowed(%q) = %v; want %v", tc.source, got, tc.want)
		}
	}
}
