// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package spike evaluates whether go-git can perform the git operations
// `inat` depends on (clone, idempotent fetch, fetch picks up new commits,
// blob read, subtree walk, subpath layout). The fixture builder mirrors
// the Rust implementation's `src/git/source/test_support.rs::create_test_source` so the spike
// asserts against a known-good shape from the Rust side.
package spike

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Fixture is a temporary git worktree that other clones can pull from.
// Use a local non-bare repo (rather than a bare + push) to avoid go-git's
// push-to-local-bare quirks; cloning from a worktree path is well-supported
// by both git and go-git.
type Fixture struct {
	OriginPath string            // path to clone from (use as URL)
	Files      map[string][]byte // expected contents keyed by forward-slash relative path
}

// NewFixture builds a worktree under t.TempDir() with the standard files.
// If subpath is non-empty, all files are nested under it (mirrors the
// `Source.path` layout that `inat sources add --path …` supports).
func NewFixture(t *testing.T, subpath string) *Fixture {
	t.Helper()
	base := t.TempDir()
	workPath := filepath.Join(base, "work")

	repo, err := git.PlainInit(workPath, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}

	files := buildFixtureFiles(subpath)
	for relPath, content := range files {
		writeFile(t, workPath, relPath, content)
	}

	commit(t, repo, "initial commit")

	return &Fixture{
		OriginPath: workPath,
		Files:      files,
	}
}

// AddCommit writes a file to the fixture worktree and creates a new commit.
// Use this in tests that need to verify "fetch picks up new state".
func (f *Fixture) AddCommit(t *testing.T, relPath string, content []byte) {
	t.Helper()
	writeFile(t, f.OriginPath, relPath, content)

	repo, err := git.PlainOpen(f.OriginPath)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	commit(t, repo, "update "+relPath)
}

func buildFixtureFiles(subpath string) map[string][]byte {
	prefix := ""
	if subpath != "" {
		prefix = subpath + "/"
	}
	return map[string][]byte{
		prefix + "registry.json":                        []byte(`{"version":"1","kits":{"test-kit":{"description":"Test kit","types":["skill","rule"]}}}`),
		prefix + "kits/test-kit/kit.json":               []byte(`{"name":"test-kit","description":"A test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"}]}`),
		prefix + "kits/test-kit/skills/review/SKILL.md": []byte("# Review skill"),
		prefix + "kits/test-kit/rules/no-secrets.md":    []byte("Never commit secrets."),
	}
}

func writeFile(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func commit(t *testing.T, repo *git.Repository, message string) {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("AddGlob: %v", err)
	}
	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
