## inat — Azure DevOps HTTPS support via git User-Agent (Plan 0012)

Pairs with [`docs/specs/0012-azure-devops-user-agent.md`](../specs/0012-azure-devops-user-agent.md). Read the spec for the contract; this document covers rollout decisions, file-by-file changes, and verification.

Closes the spike caveat noted in `go/spike/RESULTS.md` §Caveats ("Authentication was not exercised"). Reproduced 2026-04-29 by `inat sources add https://example-org@dev.azure.com/example-org/ExampleProject/_git/ExampleRepo --name ado-kits --path ai-resources` returning `status code: 400` from `git-upload-pack`. Spec + plan landed on branch `docs/0012-azure-devops-user-agent`. Implementation landed on branch `feat/0012-azure-devops-user-agent` (this commit).

---

### 1. Decisions

| Decision | Choice |
|---|---|
| Scope of override | Process-global `client.InstallProtocol` for `https` and `http` schemes. No host-sniffing. |
| User-Agent value | Static `git/2.40.0`. Not configurable; no env var. |
| Where the override lives | New file `go/git/source/transport.go`. Co-located with the auth helper for the same scheme so reviewers find both halves of the HTTP story in one directory. |
| When the override is registered | Twice: once via `init()` of package `source` (so any code path that imports `git/source` gets it for free), once via an explicit call from `cmd/inat/main.go` (defensive — survives a future refactor that drops the implicit import). `sync.Once` makes the double-call free. |
| Inner transport | `http.DefaultTransport`. Inherits proxy / TLS / connection-pool defaults. Tests inject a stub via the constructor. |
| Test strategy | Plain HTTP via `httptest.NewServer` + temporary `client.InstallProtocol("http", …)`. Defer-restore the production registration in each test. No real network calls. |
| CI smoke against Azure DevOps | Manual only. Documented in the MR description. CI does not gain credentials. |
| Failure handling | None. Init-time registration is allowed to panic per CLAUDE.md rule 10. |
| Versioning | Ships as v0.4.1 (patch — bug fix, no behaviour change for existing users). |

---

### 2. Files

