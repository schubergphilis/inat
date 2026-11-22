## inat — Azure DevOps multi-ack workaround for upload-pack POST (Spec 0013)

---

### 1. Overview

Spec [`0012-azure-devops-user-agent`](0012-azure-devops-user-agent.md) addressed the User-Agent gate that Azure DevOps's HTTP frontend imposes on the smart-HTTP discovery (`GET /info/refs?service=git-upload-pack`). Manual smoke against the live target on 2026-04-30 — captured via the `INAT_DEBUG_HTTP=1` instrumentation that 0012 added — proved the UA fix landed correctly: the GET request now returns `200 OK`. The clone still fails, but at the next phase: the upload-pack negotiation (`POST /git-upload-pack`) returns HTTP 400 with

```
X-Tfs-Exception: GitProtocolException
TF401041: The Git protocol sent is not as expected (Clients must support multi-ack.).
```

Azure DevOps gates the upload-pack POST on the client's first `want` pkt-line carrying the `multi_ack` capability. go-git v5.13.2's body advertises only `side-band-64k no-progress` and never adds `multi_ack`, so every Azure DevOps clone via inat 0.4.x dies on the second packet — even after the 0012 UA fix. This is the same upstream interop gap tracked at [go-git/go-git#64](https://github.com/go-git/go-git/issues/64); the documented workaround is to advertise `multi_ack` in the `want` capability list even though go-git does not implement multi_ack response handling. The branch is sound for `git clone` because the initial-fetch path has no `have` lines, so the multi_ack negotiation branch is never entered server-side either.

This spec adds a body-rewrite to the existing `uaTransport.RoundTrip` introduced by 0012: when the request is a POST whose URL path ends `/git-upload-pack`, parse the first 4-hex-char pkt-line prefix, append ` multi_ack` to the `want` line's capability list before its trailing LF, and recompute the prefix. All other requests (GETs, non-upload-pack POSTs, every host that doesn't gate on this) pass through unmodified.

A companion implementation plan lives at `docs/plan/0013-azure-devops-multi-ack.md`.

This spec extends spec [`0012-azure-devops-user-agent.md`](0012-azure-devops-user-agent.md) and finishes the work it scoped. It does not alter the Rust tree under `rust/` (frozen archive); all changes are in `go/`.

---

### 2. Goals

1. `inat sources add` / `install` / `update` / `status` / `refresh` against Azure DevOps HTTPS URLs (`dev.azure.com/<org>/<project>/_git/<repo>` and the legacy `<org>.visualstudio.com/...` form) MUST succeed end-to-end through both phases — the GET discovery (already passing post-0012) AND the POST upload-pack negotiation (newly passing). Verified by `inat sources add` returning exit 0, printing `added source '<alias>' — N kit(s) available`, and populating `~/.inat/cache/<alias>/` with the repo's contents.
2. The fix MUST stay scoped to upload-pack POSTs: only requests whose `Method == POST` and `URL.Path` ends `/git-upload-pack` get the body rewrite. Non-upload-pack POSTs (e.g. `git-receive-pack` if push support ever lands) pass through unmodified.
3. The fix MUST be a no-op when `multi_ack` is already advertised, when the body's first pkt-line is not a `want`, when the prefix is not parseable as a 4-hex-char length, when the prefix exceeds the body length, when the `want` line carries no capabilities, and when the body is too short to parse. Malformed input MUST NOT crash the binary or cause a request to fail that would otherwise have succeeded.
4. Existing supported hosts (GitLab, GitHub, generic HTTPS, SSH, `file://`) MUST keep working. Adding `multi_ack` to the `want` capability list is a no-op against servers that don't gate on it.
5. The implementation MUST add no new Go module dependencies (CLAUDE.md rule 8): cobra + go-git + stdlib remain the entire allowlist. The pkt-line parser is hand-rolled inline in `transport.go`.
6. The patch MUST be testable without a network round-trip. Unit tests MUST cover (a) the production-shape body rewrite, (b) idempotence when the capability is already present, (c) no-op on six distinct malformed inputs, (d) end-to-end through `RoundTrip` with a captured outbound body, (e) scope-correctness via a non-upload-pack POST that passes through unmodified.
7. The `INAT_DEBUG_HTTP` instrumentation introduced by 0012 MUST keep working unchanged. Adding the body rewrite changes only what's sent on the wire; the diagnostic envelope around it is unchanged.

---

### 3. Non-goals

