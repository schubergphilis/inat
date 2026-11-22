## inat — Azure DevOps HTTPS support via git User-Agent (Spec 0012)

---

### 1. Overview

`inat sources add` against an Azure DevOps HTTPS URL fails with `git operation failed: unexpected client error: unexpected requesting "<url>/git-upload-pack" status code: 400` before authentication is evaluated. Reproduced 2026-04-29 against `https://example-org@dev.azure.com/example-org/ExampleProject/_git/ExampleRepo` with a valid PAT in Windows Credential Manager.

The trigger is Azure DevOps's HTTP frontend, which gates `info/refs?service=git-upload-pack` on the `User-Agent` request header — only requests whose UA starts with `git/` are accepted; anything else gets HTTP 400 at the smart-HTTP handshake before the PAT is even read. go-git ships its own HTTP client whose default UA is `go-git/<ver>`, so every clone/fetch from inat against an Azure DevOps repo dies at the first packet.

This is a known go-git interop gap — tracked upstream as [go-git/go-git#64](https://github.com/go-git/go-git/issues/64) and several follow-ups — and was explicitly flagged as the largest open question in the go-git spike at `go/spike/RESULTS.md` line 62 ("Authentication was not exercised … real port needs to evaluate go-git's transport"). The spike validated GitLab + local file URLs only; Azure DevOps was never tested.

The fix is a small process-global override: register a custom HTTP transport with go-git that sets `User-Agent: git/<version>` on every outbound request, while delegating the rest of the round-trip (TLS, proxy, redirects) to `http.DefaultTransport`. Credentials still flow through the existing `git credential fill` path; nothing about auth changes.

A companion implementation plan lives at `docs/plan/0012-azure-devops-user-agent.md`.

This spec extends spec [`0001-initial-idea.md`](0001-initial-idea.md) and closes the spike caveat noted in `go/spike/RESULTS.md` §Caveats. It does not alter the Rust tree under `rust/` (frozen archive); all changes are in `go/`.

---

### 2. Goals

1. `inat sources add`, `install`, `update`, `status`, and `refresh` MUST succeed against Azure DevOps HTTPS URLs (`dev.azure.com/<org>/<project>/_git/<repo>` and the legacy `<org>.visualstudio.com/...` form) when valid credentials are available via `git credential fill`.
2. The fix MUST also keep working against existing supported hosts (GitLab, GitHub, generic HTTPS, SSH, and `file://` paths). No regression in already-passing scenarios.
3. The User-Agent override MUST be installed exactly once per process, idempotent on repeat calls. Re-imports / repeated invocations during the same `inat` run MUST NOT re-register the transport.
4. The transport MUST delegate proxy resolution, TLS verification, and connection pooling to `http.DefaultTransport`. Users behind corporate proxies relying on `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY` MUST keep working without configuration.
5. The implementation MUST add no new Go module dependencies (CLAUDE.md rule 8): cobra + go-git + stdlib remain the entire allowlist. The fix is reachable through `go-git/v5/plumbing/transport/client` and `go-git/v5/plumbing/transport/http`, both already pulled in transitively.
6. The User-Agent value MUST start with literal `git/` followed by a SemVer-shaped version. The version string need not match a real `git` binary on the host — Azure DevOps only inspects the prefix — but the value MUST be a stable constant in the binary so reproductions and bug reports can identify the inat build that emitted it.
7. The override MUST be testable without a network round-trip: a unit test using `net/http/httptest` MUST confirm that a clone attempt against a stub server receives a request whose `User-Agent` starts with `git/`.

---

### 3. Non-goals

- **Per-request UA customisation** (env var, flag, config knob). One static UA is enough to satisfy Azure DevOps; configurability is YAGNI and adds a permanent API surface for a transport-layer detail. Revisit only if a host emerges that rejects `git/2.40.0` specifically.
- **Tracking the host's git version**. Resolving the actual `git --version` on PATH and forwarding it would couple inat's HTTP behaviour to an external binary; the static value is simpler and sufficient.
- **Proactive Azure DevOps detection** (sniff the URL host and only inject UA for `dev.azure.com` / `visualstudio.com`). Setting `User-Agent: git/<ver>` on every HTTPS request is a no-op for hosts that don't care, and avoiding host-sniffing keeps the transport stateless.
- **Custom TLS configuration** (CA bundle override, mTLS, skip-verify). Out of scope; users on bespoke TLS setups already have to configure `SSL_CERT_FILE` / system trust stores and can do so for inat the same way they do for `git`.
- **HTTP/2 tuning, keep-alive overrides, retry policies**. Out of scope — `http.DefaultTransport`'s defaults are fine.
- **Switching from go-git's smart-HTTP to shelling out to `git`**. A separate, much larger discussion that the spike already considered and rejected; not reopened by this fix.
- **Caching the negotiated upload-pack response**. Performance work, not correctness.

---

### 4. Design

#### 4.1 Transport wrapper

A new `RoundTripper` in `go/git/source/transport.go` wraps `http.DefaultTransport` and sets `User-Agent` on every outbound request:

```go
const userAgent = "git/2.40.0"

type uaTransport struct {
    inner http.RoundTripper
}

func (t *uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    cloned := req.Clone(req.Context())
    cloned.Header.Set("User-Agent", userAgent)
    return t.inner.RoundTrip(cloned)
}
```

`req.Clone` is required: `RoundTripper` contracts forbid mutating the inbound request. The inner transport defaults to `http.DefaultTransport` and is overridable via a constructor parameter so tests can inject a stub.

#### 4.2 Registration

`go-git` exposes `client.InstallProtocol(scheme, transport)` to override the HTTP client used by smart-HTTP. The override is process-global. inat installs it for both `https` and `http` schemes:

```go
var registerOnce sync.Once

func RegisterHTTPTransport() {
    registerOnce.Do(func() {
        c := githttp.NewClient(&http.Client{
            Transport: &uaTransport{inner: http.DefaultTransport},
        })
        client.InstallProtocol("https", c)
        client.InstallProtocol("http", c)
    })
}
```

`sync.Once` guarantees a single registration even if the function is called from `init()` of multiple packages or from tests. `RegisterHTTPTransport` is exported so callers (notably `cmd/inat/main.go`) can wire it explicitly; package `source` also calls it from its own `init()` so any code path that imports `git/source` gets the override automatically — there is no inat call site that uses `go-git` HTTP without going through `git/source`.

#### 4.3 Version constant

`userAgent = "git/2.40.0"` is the chosen value. Rationale: 2.40 is a recent-but-not-bleeding-edge git release that Azure DevOps and every other widely-deployed host accepts. The constant lives next to the transport in `transport.go` and is referenced by tests via the package-level identifier (no hardcoded duplication in test files).

#### 4.4 Failure surface

If `RegisterHTTPTransport` is called and `client.InstallProtocol` panics (it shouldn't — go-git's implementation is a map assignment under a mutex), the panic propagates. We do not catch it. Init-time panics are explicitly allowed for invariants that cannot fail at runtime (CLAUDE.md rule 10), and a broken go-git protocol registry would render the whole binary unusable anyway.

If a future go-git version changes the registration API, the build breaks at compile time and the failure is local to `transport.go`. No runtime fallback path is needed.

---

### 5. Compatibility

- **GitLab / GitHub / generic HTTPS hosts**: setting `User-Agent: git/2.40.0` is a no-op for hosts that don't gate on it. Verified by manual smoke against the internal GitLab instance (the existing `inat-kits` source) and `github.com`.
- **SSH URLs**: untouched. The transport override only intercepts `http`/`https` scheme; SSH goes through `gossh.NewSSHAgentAuth` per `go/git/source/auth.go:35`.
- **`file://` and local-path URLs**: untouched. go-git's local transport doesn't go through the HTTP client.
- **Existing tests in `go/git/source/`**: all use local file paths via `newFixture`. The transport registration is a no-op for them; tests stay green without modification.
- **Azure DevOps `_git/_full/...` redirect URLs**: Azure DevOps occasionally 302-redirects through `_apis/git/...`. `http.DefaultTransport` follows redirects through the same client, so the UA stays attached on every hop.
- **HTTP (non-TLS) repos**: covered by registering both `http` and `https`. inat does not actively support HTTP transports but registering both costs nothing and avoids a "works in HTTPS, fails in HTTP" footgun for self-hosted setups.

---

### 6. Acceptance criteria

1. **Azure DevOps clone** — `inat sources add https://<org>@dev.azure.com/<org>/<project>/_git/<repo> --name <alias>` against a real Azure DevOps repo with a valid PAT in Windows Credential Manager (or `git credential-manager-core` on macOS/Linux) succeeds and populates `~/.inat/cache/<alias>/`. Reproduction case: the URL from the 2026-04-29 bug report.
2. **Azure DevOps fetch idempotency** — running the same `sources add` (or `inat update`) a second time hits the existing cache and runs a fast-forward fetch without HTTP 400.
3. **GitLab regression** — `inat install` / `inat refresh` / `inat status` against the existing `inat-kits` source on the internal GitLab instance continue to work after the transport is registered.
4. **Unit-level UA assertion** — a test in `go/git/source/transport_test.go` spins up `httptest.NewTLSServer` (or HTTP — see §7), points go-git at it, and asserts the recorded `User-Agent` header matches `^git/\d+\.\d+\.\d+$` and is exactly `userAgent`.
5. **Idempotent registration** — calling `RegisterHTTPTransport()` twice from a test (or once from `init()` and once from `main()`) does not re-register; verified by counting calls to a stub installer in a test, or — simpler — by checking the registered protocol pointer is unchanged after the second call.
6. **Proxy passthrough** — a test sets `HTTPS_PROXY` to a httptest server, points go-git at an unreachable upstream, and asserts the request lands on the proxy with the `git/...` UA still attached. (If the test is flaky on Windows due to env-var visibility, skip it on Windows with `t.Skip` and run only on `linux` / `darwin`.)
7. **Pre-commit gate clean** — `gofmt -l .` empty, `go vet ./...` clean, `staticcheck ./...` clean, `go test ./...` green on the implementation MR.
8. **No new go.mod requirements** — `go.mod` after the change has the same direct-dependency list as before; `go.sum` adds no new top-level modules. (`transport/client` and `transport/http` are subpackages of the already-required `go-git/v5`.)

---

### 7. Testing notes

- **TLS in tests is optional.** go-git's `transport/http.NewClient` accepts an `*http.Client` whose `Transport` is the swappable bit. Tests can use plain HTTP via `httptest.NewServer` and `client.InstallProtocol("http", …)`; the UA-injection check works identically. Avoiding TLS in tests sidesteps cert-trust setup and keeps the suite hermetic.
- **Test isolation.** `client.InstallProtocol` mutates a process-global registry. Tests that register a stub transport MUST defer-restore the prior protocol via `client.UnregisterProtocol` (or by re-installing the production registration) to avoid bleeding into sibling tests in the same package run.
- **Smoke against real Azure DevOps** (acceptance criterion 1) is manual, not automated. CI does not have credentials for a real Azure DevOps repo and adding them is out of scope. The MR description MUST include a paste of the manual smoke output (stderr, exit code, resulting cache directory listing) before merge.
- **Reproduction baseline.** Before applying the patch, the author SHOULD record the exact failing command + stderr from the unfixed binary in the MR description, so reviewers can confirm the fix targets the right symptom.

---

### 8. Documentation

- `CHANGELOG.md` entry under `[Unreleased]`: _"`inat` now supports Azure DevOps HTTPS sources. Previously, `sources add` / `install` / `update` against `dev.azure.com` failed with HTTP 400 at the git smart-HTTP handshake; inat now sets a `git/<version>` User-Agent on every HTTP request, matching what Azure DevOps expects."_
- `README.md` — short note in the §Sources or §Installation section listing Azure DevOps alongside GitLab and GitHub as a tested host. No new configuration required.
- No CLAUDE.md change. The fix is internal; the development contract is unchanged.

---

### 9. Out-of-scope follow-ups

These are deliberately deferred and called out so future readers don't think they were missed:

- **Diagnostics on HTTP 400.** A friendlier error message that suggests "your host may require a different User-Agent" when go-git returns `status code: 400` from `git-upload-pack`. Useful but speculative — the current change makes the symptom go away for the only known offender.
- **Telemetry on which hosts inat talks to.** Would help spot the next interop gap before users hit it. Out of scope for an internal CLI; raise as a separate spec if it ever becomes interesting.
- **Authenticated test against Azure DevOps in CI.** Requires a service-principal PAT scoped to a throwaway repo; deferred until we have a clear motivation beyond this single fix.
