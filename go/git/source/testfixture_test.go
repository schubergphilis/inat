// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package source

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// fixture is a temporary git worktree that other clones can pull from.
// Mirrors `go/spike/fixture.go::Fixture` so source tests assert against
// the same shape spike validated.
type fixture struct {
	OriginPath string
	Files      map[string][]byte
}

func newFixture(t *testing.T, subpath string) *fixture {
	t.Helper()
	base := t.TempDir()
	workPath := filepath.Join(base, "work")
	repo, err := git.PlainInit(workPath, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	files := buildFixtureFiles(subpath)
	for relPath, content := range files {
		writeFixtureFile(t, workPath, relPath, content)
	}
	commitFixture(t, repo, "initial commit")
	return &fixture{OriginPath: workPath, Files: files}
}

func (f *fixture) addCommit(t *testing.T, relPath string, content []byte) {
	t.Helper()
	writeFixtureFile(t, f.OriginPath, relPath, content)
	repo, err := git.PlainOpen(f.OriginPath)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	commitFixture(t, repo, "update "+relPath)
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

func writeFixtureFile(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// initRepoWithFile creates a fresh non-bare repo at workPath with a single
// committed file. Used by tests that need a non-default fixture shape.
func initRepoWithFile(workPath, relPath string, content []byte) (*git.Repository, error) {
	repo, err := git.PlainInit(workPath, false)
	if err != nil {
		return nil, err
	}
	full := filepath.Join(workPath, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	if err := wt.AddGlob("."); err != nil {
		return nil, err
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@e.com", When: time.Now()},
	}); err != nil {
		return nil, err
	}
	return repo, nil
}

func commitFixture(t *testing.T, repo *git.Repository, message string) {
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
