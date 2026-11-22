// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package validatecmd implements `inat validate [PATH] [--subpath DIR] [--json]`.
//
// Mirrors the Rust implementation's `src/cli/validate.rs`. Returns the issue count so the
// top-level main can exit with a non-zero status when issues are found.
package validatecmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/schubergphilis/inat/core/validate"
	"github.com/schubergphilis/inat/errs"
)

// Outcome carries the issue count from a single validate invocation.
//
// IssueCount is the count of error-severity issues — the value the
// top-level main uses to decide the exit code. WarningCount is the
// count of advisory issues (e.g. rules without a description); these
// never affect exit status.
type Outcome struct {
	IssueCount   int
	WarningCount int
}

// IsOK reports whether the validation pass is clean (no errors).
// Warnings do not flip this to false.
func (o Outcome) IsOK() bool { return o.IssueCount == 0 }

// RunValidate executes a structural validation and prints the result.
//
// path == "" means "current working directory". subpath may be "" for
// repo-root layouts. When jsonOut is true, the report is printed as
// pretty-printed JSON on stdout; otherwise human-readable lines go to
// stderr followed by a summary.
func RunValidate(path, subpath string, jsonOut bool) (Outcome, error) {
	root, err := resolveRoot(path)
	if err != nil {
		return Outcome{}, err
	}
	issues := validate.Run(root, subpath)
	kitCount := validate.CountKits(root, subpath)
	if kitCount < 0 {
		kitCount = 0
	}
	errors, warnings := splitBySeverity(issues)
	if jsonOut {
		if err := printJSON(root, errors, warnings); err != nil {
			return Outcome{}, err
		}
	} else {
		printHuman(errors, warnings, kitCount)
	}
	return Outcome{IssueCount: len(errors), WarningCount: len(warnings)}, nil
}

func splitBySeverity(issues []validate.Issue) (errors, warnings []validate.Issue) {
	for _, i := range issues {
		if i.IsError() {
			errors = append(errors, i)
		} else {
			warnings = append(warnings, i)
		}
	}
	return errors, warnings
}

func resolveRoot(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", errs.WrapIO(err)
	}
	return dir, nil
}

func printHuman(errors, warnings []validate.Issue, kitCount int) {
	for _, issue := range errors {
		fmt.Fprintf(os.Stderr, "error: %s: %s: %s\n", issue.Kind, issue.Path, issue.Message)
	}
	for _, issue := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s: %s: %s\n", issue.Kind, issue.Path, issue.Message)
	}
	fmt.Fprintf(os.Stderr,
		"validated %d kit(s), %d error(s), %d warning(s)\n",
		kitCount, len(errors), len(warnings))
}

func printJSON(root string, errors, warnings []validate.Issue) error {
	canonical, err := filepath.Abs(root)
	if err != nil {
		canonical = root
	}
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}
	if errors == nil {
		errors = []validate.Issue{}
	}
	if warnings == nil {
		warnings = []validate.Issue{}
	}
	payload := map[string]any{
		"path":     canonical,
		"issues":   errors,
		"warnings": warnings,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return errs.WrapJSON(err)
	}
	fmt.Println(string(data))
	return nil
}
