// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package testfixture builds disposable git remotes and project layouts
// for CLI tests. Mirrors the Rust implementation's `src/test_helpers.rs` but uses go-git and a
// non-bare worktree as the "remote" (cloning from a local worktree path
// is well-supported and avoids go-git's bare-push quirks).
//
// This package lives in `internal/` so it's only importable from this
// module's tests.
package testfixture

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/schubergphilis/inat/core/config"
)

// RemoteContext is everything a test needs to interact with a fixture
// remote: the URL to clone (a worktree path), the worktree directory,
// and the branch the initial commit landed on.
type RemoteContext struct {
	URL    string
	Work   string
	Branch string
}

// CreateTestRemote builds a fresh worktree under t.TempDir() with the
// standard test-source layout: registry.json + a single test-kit with
// one skill, one always rule, and one contextual rule.
func CreateTestRemote(t *testing.T) *RemoteContext {
	t.Helper()
	work := filepath.Join(t.TempDir(), "remote-work")
	repo, err := git.PlainInit(work, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	files := standardFiles()
	writeAll(t, work, files)
	commit(t, repo, "initial")

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return &RemoteContext{
		URL:    work,
		Work:   work,
		Branch: head.Name().Short(),
	}
}

// UpdateRemote writes files to the existing remote worktree and creates
// a new commit. Used by tests that need "fetch picks up new state".
func UpdateRemote(t *testing.T, ctx *RemoteContext, files map[string]string) {
	t.Helper()
	for path, content := range files {
		writeFile(t, ctx.Work, path, []byte(content))
	}
	repo, err := git.PlainOpen(ctx.Work)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	commit(t, repo, "update")
}

// SetupProject creates a project directory with claude-code agent
// pre-registered and a single source pointing at url.
func SetupProject(t *testing.T, url string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.Config{
		Sources: []config.Source{{
			Name: "test-source",
			URL:  url,
		}},
		Installed: map[string]config.InstalledKit{},
		Agents:    []string{"claude-code"},
	}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return project
}

// SetupProjectNoAgents creates a project with the source registered but
// no agents — useful when the test wants to assert delivery only fires
// after agents add.
func SetupProjectNoAgents(t *testing.T, url string) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.Config{
		Sources: []config.Source{{
			Name: "test-source",
			URL:  url,
		}},
		Installed: map[string]config.InstalledKit{},
		Agents:    []string{},
	}
	if err := config.SaveConfig(project, &cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return project
}

func standardFiles() map[string]string {
	return map[string]string{
		"registry.json":                        `{"version":"1","kits":{"test-kit":{"description":"Test","types":["skill","rule"]}}}`,
		"kits/test-kit/kit.json":               `{"name":"test-kit","description":"Test kit","files":[{"path":"skills/review","type":"skill"},{"path":"rules/no-secrets.md","type":"rule","priority":"always"},{"path":"rules/style.md","type":"rule","priority":"contextual"}]}`,
		"kits/test-kit/skills/review/SKILL.md": "# Review\nDo code review.",
		"kits/test-kit/rules/no-secrets.md":    "Never commit secrets.",
		"kits/test-kit/rules/style.md":         "Follow style guidelines.",
	}
}

func writeAll(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for relPath, content := range files {
		writeFile(t, root, relPath, []byte(content))
	}
}

func writeFile(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
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

// SetupUserScope points $HOME at a fresh temp dir and seeds the
// user-scope config (`$HOME/.inat/config.json`, spec 0025) with the
// test source and the given agents. Returns the home directory, which is
// both the scope root and the homeOverride for cache lookups.
func SetupUserScope(t *testing.T, url string, agents ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Config{
		Sources:   []config.Source{{Name: "test-source", URL: url}},
		Installed: map[string]config.InstalledKit{},
		Agents:    append([]string{}, agents...),
	}
	if err := config.SaveConfig(home, &cfg); err != nil {
		t.Fatalf("save user config: %v", err)
	}
	return home
}
