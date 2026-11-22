// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package naming validates inat skill, rule, and kit names.
//
// Mirrors the Rust implementation's `src/core/naming.rs`. Same regex, same failure modes,
// same error messages.
package naming

import (
	"github.com/schubergphilis/inat/errs"
)

// Maximum allowed name length, matching Rust's constraint.
const maxLen = 64

// ValidateName checks a name against inat's naming constraints:
//
//	[a-z0-9]([a-z0-9-]*[a-z0-9])?
//
// max 64 characters, no consecutive hyphens, no leading or trailing hyphen,
// only lowercase ASCII alphanumerics and hyphens.
//
// Returns nil on success, an *errs.Error with Kind=InvalidName on failure.
func ValidateName(name string) error {
	if len(name) == 0 {
		return errs.NewInvalidName(name, "name must not be empty")
	}
	if len(name) > maxLen {
		return errs.NewInvalidName(name, "name must be at most 64 characters")
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return errs.NewInvalidName(name, "name must not start or end with a hyphen")
	}
	for i := 0; i+1 < len(name); i++ {
		if name[i] == '-' && name[i+1] == '-' {
			return errs.NewInvalidName(name, "name must not contain consecutive hyphens")
		}
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if !isLowerAlphaNumOrHyphen(b) {
			return errs.NewInvalidName(name,
				"name must contain only lowercase alphanumeric characters and hyphens")
		}
	}
	return nil
}

func isLowerAlphaNumOrHyphen(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-'
}
