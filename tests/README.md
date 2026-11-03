# Optional integration and browser checks

The application and Go tests use only the Go standard library. Browser checks
use Node's built-in modules and a locally installed Chrome/Chromium executable;
there is no npm install step or browser download during tests.

## Run the suites

Run from the repository root on Linux as a non-root user:

```bash
make test              # race detector; includes Unix socket tests
make vet
make test-integration  # build a temporary binary; real processes and IPv4/TLS
make test-browser      # build tmp/filegate; Node 20+ and google-chrome
make test-release      # isolated Git fixture; release guards and package checks
CHROME_BIN=/usr/bin/chromium make test-browser
make fuzz FUZZ=FuzzAccessAlias FUZZTIME=10s
```

`make test` runs fuzz seeds but skips the two real-process tests unless
`FILEGATE_INTEGRATION=1` is set. `make test-integration` enables them explicitly.
The process tests skip when run as root; use a normal development account.
All socket/port tests require an environment that permits local listeners.
Gate fixtures use short random directory names under the configured `TMPDIR`,
without embedding test names: Linux pathname Unix sockets allow at most 107
pathname bytes. Keep checkout/temp roots reasonably short; a `bind: invalid
argument` with an overlong `admin.sock` path is a path-length failure, even when
the preceding authentication/persistence logs are expected test scenarios.
Test services bind only to loopback IPv4 and use generated temporary data.
They do not use `tmp/demo` or an existing service's configuration.

