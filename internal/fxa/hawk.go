package fxa

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// HawkCredentials are the derived identifiers used to sign FxA requests.
type HawkCredentials struct {
	ID  string // hex tokenID
	Key []byte // HMAC key
}

// HawkCredentialsFromSessionToken derives Hawk credentials from the hex
// sessionToken returned by /account/login: HKDF(sessionToken, info=
// prefix+"sessionToken", 64 bytes) → id = hex(first 32), key = last 32.
func HawkCredentialsFromSessionToken(sessionTokenHex string) (HawkCredentials, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(sessionTokenHex))
	if err != nil {
		return HawkCredentials{}, fmt.Errorf("session token is not valid hex: %w", err)
	}
	if len(raw) == 0 {
		return HawkCredentials{}, fmt.Errorf("session token is empty")
	}
	expanded := hkdfSHA256(raw, protocolPrefix+"sessionToken", 64)
	return HawkCredentials{
		ID:  hex.EncodeToString(expanded[:32]),
		Key: expanded[32:],
	}, nil
}

// hawkHeader builds the Hawk Authorization header value for an FxA API call.
// nonce must be 6 random bytes; ts is unix seconds. The MAC is Base64 per the
// FxA implementation (PLAN.md §2.1).
func hawkHeader(method string, u *url.URL, creds HawkCredentials, body []byte, ts int64, nonce []byte) (string, error) {
	if len(nonce) != 6 {
		return "", fmt.Errorf("hawk nonce must be 6 bytes, got %d", len(nonce))
	}
	nonceB64 := base64.RawURLEncoding.EncodeToString(nonce)

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}

	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}

	payloadHash := ""
	if len(body) > 0 {
		h := sha256.New()
		h.Write([]byte("hawk.1.payload\napplication/json\n"))
		h.Write(body)
		h.Write([]byte("\n"))
		payloadHash = base64.StdEncoding.EncodeToString(h.Sum(nil))
	}

	normalized := fmt.Sprintf("hawk.1.header\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n\n",
		ts, nonceB64, strings.ToUpper(method), path, u.Hostname(), port, payloadHash)

	mac := hmac.New(sha256.New, creds.Key)
	mac.Write([]byte(normalized))
	header := fmt.Sprintf(`Hawk id="%s", ts="%d", nonce="%s", mac="%s"`,
		creds.ID, ts, nonceB64, base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	if payloadHash != "" {
		header += fmt.Sprintf(`, hash="%s"`, payloadHash)
	}
	return header, nil
}
