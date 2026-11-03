# Repository Guidelines

## Project Structure & Module Organization

Filegate uses Go 1.27.3+, Linux/IPv4, and only the standard library. `cmd/filegate/` contains startup, CLI parsing, build metadata, and tests. `internal/gate/` contains configuration/reload, state, local administration, HTTP handlers, previews, and tests. `internal/logging/` provides colored console output, file rotation and retention. HTML/CSS is embedded in the handler template.
`deploy/` contains the systemd unit and installation/operation guide; keep their paths and permissions aligned with the CLI and README.
`docs/OPERATIONS.md` covers upgrades, stopped backups and isolated recovery. Keep it in the source repository and preserve its rule: deny all Web access while revoking restored codes, then restore the reviewed permanent blacklist path and restart. Never use `unblock 0.0.0.0/0` as recovery cleanup or silently reset state.
`.github/workflows/` contains scoped CI, reusable validation and tag-triggered public releases after successful checks. `scripts/` and `tests/release.sh` handle release guards, packaging and smoke tests; see `docs/RELEASING.md`.

## Build, Test, and Development Commands

- `make build`: produce the static executable `tmp/filegate` with `CGO_ENABLED=0`, `-trimpath`, and `-s -w`; embed version and UTC build time.
- `make build VERSION=v0.1.0`: override the release version.
- `make test`: run all tests with the race detector; requires a C compiler and permission to create local Unix sockets.
- `make test-integration`: opt into real binary/CLI/IPv4/TLS/signal/restart tests; use a non-root account and permit local listeners.
- `make test-browser`: optional Node 20+/Chrome CDP checks; set `CHROME_BIN` for Chromium. No npm packages or runtime dependencies.
- `make test-release`: test release guards, package isolation, native smoke, cross compilation and CI scope in a disposable Git fixture.
- `make dist VERSION=vX.Y.Z RELEASE_ARCH=amd64`: package a clean, tagged revision under `tmp/dist/`; also supports arm64. Does not run release validation or publish.
- `make fuzz FUZZ=FuzzAccessAlias FUZZTIME=10s`: fuzz one gate entry point; defaults are JSONC and 30 seconds.
- `make vet`: run Go’s static checks.
- `make vulncheck`: scan product packages with separately installed `govulncheck` (network required; development tool only).
- `gofmt -w cmd internal`: format Go sources.

Keep caches and temporary files in repository `tmp/`, never `/tmp`. Keep runtime data and binaries untracked.
Scope Go test/vet commands to `./cmd/... ./internal/...` so temporary Go sources are not scanned. See `tests/README.md` for optional suites and unverified browser/deployment cases. Never use real demo data in tests; bound waits and join/clean up children and goroutines. Avoid parallel tests that mutate global slog, signals or environment.
Use short random names under the repository test temp root for fixtures that bind Unix sockets; do not include full test/subtest names in socket paths. Test long names without falling back to system `/tmp` or skipping socket checks.

## CLI & Local Operation

Use bare action commands with double-dash options. Keep `--help` and `--version` as options; reject prefixed actions such as `--serve` and single-dash options:

```bash
./tmp/filegate --config tmp/demo/config.json init
./tmp/filegate --config tmp/demo/config.json check
./tmp/filegate --config tmp/demo/config.json serve
./tmp/filegate --config tmp/demo/config.json code ab --ttl 30m
./tmp/filegate --help
./tmp/filegate --version
```

Choose one action per invocation. Administration uses a restricted Unix socket, never Web management.
Keep `check` read-only and independent of the server: reuse parsing, directory, state and blacklist validation without initializing state, logging, locks or listeners. Allow missing initial state/log directories, preserve file contents/modes/mtimes on success and failure, and document that checks do not prove write access or successful startup/reload. Prefer running it as the service user.
Keep `--help` concise; put command details in `ACTION --help` (also `--help=ACTION`), and references in `--help=config|examples|limits|all`. Help must work without configuration, services or side effects, including root's `serve --help`. Validate all arguments before help output. Use distinct colors for headings, commands, options, arguments, descriptions and shell comments only on terminals, respecting `NO_COLOR` and `TERM=dumb`; keep redirects plain and do not start a pager automatically. Stripping ANSI must preserve the exact text; quoted hashes and URL fragments are not comments.

Production deployments use a dedicated non-login system user, normally `filegate`; development may use the developer account. Keep binaries and configuration administrator-owned, content read-only to the service, and state/logs writable. Blacklist replacement requires write access to its parent directory; prefer `state/blacklist.txt` in deployments. Reject `serve` with effective UID 0 after argument validation and before configuration or side effects; other actions remain available to root. Do not auto-drop privileges or require a specific non-root username.
For systemd changes, verify the unit with `systemd-analyze verify` and distinguish static verification from actual service acceptance. Preserve IPv4/Unix sockets and write access only to deployment state/logs. Reload runs `check` then sends SIGHUP; signal delivery is not application reload acknowledgement. Do not install/enable services or alter real deployment data during repository tests.

## Coding Style & Testing

Use idiomatic Go, `gofmt`, descriptive names, explicit errors, and `*_test.go` files with `Test...` functions. Test CLI validation, concurrent code consumption/reload, directory isolation, expiry, blacklisting, persistence failures, log rotation/retention and credential redaction. Validate arguments before side effects. No coverage threshold is established.

