// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

package transaction

import (
	"github.com/schubergphilis/inat/core/kit"
	"github.com/schubergphilis/inat/git/source"
)

// ActionKind identifies the variant of a PlannedAction. The zero value
// is ActionUpToDate so that an unpopulated PlannedAction is interpreted
// as "no work required" rather than as an actionable mutation.
type ActionKind int

const (
	ActionUpToDate ActionKind = iota
	ActionUpdate
	ActionReplace
	ActionNeedsAttention
	ActionRename
)

func (k ActionKind) String() string {
	switch k {
	case ActionUpToDate:
		return "up-to-date"
	case ActionUpdate:
		return "update"
	case ActionReplace:
		return "replace"
	case ActionNeedsAttention:
		return "needs-attention"
	case ActionRename:
		return "rename"
	default:
		return "unknown"
	}
}

// TargetKind identifies the variant of a Target.
type TargetKind int

const (
	TargetUpdate TargetKind = iota
	TargetReplace
)

func (k TargetKind) String() string {
	switch k {
	case TargetUpdate:
		return "update"
	case TargetReplace:
		return "replace"
	default:
		return "unknown"
	}
}

// Target is the input to Plan: which kit to operate on, and what kind
// of operation. The caller (cli/update or cli/replace) constructs a
// slice of Targets and hands it to Plan.
type Target struct {
	Kind       TargetKind
	InstallKey string // "<src>/<kit>" — required for both kinds
	NewRef     string // Replace only: "<newSrc>/<newKit>"; ignored for TargetUpdate
}

// PlannedAction is the per-target output of Plan. Variant fields are
// populated based on Kind; the apply phase switches on Kind. See spec
// 0017 §4.3 for the variant-field matrix.
type PlannedAction struct {
	Kind       ActionKind
	InstallKey string // identifies the kit; always set
	NewKey     string // ActionReplace only (oldKey is InstallKey)

	// Update / Replace plan data, populated by Plan:
	NewTreeSHA string
	NewKitCfg  *kit.KitConfig
	NewFiles   []source.FileBlob
	NewNames   []string

	// NeedsAttention only:
	Err error
}

// IsActionable reports whether the apply phase should perform disk
// writes for this action. ActionUpdate, ActionReplace, and ActionRename
// are actionable; ActionUpToDate and ActionNeedsAttention are not.
// ActionRename writes only the always-rule managed-section headers via
// rename.RewriteAlwaysHeaders — no file content is re-delivered.
func (a *PlannedAction) IsActionable() bool {
	return a.Kind == ActionUpdate || a.Kind == ActionReplace || a.Kind == ActionRename
}

// ApplyResult is the per-action output of Apply. Per-kit apply errors
// are captured into Err rather than returned by Apply, so the loop can
// continue across the rest of the batch.
type ApplyResult struct {
	InstallKey string
	Kind       ActionKind
	Err        error
}
