// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// legacyHash is the byte-exact SHA-256 that v0.7.0 and earlier recorded.
// Computed independently of the package so the test pins the format.
func legacyHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestHashBytesIgnoresCRLF(t *testing.T) {
	lf := HashBytes([]byte("a\nb\n"))
	crlf := HashBytes([]byte("a\r\nb\r\n"))
	if lf != crlf {
		t.Errorf("HashBytes LF = %q, CRLF = %q; want equal", lf, crlf)
	}
}

func TestHashBytesKeepsLoneCR(t *testing.T) {
	if HashBytes([]byte("a\rb")) == HashBytes([]byte("ab")) {
		t.Errorf("lone CR was stripped; want it kept")
	}
	if HashBytes([]byte("a\rb")) == HashBytes([]byte("a\nb")) {
		t.Errorf("lone CR was turned into LF; want it kept")
	}
}

func TestIsMatchAcceptsNormalisedAndRawHashes(t *testing.T) {
	cases := []struct {
		name, disk, expected string
	}{
		{"CRLF disk, LF recorded (new hash)", "a\r\nb\r\n", HashBytes([]byte("a\nb\n"))},
		{"CRLF disk, LF recorded (legacy raw hash)", "a\r\nb\r\n", legacyHash("a\nb\n")},
		{"CRLF disk, CRLF recorded (legacy raw hash)", "a\r\nb\r\n", legacyHash("a\r\nb\r\n")},
		{"LF disk, LF recorded (legacy raw hash)", "a\nb\n", legacyHash("a\nb\n")},
	}
	for _, tc := range cases {
		if !IsMatch([]byte(tc.disk), tc.expected) {
			t.Errorf("%s: IsMatch = false, want true", tc.name)
		}
	}
}

func TestIsMatchRejectsRealEdit(t *testing.T) {
	for _, recorded := range []string{HashBytes([]byte("a\nb\n")), legacyHash("a\nb\n")} {
		if IsMatch([]byte("a\r\nX\r\n"), recorded) {
			t.Errorf("edited CRLF content matched %q; want no match", recorded)
		}
	}
}

func TestIsModifiedIgnoresCRLFCheckout(t *testing.T) {
	path := writeTemp(t, "a\r\nb\r\n")
	for _, recorded := range []string{HashBytes([]byte("a\nb\n")), legacyHash("a\nb\n")} {
		mod, err := IsModified(path, recorded)
		if err != nil {
			t.Fatalf("IsModified: %v", err)
		}
		if mod {
			t.Errorf("CRLF-only change reported as modified against %q", recorded)
		}
	}
}