Web UI and application errors use the embedded Chinese/English dictionaries in `internal/gate/language.go`. Keep translations complete, preserve template escaping, and test language negotiation and preference cookies without changing authentication or file contents. CLI and logs remain English.

## Validation Before Handoff and Release

For routine features and fixes, choose tests by impact: add relevant regressions, run affected package tests, and use race detection, vet and build checks as appropriate. Include process tests for startup/CLI/reload/persistence changes, browser tests for login/cookies/UI/CSP changes, and fuzzing for parsers or input validation. Broaden checks for shared security or concurrency changes. Documentation-only changes need accuracy and diff checks. Reuse successful results for unchanged code; a later request to commit alone does not require rerunning them.

Before creating or moving a version tag, or publishing a release, verify the intended revision with `make test`, `make vet`, `make build`, `make test-release`, `make test-integration`, `make test-browser`, and 30 seconds each for all four gate fuzz targets listed in `tests/README.md`. Confirm the target tree is clean and the results apply to it. Report failures or unavailable prerequisites before tagging/releasing; never count skipped checks as passed. Record manual browser/deployment checks separately. After tagging, rebuild and verify version metadata if producing release binaries.
Release infrastructure changes also require `make test-release` and workflow linting. Pin Actions to reviewed full commit SHAs, keep PR/tests read-only and grant write permission only to the publish job. Reviewed new tags publish automatically after validation; do not publish or change existing drafts as a side effect of local tests. Never include runtime data in packages, overwrite published assets or move existing tags. Require a dated CHANGELOG section and native architecture smoke tests; distinguish local validation from actual GitHub runs. Packaging fixture tags are confined to temporary repositories and are not project releases.
Manual release dispatch accepts an existing version tag. Resolve only `refs/tags/` to a full commit, then pin reusable validation, builds and release-note sources to that commit. Keep ordinary CI checkout behavior unchanged, serialize automatic/manual runs by version, and preserve rejection of existing releases/assets. The dispatch workflow must be present on the default branch; local tests do not prove GitHub registration or publication.
Release tarballs contain exactly one regular executable named `filegate` at the archive root. Publish only the two architecture tarballs as Release assets; do not attach `.BUILDINFO`, `LICENSE` or `SHA256SUMS`. Keep checksum verification internal to the Actions bundle and retain the source LICENSE. Keep smoke tests and extraction examples aligned with this layout; published older assets retain their original layout.

Report checks run and material omissions. Finish by checking test children/listeners/profiles are closed or removed; preserve user data, useful reports and build caches unless cleanup is requested.
Keep Chrome startup deadlines separate from CDP command deadlines, reject closed transports promptly and retain bounded, redacted startup diagnostics before issuing test credentials. Never dump protocol messages, captured requests or browser profiles into CI logs.

## Security & State

Read `SECURITY_REVIEW.md` before security changes. Keep client reads/writes outside the state lock; recheck authorization and expiry after reading login bodies. Enforce IPv4-only listeners, clients and blacklists. Reject root, duplicate and nested actual directory mappings at startup/reload. Preserve `os.Root` confinement, `crypto/rand` credentials, HMAC storage, and authorization on every file request. Persist code consumption before issuing sessions. Keep sessions in memory; serialize state writes in one server process. Keep configuration, blacklist, state, logs, and TLS private keys outside the content root. Never log credentials, bodies or query strings. Normalize client metadata to known labels and bounded versions; never dump raw UA/headers. Treat reported timezone/OS/browser data as audit-only. Only generated pages may allow the constant timezone script by its exact CSP hash; keep file responses script-free and auth independent of JavaScript. Link GET/HEAD requests must not consume codes or create sessions; keep preparation bounded and memory-only, store digests, require cookie/IP/CSRF on POST and reuse durable consumption. Keep URL credentials out of confirmation pages and language links; preserve the origin-only referrer policy on `/access`. Keep ANSI colors out of files. Logging changes require restart. Use HTTP only on trusted networks. Preserve connection/request/download limits, single-range checks and rolling write deadlines; keep their fixed values documented in help and README.

SIGHUP reloads mappings and runtime limits. Validate before switching; persist revocations first. Keep old configuration on failure. Deployment paths require restart. Recheck authorization when opening files during reload.

## Commit & Pull Request Guidelines

Use Linux kernel-style subjects, e.g. `cli: Add version reporting`. Explain the problem, change, and validation. Do not add `Signed-off-by`. Keep commits focused; describe compatibility changes in pull requests.

## Documentation Maintenance

Read `SESSION_STATE.md` when resuming development. Before handing off or committing relevant changes, update `README.md` for behavior, configuration, CLI/help and examples; `CHANGELOG.md` under Unreleased for user-visible changes and compatibility; `AGENTS.md` for development rules; and `SESSION_STATE.md` for milestones, decisions, validation and pending work. Update only affected documents. Distinguish proposals from implemented behavior, record skipped checks, and never copy runtime secrets. Record commit references as dated baselines; verify actual HEAD with Git instead of embedding a document’s own commit hash. Keep changelog version boundaries aligned with actual tags; do not invent release versions or dates.
