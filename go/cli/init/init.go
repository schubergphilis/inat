// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package initcmd implements `inat init [--agent <name>] [--kits-repo]`.
//
// Mirrors the Rust implementation's `src/cli/init.rs`.
package initcmd

import (
	"fmt"
	"os"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/agents/builtin"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/scope"
)

// RunInit creates `.inat/config.json` in the current working directory.
//
// agent: pass "" to skip agent registration; otherwise the named profile
// is validated and registered.
// kitsRepo: when true, persists `kits_repo: true` in config; combined
// with a non-empty agent, additionally delivers the `inat-kits` rule.
func RunInit(agent string, kitsRepo bool) error {
	dir, err := scopecmd.ResolveDir(false)
	if err != nil {
		return err
	}
	return Init(dir, agent, kitsRepo)
}

// Init runs the init flow against an explicit project directory.
func Init(dir, agent string, kitsRepo bool) error {
	if agent != "" {
		if _, err := agents.GetProfile(agent); err != nil {
			return err
		}
	}
	if err := config.EnsureConfig(dir); err != nil {
		return err
	}
	if kitsRepo {
		cfg, err := config.LoadConfig(dir)
		if err != nil {
			return err
		}
		cfg.KitsRepo = true
		if err := config.SaveConfig(dir, &cfg); err != nil {
			return err
		}
	}
	if agent != "" {
		cfg, err := config.LoadConfig(dir)
		if err != nil {
			return err
		}
		cfg.Agents = append(cfg.Agents, agent)
		usageDelivered, err := builtin.DeliverInatUsage(dir, agent, scope.Project)
		if err != nil {
			return err
		}
		builtinEntries := []config.DeliveredFile{usageDelivered}
		if kitsRepo {
			kitsDelivered, err := builtin.DeliverInatKits(dir, agent)
			if err != nil {
				return err
			}
			builtinEntries = append(builtinEntries, kitsDelivered)
		}
		cfg.BuiltinDelivered[agent] = builtinEntries
		if err := config.SaveConfig(dir, &cfg); err != nil {
			return err
		}
		if kitsRepo {
			fmt.Fprintf(os.Stderr, "initialized inat kits repo with agent '%s'\n", agent)
		} else {
			fmt.Fprintf(os.Stderr, "initialized inat project with agent '%s'\n", agent)
		}
	} else if kitsRepo {
		fmt.Fprintln(os.Stderr, "initialized inat kits repo")
	} else {
		fmt.Fprintln(os.Stderr, "initialized inat project")
	}
	return nil
}
