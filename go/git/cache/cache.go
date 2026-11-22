// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package cache resolves on-disk locations for cached source repositories
// under `~/.inat/cache/`. Mirrors the Rust implementation's `src/git/cache.rs`.
package cache

import (
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

const (
	inatDir  = ".inat"
	cacheDir = "cache"
)

// DirUnder returns the cache directory under a given home path:
// `<home>/.inat/cache/`.
func DirUnder(home string) string {
	return filepath.Join(home, inatDir, cacheDir)
}

// EnsureDirUnder creates the cache directory under a given home path,
// returning the resolved path.
func EnsureDirUnder(home string) (string, error) {
	dir := DirUnder(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errs.WrapIO(err)
	}
	return dir, nil
}

// PathForSource returns `~/.inat/cache/<sourceName>`, with `~` resolved
// by core/config.HomeDir.
func PathForSource(sourceName string) (string, error) {
	home, err := config.HomeDir()
	if err != nil {
		return "", err
	}
	return PathForSourceUnder(home, sourceName), nil
}

// PathForSourceUnder returns `<home>/.inat/cache/<sourceName>` without
// touching the filesystem or environment.
func PathForSourceUnder(home, sourceName string) string {
	return filepath.Join(DirUnder(home), sourceName)
}
