package gui

import (
	"net/url"
	"path"
	"strings"
)

// navigationAllowed reports whether the shell may navigate to uri. base is the
// asset server URL (the tokenized root, ending in "/"); the only other
// permitted document is the empty page WebView2 starts on. Everything else —
// file:// drops, remote pages, data: URLs — is cancelled.
func navigationAllowed(uri, base string) bool {
	if uri == "about:blank" {
		return true
	}
	return underBase(uri, base)
}

// messageSourceAllowed reports whether a web message came from the shell's
// own document. WebView2 reports the source document URI; the per-run token
// in base is what a foreign document cannot present, so this is the bridge's
// authority even if the navigation guard ever misses a transition.
func messageSourceAllowed(source, base string) bool {
	return underBase(source, base)
}

// underBase reports whether raw is a URL on base's scheme, host and path
// prefix. Parsing (rather than a string prefix) keeps a path like
// /token/../evil, a different host that embeds the token in userinfo, and an
// attacker-chosen port from matching.
func underBase(raw, base string) bool {
	if raw == "" || base == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	b, err := url.Parse(base)
	if err != nil || b.Scheme == "" || b.Host == "" || b.Path == "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, b.Scheme) || !strings.EqualFold(u.Host, b.Host) {
		return false
	}
	p := path.Clean(u.Path)
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(p, b.Path)
}
