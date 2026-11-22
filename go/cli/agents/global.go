// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package agentscmd

import (
	"fmt"
	"os"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

// AgentsAddGlobal writes the agent into ~/.inat/config.json. Other
// projects pick it up on their next install/update/refresh.
func AgentsAddGlobal(name, homeOverride string) error {
	if _, err := agents.GetProfile(name); err != nil {
		return err
	}
	cfg, err := loadGlobalCfg(homeOverride)
	if err != nil {
		return err
	}
	if contains(cfg.Agents, name) {
		return errs.NewAgentAlreadyActive(name)
	}
	cfg.Agents = append(cfg.Agents, name)
	if err := saveGlobalCfg(homeOverride, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"added agent %s globally. Run 'inat refresh' in each project to deliver existing kits to this agent.\n",
		name)
	return nil
}

// AgentsRemoveGlobal removes an agent from ~/.inat/config.json. Files
// delivered by other projects while the agent was global are NOT cleaned;
// users handle that manually or via re-install.
func AgentsRemoveGlobal(name, homeOverride string) error {
	cfg, err := loadGlobalCfg(homeOverride)
	if err != nil {
		return err
	}
	if !contains(cfg.Agents, name) {
		return errs.NewAgentNotActive(name)
	}
	cfg.Agents = removeString(cfg.Agents, name)
	if err := saveGlobalCfg(homeOverride, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr,
		"removed agent %s globally. Other projects that delivered files to this agent will not be cleaned automatically; remove the files manually or re-install affected kits.\n",
		name)
	return nil
}

// userHome returns homeOverride when set, else the real home directory.
func userHome(homeOverride string) (string, error) {
	if homeOverride != "" {
		return homeOverride, nil
	}
	return config.HomeDir()
}

// loadGlobalCfg loads the full user-scope config: `--global` edits its
// agents list and must round-trip sources and kits (spec 0025 §6.5).
func loadGlobalCfg(homeOverride string) (config.Config, error) {
	home, err := userHome(homeOverride)
	if err != nil {
		return config.Config{}, err
	}
	return config.LoadUserConfig(home)
}

func saveGlobalCfg(homeOverride string, cfg *config.Config) error {
	home, err := userHome(homeOverride)
	if err != nil {
		return err
	}
	return config.SaveConfig(home, cfg)
}
