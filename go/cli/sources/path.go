// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package sources

import (
	"fmt"
	"strings"

	"github.com/schubergphilis/inat/errs"
)

// NormalizeSourcePath validates and normalises an optional source subpath.
// Mirrors the Rust implementation's `src/cli/sources/path.rs::normalize_source_path`.
//
// Returns ("", nil) when raw is empty (no subpath requested). Returns
// *errs.Error (Kind=KitValidation) for absolute paths, backslashes, or
// `.`/`..` segments.
func NormalizeSourcePath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", errs.NewKitValidation("source --path must not be empty")
	}
	if strings.HasPrefix(raw, "/") {
		return "", errs.NewKitValidation(fmt.Sprintf("source --path must be relative, got '%s'", raw))
	}
	if strings.Contains(raw, "\\") {
		return "", errs.NewKitValidation("source --path must use forward slashes")
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." || seg == "." {
			return "", errs.NewKitValidation(
				fmt.Sprintf("source --path must not contain '.' or '..' segments: '%s'", raw))
		}
	}
	return trimmed, nil
}
