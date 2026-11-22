// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package reserved holds inat's reserved rule names. It is a tiny
// leaf package so that both `core/config` and `agents/builtin` can
// reference the constants without introducing an import cycle.
//
// The Rust code keeps these constants in `agents::builtin::mod.rs`. Go
// disallows the resulting cycle (`agents/builtin` would depend on
// `core/delivery`, which depends on `core/config`, which depends on
// `agents/builtin`), so the constants live in a dedicated package here.
package reserved

// BuiltinRuleName is the reserved name of the auto-delivered `inat-usage`
// rule (spec 0005).
const BuiltinRuleName = "inat-usage"

// BuiltinKitsRuleName is the reserved name of the auto-delivered
// `inat-kits` rule (spec 0007), delivered when `kits_repo: true`.
const BuiltinKitsRuleName = "inat-kits"
