// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package source

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"

	"github.com/schubergphilis/inat/errs"
)

func TestCloneFreshSource(t *testing.T) {
	f := newFixture(t, "")
	cachePath := filepath.Join(t.TempDir(), "cache", "test-source")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("CloneOrFetch: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Fatalf("HEAD is zero")
	}
}

func TestCloneOrFetchUpdatesExisting(t *testing.T) {
	f := newFixture(t, "")
	cachePath := filepath.Join(t.TempDir(), "cache", "test-source")
	if _, err := CloneOrFetch(f.OriginPath, cachePath); err != nil {
		t.Fatalf("first clone: %v", err)
	}
	f.addCommit(t, "kits/test-kit/rules/added.md", []byte("new"))
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	head, _ := repo.Head()
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	tree, _ := commit.Tree()
	if _, err := tree.File("kits/test-kit/rules/added.md"); err != nil {
		t.Fatalf("added.md not visible after fetch: %v", err)
	}
}

func TestFetchRegistry(t *testing.T) {
	f := newFixture(t, "")
	cachePath := filepath.Join(t.TempDir(), "cache", "src")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	reg, err := FetchRegistry(repo)
	if err != nil {
		t.Fatalf("fetch registry: %v", err)
	}
	if reg.Version != "1" {
		t.Fatalf("version mismatch: %q", reg.Version)
	}
	if _, ok := reg.Kits["test-kit"]; !ok {
		t.Fatalf("test-kit missing")
	}
}

func TestFetchKitTreeAndWalk(t *testing.T) {
	f := newFixture(t, "")
	cachePath := filepath.Join(t.TempDir(), "cache", "src")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	hash, tree, err := FetchKitTree(repo, "test-kit")
	if err != nil {
		t.Fatalf("FetchKitTree: %v", err)
	}
	if hash.IsZero() {
		t.Fatalf("hash zero")
	}
	files, err := WalkKitFiles(tree)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	sort.Strings(paths)
	want := []string{"kit.json", "rules/no-secrets.md", "skills/review/SKILL.md"}
	if len(paths) != len(want) {
		t.Fatalf("paths len: got %v want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d]: got %q want %q", i, paths[i], want[i])
		}
	}
}

func TestFetchKitTreeMissing(t *testing.T) {
	f := newFixture(t, "")
	cachePath := filepath.Join(t.TempDir(), "cache", "src")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_, _, err = FetchKitTree(repo, "nonexistent")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrGit) {
		t.Fatalf("want Git error, got %v", err)
	}
}

func TestFetchRegistryMissing(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	if _, err := initRepoWithFile(work, "README.md", []byte("no registry")); err != nil {
		t.Fatalf("init: %v", err)
	}
	cachePath := filepath.Join(t.TempDir(), "cache", "no-reg")
	repo, err := CloneOrFetch(work, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_, err = FetchRegistry(repo)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrRegistryValidation) {
		t.Fatalf("want RegistryValidation, got %v", err)
	}
}

func TestFetchRegistryAtSubpath(t *testing.T) {
	f := newFixture(t, "ai-resources")
	cachePath := filepath.Join(t.TempDir(), "cache", "subpath")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	reg, err := FetchRegistryAt(repo, "ai-resources")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if reg.Version != "1" || reg.Kits["test-kit"].Description == "" {
		t.Fatalf("subpath registry mismatch: %+v", reg)
	}
}

func TestFetchKitTreeAtSubpath(t *testing.T) {
	f := newFixture(t, "ai-resources")
	cachePath := filepath.Join(t.TempDir(), "cache", "subpath")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_, tree, err := FetchKitTreeAt(repo, "ai-resources", "test-kit")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, err := tree.File("kit.json"); err != nil {
		t.Fatalf("kit.json missing: %v", err)
	}
}

func TestFetchRegistryAtMissingSubpath(t *testing.T) {
	f := newFixture(t, "")
	cachePath := filepath.Join(t.TempDir(), "cache", "src")
	repo, err := CloneOrFetch(f.OriginPath, cachePath)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	_, err = FetchRegistryAt(repo, "does-not-exist")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, errs.ErrRegistryValidation) {
		t.Fatalf("want RegistryValidation, got %v", err)
	}
}
