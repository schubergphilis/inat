## inat — Apply a name's donor before its taker (Spec 0026)

---

### 1. Overview

Spec [0017](0017-transactional-update.md) promised that a skill moving from one installed kit to another in the same batch updates cleanly (§7 acceptance 6), and fixed the plan-phase half of that with the batch-wide uniqueness check (§4.4 step 4). The apply phase still runs actions in target order, and `update` builds its targets from the `cfg.Installed` map, so the order is random. When the kit taking the name applies first, it overwrites the shared file; the donor's remove step then sees a hash mismatch and the drift abort from plan [0022](../plan/0022-update-drift-abort.md) stops the donor (`update aborted: … locally-modified file(s)`). With `--force` the donor deletes the file the taker just delivered. `TestUpdateCrossKitMigration` fails about 3 runs in 20 on this.

This spec amends 0017 §4.4 step 5 so `Plan` returns the actions in an order that `Apply` can run safely, and makes `update`'s target order deterministic.

A companion implementation plan lives at [`docs/plan/0026-update-cross-kit-order.md`](../plan/0026-update-cross-kit-order.md).

---

### 2. Goals

1. **Donor before taker.** `Plan` MUST return its actions so that, for every pair of actionable entries A and B, A comes before B when a name A records today and does not keep (`cfg.Installed[A].Names − A.NewNames`) is in B's `NewNames`. Non-actionable entries (`UpToDate`, `NeedsAttention`) add no ordering constraints.
2. **Stable otherwise.** Where goal 1 imposes no constraint, `Plan` MUST keep the input order. This replaces 0017 §4.4 step 5's "in input order": `Plan` still emits exactly one action per input `Target`.
3. **Deterministic targets.** `update` without a kit argument MUST build its targets in sorted install-key order, so the same project produces the same plan, `--plan` output and apply order on every run.
4. **`--plan` shows the real order.** Because the ordering happens inside `Plan`, spec [0020](0020-update-plan-dry-run.md)'s "lines in the order `transaction.Plan` returned them" now also means "in apply order". No printer change.
5. **Applies to `replace` too.** `replace` goes through the same `Plan`; goal 1 holds for it unchanged.

---

### 3. Non-goals

- **Swaps.** kit-a gives `x` to kit-b while kit-b gives `y` to kit-a in the same batch. No apply order satisfies both; the constraints form a cycle, the entries in it keep their input order, and the second kit aborts as locally modified as it does today. Fixing it needs a split remove-all-then-deliver-all apply, which breaks 0017's per-kit atomicity and 0021's per-kit snapshot. Deferred until someone hits it.
- **Changing the drift check.** Plan 0022's abort stays as is; the fix is that it no longer sees another kit's file.
- **Plan-time drift detection** (`update --plan` flagging locally-modified files). Separate, still deferred.

---

### 4. Acceptance

1. A two-kit batch migrating a skill from kit-a to kit-b succeeds without `--force` with the targets in either order, and `.claude/skills/<skill>/SKILL.md` holds kit-b's content.
2. The same holds for a migration from kit-b to kit-a, where sorted targets put the taker first.
3. A batch with no migrations returns its actions in input order (unchanged behaviour).
4. A swap returns its entries in input order and does not loop or drop actions.
5. `go test ./cli/update/ -run TestUpdateCrossKit -count=50` passes.
6. No on-disk schema change: `.inat/config.json` keys and fields are unchanged.
