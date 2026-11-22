// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Schuberg Philis

// Package transaction is the plan/apply primitive that backs the
// `inat update` and `inat replace` commands.
//
// Operations on installed kits proceed in two phases:
//
//   - Plan — for every target, fetch the source repo, parse the
//     registry, fetch the new kit tree, walk + parse + derive names,
//     and validate name uniqueness across the post-update batch.
//     No disk writes; cfg is not mutated.
//   - Apply — for every actionable plan entry, run the existing
//     remove + deliver sequence and mutate cfg.Installed in memory.
//     Per-kit apply errors are captured into the result, not returned.
//
// The contract is per-kit atomic with skip-and-continue at the batch
// level: a single failing kit does not block the others. Disk-write
// failures during apply remain a residual risk recovered via
// `inat refresh` — see spec 0017 §3.
//
// See `docs/specs/0017-transactional-update.md` for the contract and
// `docs/plan/0017-transactional-update.md` for the rollout.
package transaction
