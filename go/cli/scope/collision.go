// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package scopecmd

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/schubergphilis/inat/core/config"
)

// RunWithCollisionWarnings is WithCollisionWarnings on stderr for the
// scope the CLI flags select. When the scope cannot be resolved op runs
// alone and reports that failure itself.
func RunWithCollisionWarnings(user bool, op func() error) error {
	dir, err := ResolveDir(user)
	if err != nil {
		return op()
	}
	return WithCollisionWarnings(os.Stderr, dir, user, op)
}

// WithCollisionWarnings runs op against the scope rooted at dir, then
// warns on w for every name op added that the other scope also installs
// (spec 0025 §6.3). The other scope is the user scope for a project op,
// and the cwd's project for a --user op. Best effort: any config that
// cannot be read means silence. op's error is returned unchanged.
func WithCollisionWarnings(w io.Writer, dir string, user bool, op func() error) error {
	before := loadOrEmpty(dir)
	if err := op(); err != nil {
		return err
	}
	otherDir, label, ok := otherScope(user)
	if !ok {
		return nil
	}
	other, err := config.LoadConfig(otherDir)
	if err != nil {
		return nil
	}
	after := loadOrEmpty(dir)
	WarnNewCollisions(w, before, after, &other, label)
	return nil
}

// WarnNewCollisions writes one warning line per name present in after
// but not before that other also installs, naming otherLabel and the
// other scope's kit. Names are reported in sorted order.
func WarnNewCollisions(w io.Writer, before, after, other *config.Config, otherLabel string) {
	old := namesByKit(before)
	theirs := namesByKit(other)
	var hits []string
	for name := range namesByKit(after) {
		if _, had := old[name]; had {
			continue
		}
		if _, clash := theirs[name]; clash {
			hits = append(hits, name)
		}
	}
	sort.Strings(hits)
	for _, name := range hits {
		fmt.Fprintf(w, "warning: '%s' is also installed in %s (kit %s); both copies are delivered\n",
			name, otherLabel, theirs[name])
	}
}

// otherScope returns the root and display label of the scope opposite
// the one being changed. A --user op run from home has no project.
func otherScope(user bool) (string, string, bool) {
	if !user {
		home, err := config.HomeDir()
		if err != nil {
			return "", "", false
		}
		return home, "the user scope", true
	}
	cwd, err := os.Getwd()
	if err != nil || config.IsUserRoot(cwd) {
		return "", "", false
	}
	if _, err := os.Stat(config.ConfigPath(cwd)); err != nil {
		return "", "", false
	}
	return cwd, "the project at " + cwd, true
}

func loadOrEmpty(dir string) *config.Config {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return &config.Config{}
	}
	return &cfg
}

// namesByKit maps every installed name to the install key that owns it.
func namesByKit(cfg *config.Config) map[string]string {
	out := map[string]string{}
	for key, kit := range cfg.Installed {
		for _, name := range kit.Names {
			out[name] = key
		}
	}
	return out
}
