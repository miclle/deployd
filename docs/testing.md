# Testing

[中文](testing.zh.md)

## Required checks

`make lint` checks gofmt, errcheck, govet, ineffassign, staticcheck, and unused code.
`make test` runs race detection without cached results. `make coverage` emits
`coverage.out` (ignored by Git), per-function coverage, and a per-package gate:
core at least 95%, each adapter and internal helper package at least 90%. All three
CI Go versions (1.25, 1.26, and 1.27) enforce this gate.
`make lint` sets `GOTOOLCHAIN=go1.26.6` for golangci-lint, matching the Go minor
version used by the CI lint job. This avoids loading a newer local standard library
that the linter cannot parse. The Go command downloads this toolchain on first use
if needed; offline environments must install it beforehand. Override the selection
with `LINT_GOTOOLCHAIN`, for example `make lint LINT_GOTOOLCHAIN=go1.26.6`.
The linter must support the selected Go version and be built with at least that
version; check `golangci-lint version`. Setting only `.golangci.yml`'s Go version
does not select the toolchain used to load packages. Tests and coverage continue
to use your default Go toolchain; lint is never skipped.

## Coverage matrix

| Area | Evidence |
| --- | --- |
| Configuration | Strict schema, UTF-8, size/depth, aliases, duplicates, version, defaults, escaping paths and health URLs |
| Planning | Immutable source/config copies, exact digest, restore, mutable-input changes, invalid evidence |
| Engine | All stage acknowledgement failures, deadlines/cancellation, script errors, uncertain/mismatched starts, process exits, readiness redirects/failures, cleanup errors |
| Output | Bounds, split/overlapping secret values, complete and partial credentials at truncation boundaries, UTF-8 chunk boundaries, late callbacks |
| Git | Real local repository, moved HEAD after resolution, pinned checkout, symlink/oversized config, failures, timeout and bounded/redacted output |
| Local runtime | Real processes, detached lifetime, finite cancellation, group cleanup before leader reaping, process/tag identity, external supervisor termination, close and observed exit codes |
| envd runtime | HTTP protocol fixtures, stream framing, authentication, PID/exit events, inspection/signals, stale and ambiguous tags, missing/malformed/oversized responses, redirects, lost-PID cleanup, reused-PID stream failures, stop deadlines, real supervisor/child cleanup before and after session creation |
| End to end | Real Git + HTTP service: fixed commit, install artifact, readiness, duplicate attempt, stop, health failure cleanup, cancellation after start, configuration drift and symlink confinement |

The complete Go example is compile-checked; its placeholder repository is not
contacted during tests. Integration tests start a child copy of the Go test binary
as the HTTP service, so Node.js/Python are not runtime prerequisites. Git, `/bin/sh`,
`realpath`, and `sha256sum` or `shasum` must be available. Linux/macOS are the local
execution platforms; the CI matrix exercises Linux.

Supervisor tests exercise `/bin/sh` and explicitly exercise dash when installed,
including on macOS. Linux uses the native `setsid` utility; macOS uses a shim
that calls the actual session syscall and exec. Tests verify normal-exit and TERM
child-group cleanup; test failure cleanup also kills its isolated group and bounds
output-pipe waits.

Protocol fixtures prove the adapter contract only. Live envd acceptance still needs
an existing isolated target: verify source authentication, stream-disconnect process
survival, endpoint routing, exact-tag stop, cancellation cleanup, and target account
permissions. No automated test provisions or destroys remote resources.

See [live envd acceptance](envd-acceptance.md) for explicit opt-in, environment
configuration, process/reconstruction tests and pinned deployment verification.

Live-output tests verify pre-completion delivery, mixed streams, byte budgets,
UTF-8 boundaries, late callbacks and overlapping/split redaction against the
whole-stream reference algorithm. Log protocol tests cover PID confirmation,
foreign tags, cancellation, malformed/failed streams and absence of signals.

Long overlapping credentials are covered with single and fragmented writes,
including a trailing partial credential. For each secret, each matching pass marks
each pending byte at most once, avoiding repeated work for overlapping matches.
Run `go test ./internal/redact -run '^$' -bench BenchmarkStreamOverlappingMatches`
to measure this path with 1 KiB and 32 KiB repeated-byte credentials.
