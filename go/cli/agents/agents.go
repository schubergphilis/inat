// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package agentscmd implements `inat agents add/remove/list [--global]`.
//
// Mirrors the Rust implementation's `src/cli/agents.rs`.
package agentscmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/schubergphilis/inat/agents"
	"github.com/schubergphilis/inat/agents/builtin"
	"github.com/schubergphilis/inat/cli/install"
	removecmd "github.com/schubergphilis/inat/cli/remove"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/core/scope"
	"github.com/schubergphilis/inat/errs"
	"github.com/schubergphilis/inat/git/source"
)

// errGlobalWithUser: `--global` only edits the shared agents list, while
// `--user` also delivers; the two cannot be combined (spec 0025 §6.1).
var errGlobalWithUser = fmt.Errorf("--global and --user cannot be combined: --global only edits the agents list, --user also delivers to the user scope")

// RunAgentsAdd handles `inat agents add <name> [--global]` against cwd.
func RunAgentsAdd(name string, global bool, user bool) error {
	if global && user {
		return errGlobalWithUser
	}
	if global {
		return AgentsAddGlobal(name, "")
	}
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return AgentsAdd(dir, name, "")
}

// RunAgentsRemove handles `inat agents remove <name> [--global]` against cwd.
func RunAgentsRemove(name string, global bool, user bool) error {
	if global && user {
		return errGlobalWithUser
	}
	if global {
		return AgentsRemoveGlobal(name, "")
	}
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return AgentsRemove(dir, name, "")
}

// RunAgentsList handles `inat agents list` against cwd.
func RunAgentsList(user bool) error {
	dir, err := scopecmd.ResolveDir(user)
	if err != nil {
		return err
	}
	return AgentsList(dir, "")
}

// AgentsAdd registers an agent in the project config and auto-delivers
// every installed kit to it. Also delivers built-in inat-usage (and
// inat-kits when kits_repo is set).
func AgentsAdd(dir, name, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	profile, err := agents.ProfileFor(name, cfg.Scope)
	if err != nil {
		return err
	}
	if contains(cfg.Agents, name) {
		return errs.NewAgentAlreadyActive(name)
	}
	keys := installKeys(&cfg)
	for _, key := range keys {
		if err := deliverKitToAgent(dir, &cfg, key, profile, homeOverride); err != nil {
			return err
		}
	}
	usageDelivered, err := builtin.DeliverInatUsage(dir, name, cfg.Scope)
	if err != nil {
		return err
	}
	builtinEntries := []config.DeliveredFile{usageDelivered}
	if cfg.KitsRepo {
		kitsDelivered, err := builtin.DeliverInatKits(dir, name)
		if err != nil {
			return err
		}
		builtinEntries = append(builtinEntries, kitsDelivered)
	}
	cfg.Agents = append(cfg.Agents, name)
	cfg.BuiltinDelivered[name] = builtinEntries
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "added agent %s\n", name)
	return nil
}

// AgentsRemove drops an agent from the project config and cleans every
// file delivered to it (skills, contextual rules, managed-section content).
func AgentsRemove(dir, name, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	profile, err := agents.ProfileFor(name, cfg.Scope)
	if err != nil {
		return err
	}
	if !contains(cfg.Agents, name) {
		return errs.NewAgentNotActive(name)
	}
	for _, key := range installKeys(&cfg) {
		if err := removeKitFromAgent(dir, &cfg, key, profile); err != nil {
			return err
		}
	}
	if err := builtin.RemoveInatUsage(dir, name, cfg.Scope); err != nil {
		return err
	}
	// inat-kits is project-only (spec 0025 §6.2).
	if cfg.Scope == scope.Project {
		if err := builtin.RemoveInatKits(dir, name); err != nil {
			return err
		}
	}
	cfg.Agents = removeString(cfg.Agents, name)
	delete(cfg.BuiltinDelivered, name)
	if err := config.SaveConfig(dir, &cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "removed agent %s\n", name)
	return nil
}

