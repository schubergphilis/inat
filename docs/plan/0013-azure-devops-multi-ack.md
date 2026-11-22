## inat — Azure DevOps multi-ack workaround for upload-pack POST (Plan 0013)

Pairs with [`docs/specs/0013-azure-devops-multi-ack.md`](../specs/0013-azure-devops-multi-ack.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, and verification.

Closes the second of the two gates Azure DevOps imposes on git smart-HTTP — spec / plan [0012-azure-devops-user-agent](../specs/0012-azure-devops-user-agent.md) closed the User-Agent gate; this one closes the multi_ack capability gate. Discovered 2026-04-30 via the `INAT_DEBUG_HTTP=1` instrumentation that 0012 added: the GET `/info/refs` started returning 200 (UA gate cleared), but POST `/git-upload-pack` 400'd with `X-Tfs-Exception: GitProtocolException` / `TF401041: ... Clients must support multi-ack.`. The captured wire body confirmed go-git's `want` line carried only `side-band-64k no-progress` capabilities, with no `multi_ack`. Spec + plan landed on branch `docs/0013-azure-devops-multi-ack`. Implementation landed on branch `feat/0013-azure-devops-multi-ack` (this commit).

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Locus of the fix | Extend the existing `uaTransport.RoundTrip` from 0012. No new transport, no new registration site, no new package. |
| Trigger condition | `req.Method == http.MethodPost && URL.Path` ends `/git-upload-pack`. Non-upload-pack POSTs pass through unmodified. |
| Patch shape | Read the body, parse the 4-hex-char pkt-line prefix, append ` multi_ack` to the first `want` line's capability list before the trailing LF, recompute the prefix, re-wrap as `io.NopCloser(bytes.NewReader(...))`, update `Content-Length`. |
| Capability value | Static `multi_ack`. Not `multi_ack_detailed`. Azure DevOps's error message names `multi-ack` verbatim; `multi_ack` is what go-git's protocol layer would have emitted if it supported the capability. |
| Robustness | Strict no-op on six distinct malformed input shapes: empty, too-short, non-hex prefix, prefix > len(body), non-`want` first line, `want` with no capabilities. Locked by table-driven test. |
| Scope of advertisement | Whether to advertise on every host (no host-sniffing) — yes. Adding a capability is a no-op for servers that don't gate on it. Sniffing would add state. |
| Test fixture | The captured 2026-04-30 production body shape (the exact bytes go-git sends to `dev.azure.com`) lives as `productionWantBody` in `transport_test.go`. Real-shape > synthetic. |
| Diagnostic surface | The `INAT_DEBUG_HTTP=1` env-var dump that 0012 introduced is unchanged. The patcher operates inside the same `RoundTrip`, so the dump shows the *post-patch* body — useful for verifying the rewrite applied. |
| Versioning | Ships as v0.4.1 alongside 0012's UA fix. Single CHANGELOG `[Unreleased]` entry covers both. |

---

### 2. Files

#### New
- `docs/specs/0013-azure-devops-multi-ack.md` — the contract.
- `docs/plan/0013-azure-devops-multi-ack.md` — this document.

#### Modified
- `go/git/source/transport.go` — adds `multiAckCapability` constant, `isUploadPackPost` predicate, `patchWantCapabilities` pkt-line rewriter, body-read-and-rewrite block in `RoundTrip` between UA injection and delegation to the inner transport. Imports gain `bytes`, `io`, `strconv`, `strings`. Net add: ~70 lines, plus ~50 more for the env-gated `debugRequest` / `debugResponse` helpers (split out of inline RoundTrip blocks for readability — see "Refactor" below). File total ends ~190 lines, well under the 300-line target.
- `go/git/source/transport_test.go` — adds `productionWantBody` constant + 5 tests:
  - `TestPatchWantCapabilitiesAddsMultiAckToProductionBody`
  - `TestPatchWantCapabilitiesNoOpIfAlreadyPresent`
  - `TestPatchWantCapabilitiesNoOpForMalformedBody` (table-driven, 6 sub-cases)
  - `TestRoundTripPatchesUploadPackPost`
  - `TestRoundTripDoesNotPatchNonUploadPackPost`
- `CHANGELOG.md` — replaces the existing 0012-only `[Unreleased]` Azure DevOps entry with one that describes both gates and references both specs. The v0.4.1 release entry that descends from this `[Unreleased]` will be a single coherent narrative, not two stacked entries.
- `README.md` — adds a one-sentence pointer to `INAT_DEBUG_HTTP` in the §Commands "Source hosts" paragraph.
- `docs/plan/0013-azure-devops-multi-ack.md` — this document, with an "Implementation landed via this commit's branch" line added in the feat commit (matches the 0011 pattern; satisfies the pre-commit hook's spec-presence check on a feat commit that touches `go/`).

#### Refactor (not behavioural)
- The `INAT_DEBUG_HTTP` logging in `RoundTrip` was inline before; this MR factors it into `debugRequest` / `debugResponse` helpers next to the patcher. Same env-var contract, same output shape; just easier to read alongside the new body-rewrite logic. Risk: minimal — the helpers are pure functions and covered transitively by every other transport test.

#### Untouched
- `go/main.go` — no changes. The explicit `source.RegisterHTTPTransport()` call from 0012 is sufficient.
- `go/git/source/source.go`, `auth.go` — no changes. The patch is a sibling concern.
- `go.mod` / `go.sum` — no new dependencies.

---

### 3. Implementation order (TDD-first)

1. **Add `productionWantBody` constant + `TestPatchWantCapabilitiesAddsMultiAckToProductionBody` against not-yet-existing `patchWantCapabilities`.** Confirm the test fails to compile.
2. **Implement `patchWantCapabilities`** — pkt-line parser, append + length recompute. Step 1 test goes green.
3. **Add the no-op tests** — `TestPatchWantCapabilitiesNoOpIfAlreadyPresent` and the 6-case `TestPatchWantCapabilitiesNoOpForMalformedBody`. Confirm green; iterate on the parser if any branch surprises.
4. **Add `isUploadPackPost`** + the read-and-rewrite block in `RoundTrip` between UA injection and `t.inner.RoundTrip(cloned)`.
5. **Add `TestRoundTripPatchesUploadPackPost` and `TestRoundTripDoesNotPatchNonUploadPackPost`.** Confirm green.
6. **Refactor `INAT_DEBUG_HTTP` logging into helpers** for readability. Run full suite.
7. **CHANGELOG + README updates.**
8. **Manual Azure DevOps smoke** (acceptance criterion 1). Run the original failing command via the dev shim against an internal Azure DevOps kits repo. Capture `inat sources add … ado-kits …` output (`added source 'ado-kits' — N kit(s) available`) and `ls ~/.inat/cache/ado-kits/` for the MR description.
9. **GitLab regression smoke.** `inat install` against an existing kit on the production source.

---

### 4. Verification

- `go test ./...` green.
- `gofmt -l .` empty; `go vet ./...` clean; `staticcheck ./...` clean.
- Pre-commit hook (`.githooks/pre-commit`) passes.
- **Manual Azure DevOps smoke (load-bearing — already captured 2026-04-30 during development):**
  ```
  $ inat sources add https://example-org@dev.azure.com/example-org/ExampleProject/_git/ExampleRepo \
      --name ado-kits --path ai-resources
  added source 'ado-kits' — 2 kit(s) available
  $ ls ~/.inat/cache/ado-kits/
  Branding  BuildTemplates  CLAUDE.md  Configuration  Data  Docker  Documentation  Jupyter  Scripts  Settings …
  ```
- **GitLab regression smoke** — install + refresh against the existing `inat-kits` source. No regression.
- **Empty-UA + multi_ack contamination check** (paranoia): grep test output for `Go-http-client` or `go-git/`. Should not appear.

---

### 5. Risks

- **Lying about multi_ack support.** go-git does not implement multi_ack response parsing. We advertise the capability anyway because the initial-clone path has no `have` lines — the multi_ack negotiation branch is never entered server-side, and the server treats the connection as a simple `want`-only clone. If a future code path issues an upload-pack with `have` lines (delta fetches, partial clones, refs that diverged after a previous fetch), the server may engage the multi_ack response branch and go-git could misinterpret the response. inat's current code path is initial-clone-then-fast-forward only; the risk is theoretical. Mitigation if it surfaces: switch to advertising `multi_ack_detailed` (which Azure DevOps also accepts and which has the same "advertise but don't implement" workaround) or shell out to `git` for the fetch.
- **Pkt-line parser fragility.** Hand-rolled binary-format parsing has historically caused subtle bugs. Mitigation: strict no-op on six distinct malformed shapes, locked by `TestPatchWantCapabilitiesNoOpForMalformedBody`; production-shape rewrite locked by the captured-from-the-wire fixture.
- **`Content-Length` consistency.** Go's `http.Client` requires `Request.ContentLength` to match the body when the body length is known. We update both. Forgetting to update `ContentLength` would silently send a truncated request. Test `TestRoundTripPatchesUploadPackPost` asserts the equality.
- **Body re-streaming allocation.** Every upload-pack POST now allocates a copy of the body. Bodies are typically ≤1 KiB. Negligible.
- **Ordering with proxies / corporate TLS.** The patch operates on the request body before it reaches `http.DefaultTransport`; proxies see the patched body. No change in proxy behaviour.
- **Future inat features that POST to `/git-upload-pack` with capabilities the server might re-evaluate.** The patcher only adds; it never removes or reorders. A future server that gates against `multi_ack` (rejects clients that advertise it) is theoretical and not in scope.

---

### 6. Rollback

The change is contained: revert the MR. There are no migrations, no on-disk-format changes, no new commands, no schema changes. A pre-fix binary continues to work for non-Azure-DevOps hosts; a rolled-back binary loses Azure DevOps support but matches the v0.4.0 baseline (which lacked even the UA fix from 0012; rolling back this MR alone leaves 0012's UA fix in place — Azure DevOps would 400 again on the POST gate but GETs work).
