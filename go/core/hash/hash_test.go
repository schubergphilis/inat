// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package hash

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

const (
	helloWorldHash = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	emptyHash      = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func TestHashBytesKnownContent(t *testing.T) {
	got := HashBytes([]byte("hello world"))
	if got != helloWorldHash {
		t.Errorf("HashBytes(\"hello world\") = %q, want %q", got, helloWorldHash)
	}
}

func TestHashFileKnownContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if got != helloWorldHash {
		t.Errorf("HashFile = %q, want %q", got, helloWorldHash)
	}
}

func TestHashFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile: %v", err)
	}
	if got != emptyHash {
		t.Errorf("HashFile(empty) = %q, want %q", got, emptyHash)
	}
}

func TestHashFileMissingReturnsIOError(t *testing.T) {
	_, err := HashFile(filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if err == nil {
		t.Fatalf("HashFile on missing path returned nil error")
	}
	if !errors.Is(err, errs.ErrIO) {
		t.Errorf("err kind = %v, want IO", err)
	}
	if !IsNotExist(err) {
		t.Errorf("IsNotExist(err) = false, want true (err = %v)", err)
	}
}

func TestIsModifiedFalseWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, _ := HashFile(path)
	mod, err := IsModified(path, h)
	if err != nil {
		t.Fatalf("IsModified: %v", err)
	}
	if mod {
		t.Errorf("IsModified for unchanged file = true, want false")
	}
}

func TestIsModifiedTrueWhenChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h, _ := HashFile(path)
	if err := os.WriteFile(path, []byte("changed content"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	mod, err := IsModified(path, h)
	if err != nil {
		t.Fatalf("IsModified: %v", err)
	}
	if !mod {
		t.Errorf("IsModified for changed file = false, want true")
	}
}
