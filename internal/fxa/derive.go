package fxa

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

const protocolPrefix = "identity.mozilla.com/picl/v1/"

// QuickStretch derives the 32-byte quickStretch key (PBKDF2-HMAC-SHA256,
// 1000 rounds, salt = protocol prefix + "quickStretch:" + email).
func QuickStretch(email, password string) []byte {
	salt := protocolPrefix + "quickStretch:" + email
	return pbkdf2SHA256([]byte(password), []byte(salt), 1000, 32)
}

// AuthPW returns the hex-encoded authPW sent to POST /account/login.
func AuthPW(email, password string) string {
	return hex.EncodeToString(hkdfSHA256(QuickStretch(email, password), protocolPrefix+"authPW", 32))
}

// hkdfSHA256 implements RFC 5869 HKDF-Extract/Expand with a 32-byte zero
// salt, as used by the FxA onepw protocol.
func hkdfSHA256(ikm []byte, info string, length int) []byte {
	salt := make([]byte, 32)
	prk := hmacSHA256(salt, ikm)

	infoBytes := []byte(info)
	okm := make([]byte, 0, length)
	var prev []byte
	for counter := byte(1); len(okm) < length; counter++ {
		mac := hmac.New(sha256.New, prk)
		mac.Write(prev)
		mac.Write(infoBytes)
		mac.Write([]byte{counter})
		prev = mac.Sum(nil)
		okm = append(okm, prev...)
	}
	return okm[:length]
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// pbkdf2SHA256 implements PBKDF2-HMAC-SHA256 (RFC 2898).
func pbkdf2SHA256(password, salt []byte, iterations, length int) []byte {
	const hLen = sha256.Size
	numBlocks := (length + hLen - 1) / hLen
	dk := make([]byte, 0, numBlocks*hLen)
	for block := 1; block <= numBlocks; block++ {
		msg := make([]byte, 0, len(salt)+4)
		msg = append(msg, salt...)
		msg = append(msg, byte(block>>24), byte(block>>16), byte(block>>8), byte(block))
		u := hmacSHA256(password, msg)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 2; i <= iterations; i++ {
			u = hmacSHA256(password, u)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:length]
}
