// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package source clones, fetches, and reads inat source repositories.
//
// Mirrors the Rust implementation's `src/git/source/`. Backed by `go-git` (pure Go) instead of
// libgit2, which lets us cross-compile from a Linux runner to all release
// targets without a C toolchain.
package source

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"

	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/cache"
)

// FetchSource clones a source into the global cache (or fetches into the
// existing cache) and returns the open repository.
func FetchSource(url, sourceName string) (*git.Repository, error) {
	cachePath, err := cache.PathForSource(sourceName)
	if err != nil {
		return nil, err
	}
	return CloneOrFetch(url, cachePath)
}

// CloneOrFetch clones url into cachePath if cachePath does not yet
// contain a git repo, otherwise opens the existing repo and runs a
// fetch + fast-forward against the resolved default branch.
//
// Mirrors Rust's `clone_or_fetch`. Auth is resolved per the URL scheme
// (see resolveAuth).
func CloneOrFetch(url, cachePath string) (*git.Repository, error) {
	if exists, err := dirHasContent(cachePath); err != nil {
		return nil, err
	} else if exists {
		return openAndFetch(cachePath, url)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, errs.WrapIO(err)
	}
	auth := resolveAuth(url)
	repo, err := git.PlainClone(cachePath, false, &git.CloneOptions{
		URL:  url,
		Auth: auth,
	})
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	return repo, nil
}

func openAndFetch(cachePath, url string) (*git.Repository, error) {
	repo, err := git.PlainOpen(cachePath)
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	auth := resolveAuth(url)
	branch, err := defaultBranchName(repo)
	if err != nil {
		return nil, err
	}
	refSpec := config.RefSpec(
		"+refs/heads/" + branch + ":refs/remotes/origin/" + branch,
	)
	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   []config.RefSpec{refSpec},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil, errs.WrapGit(err)
	}
	if err := fastForwardToRemote(repo, branch); err != nil {
		return nil, err
	}
	return repo, nil
}

// HeadTree returns the tree at the repository's current HEAD.
func HeadTree(repo *git.Repository) (*plumbing.Hash, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	return &commit.TreeHash, nil
}

func defaultBranchName(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		// Fall back to common defaults when HEAD is detached or missing.
		if name, ok := firstLocalBranch(repo); ok {
			return name, nil
		}
		return "main", nil
	}
	short := head.Name().Short()
	if short == "" {
		return "main", nil
	}
	return short, nil
}

func firstLocalBranch(repo *git.Repository) (string, bool) {
	iter, err := repo.Branches()
	if err != nil {
		return "", false
	}
	var name string
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		name = ref.Name().Short()
		return storer.ErrStop
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return "", false
	}
	if name == "" {
		return "", false
	}
	return name, true
}

func fastForwardToRemote(repo *git.Repository, branch string) error {
	remoteRefName := plumbing.NewRemoteReferenceName("origin", branch)
	remoteRef, err := repo.Reference(remoteRefName, true)
	if err != nil {
		return errs.WrapGit(err)
	}
	localRefName := plumbing.NewBranchReferenceName(branch)
	newRef := plumbing.NewHashReference(localRefName, remoteRef.Hash())
	if err := repo.Storer.SetReference(newRef); err != nil {
		return errs.WrapGit(err)
	}
	return nil
}

func dirHasContent(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errs.WrapIO(err)
	}
	for _, e := range entries {
		// Empty directory should be treated as "fresh clone target".
		if !strings.HasPrefix(e.Name(), ".") || e.Name() == ".git" {
			return true, nil
		}
	}
	return false, nil
}
