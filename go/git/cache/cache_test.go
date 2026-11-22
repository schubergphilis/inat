// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package cache

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDirUnder(t *testing.T) {
	got := DirUnder(filepath.FromSlash("/home/user"))
	want := filepath.Join("/home/user", ".inat", "cache")
	if got != want {
		t.Fatalf("DirUnder: got %q want %q", got, want)
	}
}

func TestEnsureDirUnderCreates(t *testing.T) {
	dir := t.TempDir()
	got, err := EnsureDirUnder(dir)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".inat", "cache")) {
		t.Fatalf("path suffix: %q", got)
	}
}

func TestEnsureDirIdempotent(t *testing.T) {
	dir := t.TempDir()
	a, err1 := EnsureDirUnder(dir)
	b, err2 := EnsureDirUnder(dir)
	if err1 != nil || err2 != nil {
		t.Fatalf("err: %v %v", err1, err2)
	}
	if a != b {
		t.Fatalf("paths differ: %q vs %q", a, b)
	}
}

func TestPathForSourceUnder(t *testing.T) {
	got := PathForSourceUnder(filepath.FromSlash("/home/user"), "company")
	want := filepath.Join("/home/user", ".inat", "cache", "company")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDifferentSourcesDifferentPaths(t *testing.T) {
	a := PathForSourceUnder("/home/user", "source-a")
	b := PathForSourceUnder("/home/user", "source-b")
	if a == b {
		t.Fatalf("paths must differ")
	}
}
