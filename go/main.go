// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// inat — AI prompt and rule kit manager.
//
// Top-level command tree. Each subcommand delegates to a Run* function in
// the matching cli/<name> package. Mirrors the Rust implementation's `src/main.rs` plus its
// clap structure.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	agentscmd "github.com/schubergphilis/inat/cli/agents"
	initcmd "github.com/schubergphilis/inat/cli/init"
	"github.com/schubergphilis/inat/cli/install"
	"github.com/schubergphilis/inat/cli/refresh"
	removecmd "github.com/schubergphilis/inat/cli/remove"
	"github.com/schubergphilis/inat/cli/rename"
	"github.com/schubergphilis/inat/cli/replace"
	scopecmd "github.com/schubergphilis/inat/cli/scope"
	selfcmd "github.com/schubergphilis/inat/cli/self"
	"github.com/schubergphilis/inat/cli/sources"
	"github.com/schubergphilis/inat/cli/status"
	synccmd "github.com/schubergphilis/inat/cli/sync"
	"github.com/schubergphilis/inat/cli/update"
	validatecmd "github.com/schubergphilis/inat/cli/validate"
	"github.com/schubergphilis/inat/git/source"
)

// version is the binary's reported version. The default ("dev") is
// shown for `go run .` and unflagged builds. Release builds override
// it via:  go build -ldflags "-X main.version=$(cat ../VERSION)" .
var version = "dev"

func main() {
	source.RegisterHTTPTransport()
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "inat",
		Short:         "AI prompt and rule kit manager.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Best-effort cleanup of <binary>.old left over from a prior
		// `inat self update` (Windows can't delete the running exe; the
		// leftover lingers until the next inat invocation). See spec
		// 0015 §4.3.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			selfcmd.CleanupOldBinary()
			return scopecmd.CheckUserAllowed(cmd)
		},
	}
	scopecmd.AddUserFlag(root)
	root.AddCommand(
		newInitCmd(),
		newSourcesCmd(),
		newAgentsCmd(),
		newInstallCmd(),
		newUpdateCmd(),
		newRemoveCmd(),
		newRefreshCmd(),
		rename.NewRenameCmd(),
		replace.NewReplaceCmd(),
		newStatusCmd(),
		synccmd.NewSyncCmd(),
		newValidateCmd(),
		selfcmd.NewSelfCmd(version),
	)
	return root
}

func newInitCmd() *cobra.Command {
	var agent string
	var kitsRepo bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new inat project in the current directory.",
		RunE: func(_ *cobra.Command, _ []string) error {
			return initcmd.RunInit(agent, kitsRepo)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Initial agent profile to configure.")
	cmd.Flags().BoolVar(&kitsRepo, "kits-repo", false, "Mark this project as a kit-authoring repository.")
	return cmd
}

func newSourcesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sources", Short: "Manage source repositories."}

	var addName, addPath string
	addCmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Register a new source repository.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sources.RunSourcesAdd(args[0], addName, addPath, scopecmd.IsUser(cmd))
		},
	}
	addCmd.Flags().StringVar(&addName, "name", "", "Alias for the source.")
	addCmd.Flags().StringVar(&addPath, "path", "", "Subpath inside the repo where registry.json and kits/ live.")

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a source repository.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sources.RunSourcesRemove(args[0], scopecmd.IsUser(cmd))
		},
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured sources.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sources.RunSourcesList(scopecmd.IsUser(cmd))
		},
	}
	cmd.AddCommand(addCmd, removeCmd, listCmd)
	return cmd
}

func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agents", Short: "Manage agent profiles."}

	var addGlobal bool
	addCmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an agent profile.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return agentscmd.RunAgentsAdd(args[0], addGlobal, scopecmd.IsUser(cmd))
		},
	}
	addCmd.Flags().BoolVar(&addGlobal, "global", false, "Write to ~/.inat/config.json instead of the project.")

	var rmGlobal bool
	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an agent profile.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return agentscmd.RunAgentsRemove(args[0], rmGlobal, scopecmd.IsUser(cmd))
		},
	}
	removeCmd.Flags().BoolVar(&rmGlobal, "global", false, "Remove from ~/.inat/config.json instead of the project.")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List active agent profiles.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return agentscmd.RunAgentsList(scopecmd.IsUser(cmd))
		},
	}
	cmd.AddCommand(addCmd, removeCmd, listCmd)
	return cmd
}

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install <kit>",
		Short: "Install a kit from a source.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			user := scopecmd.IsUser(cmd)
			return scopecmd.RunWithCollisionWarnings(user, func() error {
				return install.RunInstall(args[0], user)
			})
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var force, planOnly bool
	cmd := &cobra.Command{
		Use:   "update [<kit>]",
		Short: "Update installed kits.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}
			user := scopecmd.IsUser(cmd)
			run := func() error { return update.RunUpdate(ref, force, planOnly, user) }
			if planOnly {
				return run()
			}
			return scopecmd.RunWithCollisionWarnings(user, run)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite locally modified files.")
	cmd.Flags().BoolVar(&planOnly, "plan", false, "Preview planned actions on stdout without applying anything (spec 0020).")
	return cmd
}

func newRemoveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <kit>",
		Short: "Remove an installed kit.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return removecmd.RunRemove(args[0], force, scopecmd.IsUser(cmd))
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite locally modified files.")
	return cmd
}

func newRefreshCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Re-deliver all installed kits from cache.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return refresh.RunRefresh(force, scopecmd.IsUser(cmd))
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite locally modified files.")
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show installed kits and update availability.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return status.RunStatus(scopecmd.IsUser(cmd))
		},
	}
}

func newValidateCmd() *cobra.Command {
	var subpath string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "validate [PATH]",
		Short: "Validate a directory as a well-formed inat kits-publishing repository.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			outcome, err := validatecmd.RunValidate(path, subpath, jsonOut)
			if err != nil {
				return err
			}
			if !outcome.IsOK() {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&subpath, "subpath", "", "Subdirectory where registry.json and kits/ live.")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON output on stdout.")
	return cmd
}