// AgentsList prints project-active agents, global-active agents, and
// the available built-in profiles. At user scope the shared list is the
// scope's own, so it prints once as "user agents" (spec 0025 §6.1).
func AgentsList(dir, homeOverride string) error {
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return err
	}
	global, err := loadGlobalCfg(homeOverride)
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, formatAgentsList(&cfg, global.Agents))
	return nil
}

func formatAgentsList(cfg *config.Config, global []string) string {
	var b strings.Builder
	if cfg.Scope == scope.User {
		writeAgentBlock(&b, "user agents:", cfg.Agents)
	} else {
		writeAgentBlock(&b, "project agents:", cfg.Agents)
		writeAgentBlock(&b, "\nglobal agents:", global)
	}
	b.WriteString("\navailable profiles:\n")
	for _, p := range agents.ListProfiles() {
		fmt.Fprintf(&b, "  %s\n", p.Name)
	}
	return b.String()
}

func writeAgentBlock(b *strings.Builder, title string, names []string) {
	b.WriteString(title + "\n")
	if len(names) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range names {
		fmt.Fprintf(b, "  %s\n", a)
	}
}

func deliverKitToAgent(dir string, cfg *config.Config, installKey string, profile agents.AgentProfile, homeOverride string) error {
	installed := cfg.Installed[installKey]
	idx := strings.Index(installKey, "/")
	if idx == -1 {
		return errs.NewKitValidation(fmt.Sprintf("invalid install key '%s'", installKey))
	}
	sourceName, kitName := installKey[:idx], installKey[idx+1:]
	repo, hashRef, err := install.OpenCachedTree(sourceName, installed.Tree, homeOverride)
	if err != nil {
		return err
	}
	tree, err := repo.TreeObject(*hashRef)
	if err != nil {
		return errs.NewKitValidation(fmt.Sprintf(
			"tree not found in cache for '%s', run update first", installKey))
	}
	allFiles, err := source.WalkKitFiles(tree)
	if err != nil {
		return err
	}
	conv := make([]kit.FileBlob, len(allFiles))
	for i, f := range allFiles {
		conv[i] = kit.FileBlob{Path: f.Path, Content: f.Content}
	}
	kitCfg, warnings, err := kit.ReadAndValidateKit(conv)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	newDelivered, err := install.DeliverToAgent(dir, &profile.Mapping, &kitCfg, allFiles, sourceName, kitName, nil)
	if err != nil {
		return err
	}
	updated := cfg.Installed[installKey]
	existing := map[string]struct{}{}
	for _, d := range updated.Delivered {
		existing[d.Path] = struct{}{}
	}
	for _, f := range newDelivered {
		if _, hit := existing[f.Path]; !hit {
			updated.Delivered = append(updated.Delivered, f)
		}
	}
	cfg.Installed[installKey] = updated
	return nil
}

// removeKitFromAgent deletes one kit's files delivered to the agent and
// strips its managed-section content, dropping those files from cfg.
func removeKitFromAgent(dir string, cfg *config.Config, key string, profile agents.AgentProfile) error {
	installed := cfg.Installed[key]
	var agentFiles, remaining []config.DeliveredFile
	for _, f := range installed.Delivered {
		if isAgentFile(f, profile) {
			agentFiles = append(agentFiles, f)
		} else {
			remaining = append(remaining, f)
		}
	}
	installed.Delivered = remaining
	cfg.Installed[key] = installed
	if _, err := removecmd.RemoveDeliveredFiles(dir, agentFiles, true); err != nil {
		return err
	}
	refPaths := make([]string, len(agentFiles))
	for i, f := range agentFiles {
		refPaths[i] = f.Path
	}
	return removecmd.CleanManagedSections(dir, cfg.Scope, []string{profile.Name}, key, refPaths)
}

func isAgentFile(file config.DeliveredFile, profile agents.AgentProfile) bool {
	return strings.HasPrefix(file.Path, profile.Mapping.Skill.Path) ||
		strings.HasPrefix(file.Path, profile.Mapping.Rule.Contextual.Path)
}

func installKeys(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Installed))
	for k := range cfg.Installed {
		keys = append(keys, k)
	}
	return keys
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func removeString(in []string, s string) []string {
	out := in[:0]
	for _, v := range in {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}