For systemd deployment, run `systemd-analyze verify` after installing the binary
and unit, then follow the target-host acceptance checklist in
[deploy/README.md](../deploy/README.md#验证范围). Static unit verification and
the application process suite do not validate systemd namespace restrictions.
Repository tests must not install, enable or modify a real system service.

The process suite checks CLI administration, Chinese files, valid and rejected
SIGHUP reloads, SIGTERM/SIGINT, persistent consumption and unblocking, restart,
second instances, occupied ports, unavailable logging, invalid certificates,
and TLS/HTTP2 using certificate files and a test trust store.
It also runs `check` before startup, while the server holds its locks and after
TLS configuration, and checks exit status for invalid deployment files. The
regular CLI suite snapshots file contents, modes and modification times on
successful and failed checks, including missing initial state/log directories,
Chinese mappings, symlinks, FIFOs, unreadable files and invalid state/TLS pairs.

GitHub Actions runs scoped CI and a reusable full validation workflow before
tagged or manually dispatched public releases. Packaging regressions create tags only inside temporary
Git fixtures; they do not tag, push or publish this repository. See
[docs/RELEASING.md](../docs/RELEASING.md) for permissions, architecture smoke
coverage, checksums, artifact retention and the remaining cloud-run validation.
Archive regressions require exactly one root entry, `filegate`, for both
architectures. Local packaging must produce only the two architecture
tarballs, with no BUILDINFO, LICENSE or SHA256SUMS sidecars. The workflow
keeps checksums in its internal Actions bundle, without publishing them.

## Choosing validation scope

For ordinary feature work, Codex chooses checks based on the affected behavior.
Run relevant regression and package tests; add race, vet and build checks where
applicable. Startup/CLI/reload/persistence changes call for process tests;
login/cookie/UI/CSP changes call for browser tests; input parsing changes call
for the relevant fuzz target. Shared security or concurrency changes justify a
broader regression run. Documentation-only edits need accuracy and diff checks.
Previously successful checks remain valid when the tested code is unchanged.

Before creating/moving a version tag or publishing a release, run the complete
automated suite against the intended clean revision, in addition to the normal
checks below. This is a Codex repository instruction, not a Git hook:

```bash
make test
make vet
make build
make test-release
make test-integration
make test-browser
make fuzz FUZZ=FuzzStripJSONComments FUZZTIME=30s
make fuzz FUZZ=FuzzAccessAlias FUZZTIME=30s
make fuzz FUZZ=FuzzRelativePath FUZZTIME=30s
make fuzz FUZZ=FuzzClientMetadata FUZZTIME=30s
```

Report failures and unavailable prerequisites before proceeding with the tag or
release; skipped tests are not passing tests. Record Firefox, media and other
manual checks separately. Rebuild tagged release binaries and verify version
metadata after creating the tag. Finish by verifying test processes, listeners
and browser profiles are cleaned up. Keep reports/caches unless asked to remove
them, and preserve user data.

## Automated browser scope

`browser.cjs` starts an isolated headless Chrome profile and controls it through
CDP pipes. It uses `--no-sandbox` for this local, generated test fixture; do not
reuse this launch command to browse arbitrary websites. Every service, profile
and temporary file is under the repository `tmp/`. Children are terminated and
temporary data is deleted on success; failed runs retain artifacts in the
reported `tmp/browser-*` directory. Those artifacts can contain test access
codes in browser history and must remain untracked.

The first `Browser.getVersion` handshake has a separate 30-second startup
deadline; subsequent CDP commands retain their 5-second deadline. Pipe EOF,
closure, spawn errors and browser exit reject pending/future requests instead
of waiting for a timeout. `make test-browser` first runs the dependency-free
transport regressions in `cdp_test.cjs`, including delayed startup, framing,
disconnects, bounded timeout and diagnostic redaction.

If startup fails, the runner prints bounded startup-only Chrome/service stderr
with URLs, authentication headers and credential fields redacted, plus browser
exit/signal state. No CDP messages, captured requests or profile contents are
dumped. CI also prints the installed Chrome version. A handshake timeout means
the browser did not answer before the deadline; it does not by itself establish
a Filegate login failure or prove which browser/environment problem occurred.

The suite tests actual Origin headers, SameSite cookies, manual code login,
cross-site link landing/confirmation, replacement of an existing login,
directory isolation and logout, with JavaScript enabled and disabled. With
JavaScript enabled it also verifies Asia/Shanghai timezone reporting, the fixed
script's CSP hash, rejection of an unapproved inline script, and HTML/SVG source
preview. It checks application logs for test credential leakage. Inspector
evaluation is used to drive forms; it is not itself a test of CSP enforcement,
which is checked separately by inserting an unapproved page script.

`timezone.cjs` retains the submit-only, repeated-submission and missing-Intl
simulation as a reproducible optional check. Run it directly with
`node tests/timezone.cjs`; `make test-browser` also runs it first.

## Firefox and release acceptance

The CDP runner supports Chrome/Chromium. Firefox uses a different automation
protocol and is not silently treated as tested by a Chrome run. Until a Firefox
runner is added, perform this checklist in a disposable Firefox profile against
a separate initialized test service, and record the Firefox version and result:

1. Generate an `ab` code, enter it in the form and confirm that login succeeds;
   use Developer Tools to verify a same-origin POST and no unexpected CSP error.
2. Open a freshly generated `cd` access link from a page on another site
   (for example, `localhost` versus the service's `127.0.0.1`). Verify that GET
   does not consume the code, the URL is cleaned and confirmation switches to
   the correct directory. The previous session is replaced only after success.
3. Verify normal logout and inspect cookie attributes, as well as the UTC
   timezone/offset fields reported in application logs.
4. Disable JavaScript in the disposable profile, reload the login page, and
   repeat manual login, link confirmation and logout. Timezone may be unknown.
5. Open HTML and SVG fixtures containing harmless test scripts. They must be
   shown as source and must not execute. Check that unrelated inline scripts
   on generated pages are blocked by CSP.
6. For release acceptance, also check the files actually used in deployment:
   PDF, MP3/video seeking, images, UTF-8/UTF-16 text, explicit download and
   Chinese filenames. Browser media support varies; header-only fixtures do
   not prove that a media file decodes successfully.

No checklist entry counts as executed merely because it is documented.
Real disk-full/fsync failure, power loss, cross-UID permissions and mobile
browser behavior require separate controlled environments; do not change
system users, fill disks or kill a real service as part of ordinary tests.

Release dispatch regressions in `make test-release` also cover strict version
tag resolution, annotated/lightweight tags, prereleases, missing tags,
branch-name collisions and selecting a tag behind the workflow branch.
Workflow wiring is linted with actionlint during release infrastructure work;
actual GitHub dispatch registration, native arm64 execution and publication
must be checked in the hosted repository.
