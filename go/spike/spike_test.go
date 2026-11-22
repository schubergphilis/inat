// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package spike

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// TestClone — go-git clones a local fixture and produces a readable HEAD.
// Mirror of the Rust implementation's `src/git/source/repo.rs::clone_or_fetch` (the clone branch).
func TestClone(t *testing.T) {
	f := NewFixture(t, "")
	cache := filepath.Join(t.TempDir(), "cache")

	repo, err := git.PlainClone(cache, false, &git.CloneOptions{URL: f.OriginPath})
	if err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Fatalf("HEAD is zero hash")
	}
	if _, err := os.Stat(filepath.Join(cache, ".git")); err != nil {
		t.Fatalf(".git not present in clone: %v", err)
	}
}

// TestFetchIdempotent — re-opening a cache and fetching with no upstream
// changes returns NoErrAlreadyUpToDate and leaves HEAD unchanged.
// Mirror of the `repo exists` branch of `clone_or_fetch`.
func TestFetchIdempotent(t *testing.T) {
	f := NewFixture(t, "")
	cache := filepath.Join(t.TempDir(), "cache")

	if _, err := git.PlainClone(cache, false, &git.CloneOptions{URL: f.OriginPath}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	repo, err := git.PlainOpen(cache)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	head1, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	err = repo.Fetch(&git.FetchOptions{})
	if !errors.Is(err, git.NoErrAlreadyUpToDate) {
		t.Fatalf("expected NoErrAlreadyUpToDate, got %v", err)
	}
	head2, err := repo.Head()
	if err != nil {
		t.Fatalf("Head after fetch: %v", err)
	}
	if head1.Hash() != head2.Hash() {
		t.Fatalf("HEAD changed during idempotent fetch: %s → %s", head1.Hash(), head2.Hash())
	}
}

// TestFetchPicksUpNewCommit — adding a commit upstream and re-fetching
// updates the remote-tracking ref. Resolve refs dynamically so we don't
// hard-depend on `master` vs `main`.
func TestFetchPicksUpNewCommit(t *testing.T) {
	f := NewFixture(t, "")
	cache := filepath.Join(t.TempDir(), "cache")

	if _, err := git.PlainClone(cache, false, &git.CloneOptions{URL: f.OriginPath}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	repo, err := git.PlainOpen(cache)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	originalRemote := firstOriginRef(t, repo)

	f.AddCommit(t, "kits/test-kit/rules/added.md", []byte("new rule"))

	if err := repo.Fetch(&git.FetchOptions{}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		t.Fatalf("Fetch after new upstream commit: %v", err)
	}
	updatedRemote := firstOriginRef(t, repo)
	if updatedRemote.IsZero() {
		t.Fatalf("no remote tracking ref found after fetch")
	}
	if updatedRemote == originalRemote {
		t.Fatalf("remote ref unchanged after upstream commit (%s)", updatedRemote)
	}
}

// TestReadBlob — navigate Head → Commit → Tree → File → Contents and
// assert byte-equal to the fixture content. Mirror of how
// the Rust implementation's `src/git/source/tree.rs` reads `registry.json`.
func TestReadBlob(t *testing.T) {
	f := NewFixture(t, "")
	cache := filepath.Join(t.TempDir(), "cache")

	repo, err := git.PlainClone(cache, false, &git.CloneOptions{URL: f.OriginPath})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	head, _ := repo.Head()
	commitObj, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	tree, err := commitObj.Tree()
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	file, err := tree.File("registry.json")
	if err != nil {
		t.Fatalf("File(registry.json): %v", err)
	}
	contentStr, err := file.Contents()
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}
	if !bytes.Equal([]byte(contentStr), f.Files["registry.json"]) {
		t.Fatalf("blob mismatch:\nwant %q\ngot  %q", f.Files["registry.json"], contentStr)
	}
}

// TestWalkSubtree — `tree.Tree("kits/test-kit").Files().ForEach(...)`
// returns the kit's full file set. Mirror of
// the Rust implementation's `src/git/source/tree.rs::walk_kit_files`.
func TestWalkSubtree(t *testing.T) {
	f := NewFixture(t, "")
	got := walkKitTree(t, f, "kits/test-kit")

	want := map[string][]byte{
		"kit.json":               f.Files["kits/test-kit/kit.json"],
		"skills/review/SKILL.md": f.Files["kits/test-kit/skills/review/SKILL.md"],
		"rules/no-secrets.md":    f.Files["kits/test-kit/rules/no-secrets.md"],
	}
	assertFileSetEqual(t, "kits/test-kit", got, want)
}

// TestSubpathFixture — second fixture nested under `ai-resources/`,
// walked via `tree.Tree("ai-resources/kits/test-kit")`. Mirror of
// `fetch_kit_tree_at_finds_kit_under_subpath`.
func TestSubpathFixture(t *testing.T) {
	f := NewFixture(t, "ai-resources")
	got := walkKitTree(t, f, "ai-resources/kits/test-kit")

	want := map[string][]byte{
		"kit.json":               f.Files["ai-resources/kits/test-kit/kit.json"],
		"skills/review/SKILL.md": f.Files["ai-resources/kits/test-kit/skills/review/SKILL.md"],
		"rules/no-secrets.md":    f.Files["ai-resources/kits/test-kit/rules/no-secrets.md"],
	}
	assertFileSetEqual(t, "ai-resources/kits/test-kit", got, want)
}

// --- helpers ---

func walkKitTree(t *testing.T, f *Fixture, subPath string) map[string][]byte {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "cache")
	repo, err := git.PlainClone(cache, false, &git.CloneOptions{URL: f.OriginPath})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	head, _ := repo.Head()
	commitObj, _ := repo.CommitObject(head.Hash())
	tree, _ := commitObj.Tree()

	sub, err := tree.Tree(subPath)
	if err != nil {
		t.Fatalf("Tree(%s): %v", subPath, err)
	}

	got := map[string][]byte{}
	err = sub.Files().ForEach(func(file *object.File) error {
		c, err := file.Contents()
		if err != nil {
			return err
		}
		got[file.Name] = []byte(c)
		return nil
	})
	if err != nil {
		t.Fatalf("Files().ForEach: %v", err)
	}
	return got
}

func assertFileSetEqual(t *testing.T, label string, got, want map[string][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s file count: want %d, got %d (got=%v)", label, len(want), len(got), sortedKeys(got))
	}
	for path, w := range want {
		g, ok := got[path]
		if !ok {
			t.Fatalf("%s: missing file %q (got=%v)", label, path, sortedKeys(got))
		}
		if !bytes.Equal(g, w) {
			t.Fatalf("%s file %q content mismatch:\nwant %q\ngot  %q", label, path, w, g)
		}
	}
}

// firstOriginRef returns the hash of any ref under refs/remotes/origin/
// that isn't HEAD. Tolerant of `master` vs `main` defaults.
func firstOriginRef(t *testing.T, repo *git.Repository) plumbing.Hash {
	t.Helper()
	refs, err := repo.References()
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	var found plumbing.Hash
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if strings.HasPrefix(name, "refs/remotes/origin/") && !strings.HasSuffix(name, "/HEAD") {
			found = ref.Hash()
			return storer.ErrStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		t.Fatalf("ForEach refs: %v", err)
	}
	return found
}

func sortedKeys(m map[string][]byte) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
