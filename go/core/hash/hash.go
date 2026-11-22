// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package hash computes SHA-256 hashes for delivered files.
//
// Output is lowercase hex. Hashes are line-ending insensitive: CRLF is
// normalised to LF before hashing, so a Windows checkout under
// `core.autocrlf=true` does not read as a local edit (plan 0023). This
// departs from the Rust implementation's `src/core/hash.rs`, which hashed raw bytes.
package hash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"

	"github.com/schubergphilis/inat/errs"
)

// HashBytes returns the lowercase-hex SHA-256 of content with every CRLF
// replaced by LF. A lone CR is kept.
func HashBytes(content []byte) string {
	return rawHash(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")))
}

func rawHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// IsMatch reports whether content matches expectedHash. It accepts the
// line-ending-normalised hash and, for configs written by v0.7.0 and
// earlier, the raw byte hash.
func IsMatch(content []byte, expectedHash string) bool {
	return HashBytes(content) == expectedHash || rawHash(content) == expectedHash
}

// HashFile reads path and returns its lowercase-hex SHA-256.
//
// Returns *errs.Error with Kind=IO wrapping os.ErrNotExist (or other) on
// read failure.
func HashFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", errs.WrapIO(err)
	}
	return HashBytes(content), nil
}

// IsModified returns true iff the file at path does not match
// expectedHash (see IsMatch). Returns the same IO error as HashFile if
// the file cannot be read.
func IsModified(path, expectedHash string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, errs.WrapIO(err)
	}
	return !IsMatch(content, expectedHash), nil
}

// IsNotExist is a small helper for tests/callers that want to detect the
// file-missing case after an IO-wrapped error. Mirrors the Rust test's
// `matches!(err, InatError::Io(e) if e.kind() == io::ErrorKind::NotFound)`.
func IsNotExist(err error) bool {
	var e *errs.Error
	if !errors.As(err, &e) || e.Kind != errs.IO {
		return false
	}
	return errors.Is(e.Cause, os.ErrNotExist)
}