- **Implementing actual `multi_ack` response handling in go-git.** That's an upstream contribution to go-git, not an inat fix. Advertising-without-supporting is sufficient because the initial-clone path has no `have` lines and the server's multi_ack response branch never fires. If a future inat code path issues partial-fetch with `have` lines, this assumption could break — see Risks.
- **Switching to `multi_ack_detailed`.** Azure DevOps accepts both, but `multi_ack` is what its error message names verbatim and is the simpler advertisement. No reason to use the detailed variant.
- **Host-sniffing the URL** to apply the rewrite only to `dev.azure.com` / `visualstudio.com`. The rewrite is a no-op for hosts that don't gate on it; sniffing adds state and a host allowlist that would have to be maintained.
- **Generic capability injection** (a knob to add arbitrary capabilities). YAGNI. If another host appears that requires a different capability, extend the patcher then.
- **Push (`git-receive-pack`) support.** inat is read-only against sources today. The patcher specifically excludes the receive-pack path so push, if ever added, doesn't quietly inherit the rewrite.
- **A new `transport_advanced` package or `git/source/upload_pack.go` split.** The patcher is ~50 lines; `transport.go` is well under the 300-line target after the addition; no split is justified.

---

### 4. Design

#### 4.1 Recap of 0012's transport

`go/git/source/transport.go` already defines `uaTransport`, an `http.RoundTripper` that wraps `http.DefaultTransport`, sets `User-Agent: git/2.40.0` on every outbound request, and is registered process-globally for `https`/`http` schemes via `client.InstallProtocol`. 0013 adds the upload-pack body rewrite on the same code path; no new transport, no new registration site.

#### 4.2 Pkt-line format reminder

The git smart-HTTP protocol frames its requests and responses as pkt-lines: a 4-character ASCII hex length prefix (covering the prefix itself + the payload), followed by the payload. A POST `/git-upload-pack` body looks like

```
0067want 04a669591649fa9a33193bf27525871ea06d079f side-band-64k no-progress\n
0032want 5df64250b8cf7ee770635b77e00ee0c4ec393402\n
…additional `want` lines, no caps…
0000          ← flush packet
0009done\n
```

The capability list is *only* on the first `want` pkt-line; subsequent `want` lines carry just a SHA. Capabilities are space-separated and terminated by LF.

#### 4.3 The patch

`patchWantCapabilities(body []byte, cap string) []byte` rewrites the first pkt-line in place when all of these hold:

- `len(body) >= 4`
- `body[0:4]` parses as a 4-hex-char unsigned int `n`
- `4 <= n <= len(body)`
- `body[4:n]` starts with `want `
- `body[4:n]` is long enough to plausibly hold caps (`len(want) + 40-char-sha + 1-space + 1-cap-char + 1-LF`)
- `cap` is not already present anywhere in the line

Otherwise it returns `body` unchanged.

When all hold, it appends ` cap` (a space then the capability) before the trailing LF, recomputes `n + len(insert)` as the new total length, formats the new prefix with `fmt.Sprintf("%04x", newTotal)`, and concatenates `[newPrefix][newLine][body[n:]]`. The tail of the body — additional `want` lines, the flush, the `done` — is byte-identical to the input.

`isUploadPackPost(req *http.Request)` is a one-line helper: `req.Method == http.MethodPost && req.URL != nil && strings.HasSuffix(req.URL.Path, "/git-upload-pack")`.

In `RoundTrip`, the rewrite happens after UA injection and before delegating to the inner transport:

```go
if isUploadPackPost(cloned) && cloned.Body != nil {
    body, err := io.ReadAll(cloned.Body)
    if err != nil { return nil, err }
    _ = cloned.Body.Close()
    patched := patchWantCapabilities(body, multiAckCapability)
    cloned.Body = io.NopCloser(bytes.NewReader(patched))
    cloned.ContentLength = int64(len(patched))
}
```

`Content-Length` is updated to match the new body length; go-git constructs the request with a known `Content-Length`, and Azure DevOps requires the value to match.

#### 4.4 Constants

`multiAckCapability = "multi_ack"` — the literal capability flag. Lives in `transport.go` next to `userAgent`. Tests reference the package-level identifier so changing the value doesn't require fixture updates.

#### 4.5 Failure surface

The patch never returns an error. Read failures on `cloned.Body` propagate up through `RoundTrip` (via `return nil, err`) — go-git already handles transport errors and the symptom would be identical to the network failure cases it expects. Allocation cost for read-then-rewrap is bounded by body size, which for `git-upload-pack` POSTs is typically ≤1 KiB.

---

### 5. Compatibility

