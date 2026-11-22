// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package scopecmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func TestResolveDirUserIsHome(t *testing.T) {
	home := t.TempDir()
	testfixture.SetHome(t, home)
	chdir(t, t.TempDir())
	dir, err := scopecmd.ResolveDir(true)
	if err != nil || dir != home {
		t.Errorf("ResolveDir(user) = %q, %v; want %q", dir, err, home)
	}
}

func TestResolveDirProjectIsCwd(t *testing.T) {
	testfixture.SetHome(t, t.TempDir())
	cwd := t.TempDir()
	chdir(t, cwd)
	dir, err := scopecmd.ResolveDir(false)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(cwd)
	got, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Errorf("ResolveDir(project) = %q, want %q", dir, cwd)
	}
}

// Spec 0025 §6.4 / acceptance 8: no project rooted at home.
func TestResolveDirRefusesHomeAsProject(t *testing.T) {
	home := t.TempDir()
	testfixture.SetHome(t, home)
	chdir(t, home)
	if _, err := scopecmd.ResolveDir(false); !errors.Is(err, errs.ErrHomeAsProject) {
		t.Errorf("err = %v, want HomeAsProject", err)
	}
}

// run executes a minimal tree mirroring inat's: --user on root, checked
// in the root PersistentPreRunE.
func run(args ...string) error {
	root := &cobra.Command{Use: "inat", SilenceUsage: true, SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error { return scopecmd.CheckUserAllowed(cmd) }}
	scopecmd.AddUserFlag(root)
	noop := func(*cobra.Command, []string) error { return nil }
	self := &cobra.Command{Use: "self"}
	self.AddCommand(&cobra.Command{Use: "update", RunE: noop})
	root.AddCommand(
		&cobra.Command{Use: "init", RunE: noop},
		&cobra.Command{Use: "validate", RunE: noop},
		&cobra.Command{Use: "install", RunE: noop},
		self,
	)
	root.SetArgs(args)
	root.SetOut(os.Stderr)
	return root.Execute()
}

// Spec 0025 §4 / acceptance 9.
func TestCheckUserAllowed(t *testing.T) {
	for _, args := range [][]string{{"--user", "init"}, {"init", "--user"}, {"validate", "--user"}, {"self", "update", "--user"}} {
		if err := run(args...); err == nil {
			t.Errorf("%v: want usage error", args)
		}
	}
	for _, args := range [][]string{{"--user", "install"}, {"install", "--user"}, {"init"}, {"self", "update"}} {
		if err := run(args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestIsUser(t *testing.T) {
	root := &cobra.Command{Use: "inat"}
	scopecmd.AddUserFlag(root)
	child := &cobra.Command{Use: "status", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(child)
	root.SetArgs([]string{"status", "--user"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !scopecmd.IsUser(child) {
		t.Error("IsUser = false after --user")
	}
	if scopecmd.IsUser(&cobra.Command{Use: "bare"}) {
		t.Error("IsUser without the flag must be false")
	}
}

// chdir is testing.T.Chdir for go1.22: change cwd, restore on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
}