#### New
- `go/git/source/transport.go` (~50 lines) — `userAgent` constant, `uaTransport` struct + `RoundTrip` method, `RegisterHTTPTransport()` exported function, package `init()` that calls it.
- `go/git/source/transport_test.go` — table-driven tests:
  - `TestUATransportSetsUserAgent` — direct `RoundTrip` call against a stub `http.RoundTripper` that captures the request; assert UA == `userAgent`.
  - `TestUATransportClonesRequest` — pass an inbound request, assert the inbound `req.Header.Get("User-Agent")` is unchanged afterward (clone semantics).
  - `TestRegisterHTTPTransportIsIdempotent` — call `RegisterHTTPTransport()` twice; assert the registered transport pointer is identical after both calls (via a small reflection helper or by capturing the first registration and re-checking).
  - `TestGoGitCloneSendsGitUserAgent` — end-to-end: spin up `httptest.NewServer` that records `User-Agent` and returns 401 (we don't need a real git protocol response, we just need the first packet); `git.PlainClone` against `srv.URL`; assert the captured UA starts with `git/`.
- `docs/specs/0012-azure-devops-user-agent.md` — the contract.
- `docs/plan/0012-azure-devops-user-agent.md` — this document.

#### Modified
- `go/main.go` — add `source.RegisterHTTPTransport()` as the first call in `main()`, above the cobra wiring at line 31. Defensive belt-and-braces alongside the package `init()`. Note: `main` does not currently import `git/source` directly — the import is transitive through `cli/install`, `cli/update`, `cli/sources`, `cli/status`, `cli/refresh`. Adding a direct import is fine and makes the dependency explicit.
- `CHANGELOG.md` — add entry under `[Unreleased]` per spec §8.
- `README.md` — short note listing Azure DevOps as a supported host.
- `VERSION` — bump to `0.4.1` when the MR is ready to release. (Optional in the implementation MR; can be a separate release MR.)

#### Untouched but relevant
- `go/git/source/source.go` — `CloneOrFetch` and `openAndFetch` keep their current shape. The transport override is a sibling concern; auth (already in `auth.go`) and protocol registration (new `transport.go`) compose without changing the call sites.
- `go/git/source/auth.go` — unchanged. `git credential fill` still resolves the PAT; the transport just makes sure the request gets past Azure DevOps's UA gate so the auth header actually gets evaluated.
- `rust/` — frozen archive; no changes (CLAUDE.md §Repo layout).

---

### 3. Implementation order (TDD-first)

1. **`transport_test.go` — direct unit tests first.** Write `TestUATransportSetsUserAgent` and `TestUATransportClonesRequest` against the not-yet-existing `uaTransport`. Confirm they fail to compile.
2. **`transport.go` — minimal implementation.** Add `userAgent`, `uaTransport`, `RoundTrip`. Tests from step 1 go green.
3. **`TestRegisterHTTPTransportIsIdempotent`.** Add `sync.Once` + `RegisterHTTPTransport`. Test passes.
4. **`TestGoGitCloneSendsGitUserAgent` — end-to-end via httptest.** This is the load-bearing test for the whole spec; it exercises the same code path Azure DevOps will hit in production.
5. **`init()` registration.** Add `init()` calling `RegisterHTTPTransport()` to `transport.go`. Confirm existing `source_test.go` tests still pass (they should — file:// URLs don't go through the HTTP client).
6. **`main.go` defensive call.** Add `source.RegisterHTTPTransport()` to `main`. Compile + smoke locally.
7. **Manual Azure DevOps smoke** (acceptance criterion 1). Run the original failing command against the live Azure DevOps repo with a real PAT. Capture stderr + cache listing for the MR description.
8. **GitLab regression smoke.** `inat install <existing-kit>` against the production source. Confirm no behaviour change.
9. **Docs** — `CHANGELOG.md`, `README.md`. Bump `VERSION` if releasing in the same MR.

---

### 4. Verification

- `go test ./...` green.
- `gofmt -l .` empty; `go vet ./...` clean; `staticcheck ./...` clean.
- Pre-commit hook (`.githooks/pre-commit`) passes (the spec+plan pair satisfies the spec-presence check).
- **Manual Azure DevOps smoke (load-bearing):**
  1. Pre-fix baseline: `inat sources add https://example-org@dev.azure.com/example-org/ExampleProject/_git/ExampleRepo --name ado-kits --path ai-resources` → expect `status code: 400`. Capture stderr.
  2. Apply patch.
  3. Re-run the same command → expect exit 0, `~/.inat/cache/ado-kits/` populated, `.inat/config.json` updated with the new source.
  4. Re-run again to exercise the cached-fetch path → expect exit 0, no warnings.
  5. `inat sources list` → confirm `ado-kits` listed with the correct URL and subpath.
- **GitLab regression smoke:** in a scratch project under `~/.inat/cache/smoke/`, run `inat install <a-known-kit-on-the-existing-source>`; confirm exit 0 and the kit's files land in the project.
- **Empty-UA contamination check** (paranoia): grep the test output for `Go-http-client` or `go-git/`. Should not appear in any captured request from go-git's HTTP client after the override is installed.
- **Local dev shim** (`~/.local/bin/inat`) runs `go -C go/ run .`, so all smoke runs hit the working tree without rebuilding.

---

### 5. Risks

- **`client.InstallProtocol` is process-global.** If a future caller imports `git/source` from a long-lived process (e.g. a test harness running multiple inat invocations in-process), the override persists across runs. Acceptable — every real inat invocation wants this — but a footgun if the package is ever extracted into a library. Documented in the spec §4.2.
- **`req.Clone` allocation cost.** Every HTTP request now allocates a header copy. Negligible for a CLI that makes a handful of requests per invocation. Not a concern.
- **Static User-Agent goes stale.** If Azure DevOps ever tightens the gate to require a UA newer than `2.40.0` (no announcement of this so far in 5+ years), the constant needs bumping. Cheap one-line follow-up. Risk is minimal because the gate so far has only checked the `git/` prefix, not the version.
- **`http.DefaultTransport` mutation by other code.** If a third party modifies `http.DefaultTransport` globally (e.g. a debugging wrapper), the override picks up that mutation. Same risk applies to the standard library. Out of scope.
- **TLS root trust on Windows.** Some Windows users hit corporate root-CA issues with Go's default trust store (Go does not consult the Windows cert store on older releases). Unrelated to this fix, but the symptom can look similar (handshake error). If it surfaces, treat as a separate diagnostic.
- **Test flakiness on Windows.** `httptest.NewServer` on Windows occasionally has port-binding hiccups under parallel `go test -p 4`. If it shows up, the affected test can `t.Parallel()` off; not predicted to need this.
- **`init()` ordering.** Go runs package `init()`s in dependency order. `git/source` does not import any package that uses `client.InstallProtocol`, so its init runs before any HTTP code path. No ordering hazard.

---

### 6. Rollback

The change is contained: revert the MR. There are no migrations, no on-disk-format changes, no new commands. A pre-fix binary continues to work for non-Azure-DevOps hosts; a rolled-back binary loses Azure DevOps support but matches the v0.3.x / v0.4.0 baseline exactly.
