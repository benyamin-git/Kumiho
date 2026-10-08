package fxa

import (
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

// Vector values generated independently with .NET crypto on 2026-10-05.
const (
	testHawkID  = "2744d7699c962e4e69b5539eaf76149abb58464a287ce6caa44cb856c585b187"
	testHawkKey = "20aab55aea7f3a870bf8afa0cfab90920532eb7ba8fd489b958db9426cad7443"
)

// testSessionTokenHex returns the hex of session token bytes 00..3f.
func testSessionTokenHex() string {
	raw := make([]byte, 64)
	for i := range raw {
		raw[i] = byte(i)
	}
	return hex.EncodeToString(raw)
}

func TestHawkCredentialsFromSessionToken(t *testing.T) {
	creds, err := HawkCredentialsFromSessionToken(testSessionTokenHex())
	if err != nil {
		t.Fatal(err)
	}
	if creds.ID != testHawkID {
		t.Errorf("id = %s, want %s", creds.ID, testHawkID)
	}
	if got := hex.EncodeToString(creds.Key); got != testHawkKey {
		t.Errorf("key = %s, want %s", got, testHawkKey)
	}
	if _, err := HawkCredentialsFromSessionToken("not-hex!"); err == nil {
		t.Error("expected error for non-hex session token")
	}
	if _, err := HawkCredentialsFromSessionToken(""); err == nil {
		t.Error("expected error for empty session token")
	}
}

func TestHawkHeaderVector(t *testing.T) {
	creds, err := HawkCredentialsFromSessionToken(testSessionTokenHex())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse("https://api.accounts.firefox.com/v1/oauth/token")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"client_id":"5882386c6d801776","grant_type":"fxa-credentials","scope":"profile https://identity.mozilla.com/apps/vpn","access_type":"offline"}`)

	got, err := hawkHeader("POST", u, creds, body, 1759660000, []byte{1, 2, 3, 4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	want := `Hawk id="2744d7699c962e4e69b5539eaf76149abb58464a287ce6caa44cb856c585b187", ts="1759660000", nonce="AQIDBAUG", mac="iqwlyK6dMLWko8M2Ia2avLSJR6y8Li+O32l39Sq54rw=", hash="9yqFNj5ft5dZizMmfcQJFt4Y7H8s0k+W85/KwrJnPII="`
	if got != want {
		t.Fatalf("hawk header mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestHawkHeaderGetWithoutBody(t *testing.T) {
	creds, err := HawkCredentialsFromSessionToken(testSessionTokenHex())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse("https://api.accounts.firefox.com/v1/session/status")
	if err != nil {
		t.Fatal(err)
	}
	h, err := hawkHeader("GET", u, creds, nil, 1759660000, []byte{1, 2, 3, 4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, "hash=") {
		t.Errorf("GET without body must not include payload hash: %s", h)
	}
	if !strings.Contains(h, `nonce="AQIDBAUG"`) || !strings.Contains(h, `ts="1759660000"`) {
		t.Errorf("header missing fields: %s", h)
	}
}

func TestHawkHeaderRejectsBadNonce(t *testing.T) {
	creds := HawkCredentials{ID: "x", Key: []byte("key")}
	u, _ := url.Parse("https://api.accounts.firefox.com/")
	if _, err := hawkHeader("GET", u, creds, nil, 1, []byte{1, 2}); err == nil {
		t.Error("expected error for non-6-byte nonce")
	}
}
