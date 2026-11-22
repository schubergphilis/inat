// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	"fmt"
	"io"
)

// PrintPlanDetails writes one detail line per planned action to w in
// the plan's returned order. Format per spec 0020 §4.5:
//
//	up-to-date <key>
//	update <key>
//	rename <key> → <newKey>
//	replace <key> → <newKey>
//	needs-attention <key>: <err>
//
// w is typically os.Stdout or an in-memory buffer; write errors are not
// returned because the CLI exit-code authority is the caller.
func PrintPlanDetails(w io.Writer, plan []PlannedAction) {
	for _, a := range plan {
		switch a.Kind {
		case ActionRename, ActionReplace:
			fmt.Fprintf(w, "%s %s → %s\n", a.Kind, a.InstallKey, a.NewKey)
		case ActionNeedsAttention:
			fmt.Fprintf(w, "%s %s: %v\n", a.Kind, a.InstallKey, a.Err)
		default:
			fmt.Fprintf(w, "%s %s\n", a.Kind, a.InstallKey)
		}
	}
}
