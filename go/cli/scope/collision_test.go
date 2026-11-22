// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package scopecmd_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/schubergphilis/inat/cli/install"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/internal/testfixture"
)

func cfgWith(kits map[string][]string) *config.Config {
	c := &config.Config{Installed: map[string]config.InstalledKit{}}
	for k, names := range kits {
		c.Installed[k] = config.InstalledKit{Names: names}
	}
	return c
}

// Only names that are new in this run and present in the other scope
// are reported, one line each, naming the other scope and its kit.
func TestWarnNewCollisions(t *testing.T) {
	before := cfgWith(map[string][]string{"s/old": {"kept"}})
	after := cfgWith(map[string][]string{"s/old": {"kept"}, "s/new": {"review", "style", "fresh"}})
	other := cfgWith(map[string][]string{"u/k": {"kept", "review", "style"}})
	var buf bytes.Buffer
	scopecmd.WarnNewCollisions(&buf, before, after, other, "the user scope")
	out := buf.String()
	if n := strings.Count(out, "\n"); n != 2 {
		t.Errorf("want 2 lines, got %d:\n%s", n, out)
	}
	for _, want := range []string{"'review'", "'style'", "the user scope", "u/k"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "kept") || strings.Contains(out, "fresh") {
		t.Errorf("reported a name that is not a new collision:\n%s", out)
	}
}

// Acceptance 7: a project install of a name the user scope already
// delivers succeeds and warns; the reverse direction warns too.
func TestCollisionWarningsBothDirections(t *testing.T) {
	ctx := testfixture.CreateTestRemote(t)
	home := testfixture.SetupUserScope(t, ctx.URL, "claude-code")
	project := testfixture.SetupProject(t, ctx.URL)
	chdir(t, project)

	var buf bytes.Buffer
	if err := scopecmd.WithCollisionWarnings(&buf, home, true, func() error {
		return install.Install(home, "test-source/test-kit", home)
	}); err != nil {
		t.Fatalf("user install: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("no project kits yet, want silence:\n%s", buf.String())
	}
	if err := scopecmd.WithCollisionWarnings(&buf, project, false, func() error {
		return install.Install(project, "test-source/test-kit", home)
	}); err != nil {
		t.Fatalf("project install must succeed: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "'review'") || !strings.Contains(out, "user scope") {
		t.Errorf("project install: want collision warning, got:\n%s", out)
	}

	buf.Reset()
	if err := os.RemoveAll(filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadUserConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	delete(cfg.Installed, "test-source/test-kit")
	if err := config.SaveConfig(home, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := scopecmd.WithCollisionWarnings(&buf, home, true, func() error {
		return install.Install(home, "test-source/test-kit", home)
	}); err != nil {
		t.Fatalf("user reinstall: %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "'review'") || !strings.Contains(out, project) {
		t.Errorf("user install: want collision warning naming the project, got:\n%s", out)
	}
}

// Best effort: an unreadable other config is silence, never an error,
// and the operation's own error is returned unchanged.
func TestCollisionWarningsBestEffort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".inat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath(home), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := config.SaveConfig(project, cfgWith(map[string][]string{"s/k": {"review"}})); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := scopecmd.WithCollisionWarnings(&buf, project, false, func() error { return nil }); err != nil {
		t.Errorf("corrupt other config must not fail: %v", err)
	}
	opErr := errors.New("boom")
	if err := scopecmd.WithCollisionWarnings(&buf, project, false, func() error { return opErr }); err != opErr {
		t.Errorf("err = %v, want the op's error", err)
	}
	if buf.Len() != 0 {
		t.Errorf("want silence, got:\n%s", buf.String())
	}
}
