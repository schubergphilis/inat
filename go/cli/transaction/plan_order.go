// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import "github.com/schubergphilis/inat/core/config"

// orderDonorsFirst returns plan reordered so that a kit giving up a
// name applies before the kit taking it over (spec 0026 §2). Action A
// must precede action B when a name A records today and does not keep
// is in B's NewNames; only actionable entries carry edges.
//
// The sort is a stable Kahn topological sort: at every step the
// earliest remaining entry with no pending donor is emitted, so the
// incoming order survives wherever no edge applies. Entries left in a
// cycle (a swap, spec 0026 §3) are appended in incoming order.
func orderDonorsFirst(cfg *config.Config, plan []PlannedAction) []PlannedAction {
	succ, indegree := donorEdges(cfg, plan)
	emitted := make([]bool, len(plan))
	out := make([]PlannedAction, 0, len(plan))
	for len(out) < len(plan) {
		next := firstReady(indegree, emitted)
		if next < 0 {
			break
		}
		emitted[next] = true
		out = append(out, plan[next])
		for _, j := range succ[next] {
			indegree[j]--
		}
	}
	for i := range plan {
		if !emitted[i] {
			out = append(out, plan[i])
		}
	}
	return out
}

// donorEdges builds the donor → taker graph over plan indexes:
// succ[i] lists the entries that must run after i, indegree[j] counts
// the entries that must run before j.
func donorEdges(cfg *config.Config, plan []PlannedAction) ([][]int, []int) {
	succ := make([][]int, len(plan))
	indegree := make([]int, len(plan))
	for i := range plan {
		given := givenUpNames(cfg, &plan[i])
		if len(given) == 0 {
			continue
		}
		for j := range plan {
			if i != j && plan[j].IsActionable() && takesAny(plan[j].NewNames, given) {
				succ[i] = append(succ[i], j)
				indegree[j]++
			}
		}
	}
	return succ, indegree
}

// givenUpNames returns the names a's kit records today and its planned
// state drops. Non-actionable entries give up nothing.
func givenUpNames(cfg *config.Config, a *PlannedAction) map[string]bool {
	if !a.IsActionable() {
		return nil
	}
	given := make(map[string]bool)
	for _, n := range cfg.Installed[a.InstallKey].Names {
		given[n] = true
	}
	for _, n := range a.NewNames {
		delete(given, n)
	}
	return given
}

func takesAny(names []string, given map[string]bool) bool {
	for _, n := range names {
		if given[n] {
			return true
		}
	}
	return false
}

func firstReady(indegree []int, emitted []bool) int {
	for i, d := range indegree {
		if d == 0 && !emitted[i] {
			return i
		}
	}
	return -1
}
