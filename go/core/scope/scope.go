// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package scope names where an inat operation applies: a project
// (`<cwd>/.inat/config.json`) or the user (`~/.inat/config.json`,
// spec 0025). Leaf package so agents and config can share it.
package scope

// Scope is the install scope. The zero value is Project, so configs and
// call sites that predate spec 0025 keep project behaviour.
type Scope int

const (
	Project Scope = iota
	User
)

// String returns "project" or "user".
func (s Scope) String() string {
	if s == User {
		return "user"
	}
	return "project"
}
