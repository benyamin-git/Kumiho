package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/benyamin-git/kumiho/internal/version"
)

// The manual update check (D9/A3): on demand only, the official GitHub
// releases feed, unauthenticated. These are the freeze-point constants; the
// checker's Client/BaseURL/Timeout fields exist so tests can inject a local
// server and no test ever touches the network.
const (
	// updateTimeout bounds one check end to end (A3's 10 s).
	updateTimeout = 10 * time.Second
	// updateFeedBase is the GitHub API origin.
	updateFeedBase = "https://api.github.com"
	// updateFeedPath is the "latest release" endpoint of the Kumiho repo.
	updateFeedPath = "/repos/benyamin-git/Kumiho/releases/latest"
	// updateUserAgent identifies the GUI to GitHub.
	updateUserAgent = "kumiho-gui"
	// updateMaxBody bounds the feed response body.
	updateMaxBody = 1 << 20
)

// UpdateInfo is the GUI-side projection of the manual update check
// (inventory D9): the version this build runs (set at controller
// construction, refreshed by each successful check), the latest release the
// feed reported and when the check last succeeded. A failure surfaces through
// the settings notice, not through a field here.
type UpdateInfo struct {
	Current   string `json:"current,omitempty"`    // this build's version (internal/version)
	Latest    string `json:"latest,omitempty"`     // latest release tag from the feed
	URL       string `json:"url,omitempty"`        // the release page (html_url)
	CheckedAt string `json:"checked_at,omitempty"` // RFC3339 UTC; empty until a successful check
}

// UpdateChecker fetches the latest Kumiho release from GitHub. The zero value
// is ready to use; every field is a test/seam override.
type UpdateChecker struct {
	// Client is the HTTP client; nil uses http.DefaultClient. The check's
	// context deadline is the real bound either way.
	Client *http.Client
	// BaseURL overrides the feed origin (updateFeedBase); tests point it at
	// an httptest server.
	BaseURL string
	// Current overrides the version reported in UpdateInfo.Current; empty
	// reports version.Version.
	Current string
	// Timeout overrides the check's deadline (updateTimeout).
	Timeout time.Duration
}

// latestRelease is the slice of the GitHub "latest release" reply the check
// consumes: the tag and the human release page.
type latestRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// Check performs one update check and returns the projection. Errors are
// plain (transport, HTTP status, malformed reply); the controller wraps them
// into the settings notice.
func (u UpdateChecker) Check(ctx context.Context) (UpdateInfo, error) {
	base := strings.TrimSuffix(u.BaseURL, "/")
	if base == "" {
		base = updateFeedBase
	}
	current := u.Current
	if current == "" {
		current = version.Version
	}
	timeout := u.Timeout
	if timeout <= 0 {
		timeout = updateTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+updateFeedPath, nil)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("update check: %w", err)
	}
	req.Header.Set("User-Agent", updateUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := u.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("update check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("update check: releases/latest: HTTP %s", resp.Status)
	}
	var rel latestRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, updateMaxBody)).Decode(&rel); err != nil {
		return UpdateInfo{}, fmt.Errorf("update check: releases/latest: %w", err)
	}
	if rel.TagName == "" {
		return UpdateInfo{}, fmt.Errorf("update check: releases/latest: reply has no tag_name")
	}
	if err := validReleaseURL(rel.HTMLURL); err != nil {
		return UpdateInfo{}, fmt.Errorf("update check: releases/latest: %w", err)
	}
	return UpdateInfo{
		Current:   current,
		Latest:    rel.TagName,
		URL:       rel.HTMLURL,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// validReleaseURL rejects anything that could not be opened by the frozen
// open_external action (http/https only; the feed is untrusted input).
func validReleaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing non-http(s) html_url %q", raw)
	}
	return nil
}

// sameRelease reports whether two release tags name the same version,
// ignoring a leading "v" and case (this build's version string and the feed's
// tag are compared for the "up to date" notice). An empty side never matches.
func sameRelease(a, b string) bool {
	a = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "v")
	b = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(b)), "v")
	return a != "" && a == b
}
