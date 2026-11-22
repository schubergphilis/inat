// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package source

import (
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/schubergphilis/inat/core/registry"
	"github.com/schubergphilis/inat/errs"
)

// FetchRegistry reads `registry.json` from the HEAD tree of repo (no
// subpath). Convenience wrapper around FetchRegistryAt.
func FetchRegistry(repo *git.Repository) (registry.Registry, error) {
	return FetchRegistryAt(repo, "")
}

// FetchRegistryAt reads `registry.json` from `<subpath>/` in repo's HEAD
// tree (or the repo root when subpath is empty). Parses the JSON but does
// not validate it — callers must invoke registry.ValidateRegistry.
func FetchRegistryAt(repo *git.Repository, subpath string) (registry.Registry, error) {
	tree, err := headTreeObject(repo)
	if err != nil {
		return registry.Registry{}, err
	}
	pathStr := "registry.json"
	if subpath != "" {
		pathStr = subpath + "/registry.json"
	}
	file, err := tree.File(pathStr)
	if err != nil {
		return registry.Registry{}, errs.NewRegistryValidation(
			fmt.Sprintf("%s not found in source repository", pathStr))
	}
	content, err := file.Contents()
	if err != nil {
		return registry.Registry{}, errs.WrapGit(err)
	}
	return registry.ParseRegistry(content)
}

// FetchKitTree returns the tree SHA and tree object for `kits/<kitName>`
// in the HEAD tree of repo. Convenience wrapper.
func FetchKitTree(repo *git.Repository, kitName string) (plumbing.Hash, *object.Tree, error) {
	return FetchKitTreeAt(repo, "", kitName)
}

// FetchKitTreeAt returns the tree SHA and tree object for the kit
// directory under the optional subpath. Returns *errs.Error (Kind=Git)
// when the kit is not found.
func FetchKitTreeAt(repo *git.Repository, subpath, kitName string) (plumbing.Hash, *object.Tree, error) {
	tree, err := headTreeObject(repo)
	if err != nil {
		return plumbing.ZeroHash, nil, err
	}
	pathStr := "kits/" + kitName
	if subpath != "" {
		pathStr = subpath + "/kits/" + kitName
	}
	sub, err := tree.Tree(pathStr)
	if err != nil {
		return plumbing.ZeroHash, nil, errs.WrapGit(err)
	}
	return sub.Hash, sub, nil
}

// ReadBlob reads a blob's bytes from the given tree by relative path.
// Returns *errs.Error (Kind=Git) on missing path or read failure.
func ReadBlob(tree *object.Tree, path string) ([]byte, error) {
	file, err := tree.File(path)
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	defer reader.Close()
	content, err := file.Contents()
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	return []byte(content), nil
}

// WalkKitFiles walks tree recursively and returns a (path, content) pair
// for every blob. Paths use forward slashes.
func WalkKitFiles(tree *object.Tree) ([]FileBlob, error) {
	var files []FileBlob
	err := tree.Files().ForEach(func(file *object.File) error {
		content, err := file.Contents()
		if err != nil {
			return err
		}
		files = append(files, FileBlob{
			Path:    file.Name,
			Content: []byte(content),
		})
		return nil
	})
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	return files, nil
}

// FileBlob is one file's relative path and content from a tree walk.
type FileBlob struct {
	Path    string
	Content []byte
}

func headTreeObject(repo *git.Repository) (*object.Tree, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, errs.WrapGit(err)
	}
	return tree, nil
}
