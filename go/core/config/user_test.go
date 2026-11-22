// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
)

// HomeDir must resolve the same folder Claude Code uses for `~/.claude`
// (spec 0025 §6.5): os.UserHomeDir, i.e. $HOME off Windows.
func TestHomeDirFollowsHOME(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows resolves USERPROFILE; checked manually (spec 0025 acceptance 12)")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got, err := HomeDir()
	if err != nil {
		t.Fatalf("HomeDir: %v", err)
	}
	if got != tmp {
		t.Errorf("HomeDir = %q, want %q", got, tmp)
	}
}

// Off Windows, USERPROFILE is not a fallback: os.UserHomeDir reads $HOME
// only, so an unset $HOME is an error rather than a silent other folder.
func TestHomeDirIgnoresUSERPROFILE(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("USERPROFILE is the Windows home")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", t.TempDir())
	if got, err := HomeDir(); err == nil {
		t.Errorf("HomeDir = %q, want error with $HOME unset", got)
	}
}

// setHome is testfixture.SetHome, which this package cannot import:
// os.UserHomeDir reads $HOME off Windows and %USERPROFILE% on Windows.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func writeUserConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".inat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec 0025 §3/§4: no `init --user`; a missing user config is empty.
func TestLoadUserConfigMissingIsEmpty(t *testing.T) {
	home := t.TempDir()
	cfg, err := LoadUserConfig(home)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if cfg.Scope != scope.User {
		t.Errorf("Scope = %v, want user", cfg.Scope)
	}
	if cfg.Sources == nil || cfg.Installed == nil || cfg.Agents == nil || cfg.BuiltinDelivered == nil {
		t.Errorf("want non-nil empty fields, got %+v", cfg)
	}
	if _, err := os.Stat(ConfigPath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("load must not create the file (stat err = %v)", err)
	}
}

// A spec-0006 file holding only `agents` loads as a user scope with no
// sources and no kits.
func TestLoadUserConfigSpec0006File(t *testing.T) {
	home := t.TempDir()
	writeUserConfig(t, home, `{"agents":["claude-code"]}`)
	cfg, err := LoadUserConfig(home)
	if err != nil {
		t.Fatalf("LoadUserConfig: %v", err)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0] != "claude-code" || len(cfg.Sources) != 0 || len(cfg.Installed) != 0 {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadUserConfigRejectsKitsRepo(t *testing.T) {
	home := t.TempDir()
	writeUserConfig(t, home, `{"agents":[],"kits_repo":true}`)
	_, err := LoadUserConfig(home)
	if !errors.Is(err, errs.ErrConfigInvalid) {
		t.Fatalf("err = %v, want ConfigInvalid", err)
	}
	if !strings.Contains(err.Error(), "kits_repo") {
		t.Errorf("error must name the field: %v", err)
	}
}

// LoadConfig routes the home directory to the user scope (spec 0025
// §6.4: root == home is equivalent to user scope).
func TestLoadConfigAtHomeIsUserScope(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	cfg, err := LoadConfig(home)
	if err != nil {
		t.Fatalf("LoadConfig(home): %v", err)
	}
	if cfg.Scope != scope.User {
		t.Errorf("Scope = %v, want user", cfg.Scope)
	}
}

func TestLoadConfigElsewhereIsProjectScope(t *testing.T) {
	setHome(t, t.TempDir())
	project := t.TempDir()
	if _, err := LoadConfig(project); !errors.Is(err, errs.ErrConfigNotFound) {
		t.Errorf("missing project config: err = %v, want ConfigNotFound", err)
	}
	if err := EnsureConfig(project); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(project)
	if err != nil || cfg.Scope != scope.Project {
		t.Errorf("cfg.Scope = %v, err = %v; want project, nil", cfg.Scope, err)
	}
}

func TestIsUserRoot(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	if err := os.Mkdir(filepath.Join(home, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsUserRoot(home) {
		t.Error("IsUserRoot(home) = false")
	}
	if !IsUserRoot(filepath.Join(home, "sub", "..")) {
		t.Error("IsUserRoot(home/sub/..) = false")
	}
	if IsUserRoot(filepath.Join(home, "sub")) || IsUserRoot(t.TempDir()) {
		t.Error("IsUserRoot(other) = true")
	}
}
