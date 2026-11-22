// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package scopecmd wires the `--user` flag (spec 0025): which commands
// accept it, and which directory a command runs against.
package scopecmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/schubergphilis/inat/core/config"
	"github.com/schubergphilis/inat/errs"
)

// UserFlag is the root-level persistent flag selecting the user scope.
const UserFlag = "user"

// noUserCommands are the top-level commands that have no user scope
// (spec 0025 §4). Their subcommands inherit the restriction.
var noUserCommands = map[string]bool{"init": true, "validate": true, "self": true}

// AddUserFlag registers `--user` as a persistent flag on root.
func AddUserFlag(root *cobra.Command) {
	root.PersistentFlags().Bool(UserFlag, false,
		"Operate on the user scope (~/.inat/config.json, delivered to the agent's user-level targets).")
}

// IsUser reports whether `--user` was given to cmd. False when the flag
// is not registered.
func IsUser(cmd *cobra.Command) bool {
	v, err := cmd.Flags().GetBool(UserFlag)
	return err == nil && v
}

// CheckUserAllowed rejects `--user` on commands without a user scope.
// Meant for the root PersistentPreRunE.
func CheckUserAllowed(cmd *cobra.Command) error {
	if !IsUser(cmd) {
		return nil
	}
	top := cmd
	for top.HasParent() && top.Parent().HasParent() {
		top = top.Parent()
	}
	if noUserCommands[top.Name()] {
		return fmt.Errorf("flag --user is not supported by 'inat %s'", top.Name())
	}
	return nil
}

// ResolveDir returns the scope root a command runs against: the home
// directory for the user scope, else the current directory. A project
// rooted at home is refused, since its config would be the user config
// (spec 0025 §6.4).
func ResolveDir(user bool) (string, error) {
	if user {
		return config.HomeDir()
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", errs.WrapIO(err)
	}
	if config.IsUserRoot(dir) {
		return "", errs.NewHomeAsProject(dir)
	}
	return dir, nil
}