- **Azure DevOps** — both gates now closed. Verified end-to-end against `dev.azure.com/example-org/ExampleProject/_git/ExampleRepo` on 2026-04-30. `inat sources add` succeeds; `~/.inat/cache/ado-kits/` is populated with the repo contents.
- **GitLab / GitHub / generic HTTPS hosts** — the rewrite adds ` multi_ack` to the capability list. These servers either accept the extra capability silently (most common) or ignore unknown capabilities. Tested against the internal GitLab instance (existing `inat-kits` source).
- **SSH and `file://` URLs** — unaffected. The `uaTransport` is registered only for `https`/`http`; SSH and local paths take other go-git code paths.
- **Existing tests in `go/git/source/`** — all use local file paths via `newFixture`; they don't exercise the HTTP path and are unaffected.
- **Non-upload-pack POSTs** — explicitly bypassed by `isUploadPackPost`. A push (which inat doesn't issue) or any other non-clone POST passes through unmodified.

---

### 6. Acceptance criteria

1. **Azure DevOps end-to-end clone** — `inat sources add https://<org>@dev.azure.com/<org>/<project>/_git/<repo> --name <alias>` against the production target with a valid PAT in Windows Credential Manager returns exit 0 and prints `added source '<alias>' — <N> kit(s) available`. `~/.inat/cache/<alias>/` is populated.
2. **Azure DevOps fetch idempotency** — running the same command (or `inat update`) a second time hits the cache, runs a fast-forward fetch, and exits 0 without 400.
3. **GitLab regression** — `inat install` against a kit on the existing `inat-kits` source on the internal GitLab instance keeps working post-fix.
4. **Production-body rewrite** — `TestPatchWantCapabilitiesAddsMultiAckToProductionBody` confirms the captured 2026-04-30 wire body is rewritten so the first `want` pkt-line ends with `... no-progress multi_ack\n` and its 4-hex-char prefix advances from `004c` to `0056`. The tail of the body (subsequent `want`s + flush + done) is byte-identical to the input.
5. **Idempotent on already-present capability** — `TestPatchWantCapabilitiesNoOpIfAlreadyPresent` confirms a body that already advertises `multi_ack` is returned unchanged.
6. **Safe on malformed input** — `TestPatchWantCapabilitiesNoOpForMalformedBody` covers six sub-cases: empty body, body shorter than the prefix, non-hex prefix, prefix-larger-than-body, non-`want` first pkt-line, `want` line without capabilities. All return body unchanged; no panic.
7. **End-to-end `RoundTrip` rewrite** — `TestRoundTripPatchesUploadPackPost` POSTs the production body to a `/git-upload-pack` URL through the real `uaTransport`; the captured outbound body contains `multi_ack` and `Content-Length` matches the new body length.
8. **Non-upload-pack POSTs untouched** — `TestRoundTripDoesNotPatchNonUploadPackPost` POSTs the same body to a `/git-receive-pack` URL; the outbound body does not contain `multi_ack` (the User-Agent injection still applies).
9. **Pre-commit gate clean** — `gofmt -l .` empty, `go vet ./...` clean, `staticcheck ./...` clean, `go test ./...` green on the implementation MR.
10. **No new go.mod requirements** — `go.mod` and `go.sum` are unchanged at the direct-dependency level.

---

### 7. Testing notes

- **Production body fixture.** The captured-from-the-wire body lives in `transport_test.go` as a `productionWantBody` constant (the exact bytes printed by `INAT_DEBUG_HTTP=1` against `dev.azure.com/example-org/ExampleProject/_git/ExampleRepo` on 2026-04-30, with the trailing `done\n`). Using the real shape catches off-by-one errors in the prefix arithmetic that synthetic fixtures might mask.
- **Manual smoke against real Azure DevOps** is the load-bearing test for criterion 1 and is performed by the author against an internal Azure DevOps kits repo on the local Windows workstation. CI does not have credentials.
- **Diagnostic logging** (`INAT_DEBUG_HTTP=1`) is the bridge: when something looks broken on a new host, set the env var, run the failing command, paste both the request and 4xx response body into the bug report.

---

### 8. Documentation

- `CHANGELOG.md` — the existing `[Unreleased]` Azure DevOps entry (from 0012) is rewritten to describe both gates accurately. The 0012-only narrative ("inat now sets a `git/<version>` User-Agent") is replaced with a two-bullet description: (1) UA gate, (2) multi-ack gate, and how the same transport closes both. v0.4.1 will ship the combined fix; the changelog entry should reflect the user-visible reality at that release.
- `README.md` — the §Commands "Source hosts" paragraph already mentions Azure DevOps. Add a one-line note pointing at `INAT_DEBUG_HTTP` for diagnosing similar HTTP-layer issues against future hosts.
- No CLAUDE.md change. The development contract is unchanged.

---

### 9. Out-of-scope follow-ups

- **Upstream go-git PR adding `multi_ack` advertisement natively.** Right place to fix this is in go-git, not in inat. Out of scope; would also remove the need for our body-rewrite if it ever lands.
- **Partial-fetch / shallow-clone support.** If inat ever issues uploads with `have` lines, the multi_ack-without-handling assumption breaks and we'd have to either implement multi_ack response parsing or shell out to `git`. Not on any current roadmap.
- **Retry / fallback on 400.** Speculative. The current change makes the symptom go away for the only known offender; adding a fallback path would just be dead code until another such host appears.
