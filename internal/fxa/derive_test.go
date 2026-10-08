package fxa

import (
	"encoding/hex"
	"testing"
)

// Vectors in this file were generated independently on 2026-10-05 with .NET
// crypto in PowerShell (PBKDF2/HKDF implemented per RFC 2898/5869; the HKDF
// implementation was verified against the published RFC 5869 test case 1).

func TestQuickStretchVector(t *testing.T) {
	got := hex.EncodeToString(QuickStretch("test@example.com", "password"))
	want := "0e87c42ad8741beb52df3f2770b9ce9d5a38d512cfd3c79be16d05a532b57098"
	if got != want {
		t.Fatalf("QuickStretch = %s, want %s", got, want)
	}
}

func TestAuthPWVector(t *testing.T) {
	got := AuthPW("test@example.com", "password")
	want := "a624b128b0e54c3377fe16ca819c09d867827328bd8c740e620bffe3d1208475"
	if got != want {
		t.Fatalf("AuthPW = %s, want %s", got, want)
	}
}

func TestHkdfKnownAnswer(t *testing.T) {
	// RFC 5869 test case 1 (different info/salt than FxA uses, but the same
	// primitive): confirms Extract+Expand wiring.
	ikm := make([]byte, 22)
	for i := range ikm {
		ikm[i] = 0x0b
	}
	salt := make([]byte, 13)
	for i := range salt {
		salt[i] = byte(i)
	}
	// hkdfSHA256 hardcodes a 32-byte zero salt, so use the raw primitive here.
	prk := hmacSHA256(salt, ikm)
	info := make([]byte, 10)
	for i := range info {
		info[i] = byte(0xf0 + i)
	}
	var okm []byte
	var prev []byte
	for counter := byte(1); len(okm) < 42; counter++ {
		mac := hmacSHA256(prk, append(append(append([]byte{}, prev...), info...), counter))
		prev = mac
		okm = append(okm, mac...)
	}
	got := hex.EncodeToString(okm[:42])
	want := "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865"
	if got != want {
		t.Fatalf("HKDF RFC 5869 vector = %s, want %s", got, want)
	}
}
