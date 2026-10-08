# Testing

[中文](testing.zh.md)

## Required checks

`make lint` checks gofmt, errcheck, govet, ineffassign, staticcheck, and unused code.
`make test` runs race detection without cached results. `make coverage` emits
`coverage.out` (ignored by Git), per-function coverage, and a per-package gate:
core at least 95%, each adapter at least 90%. Both CI Go versions enforce this gate.
If a newer local Go toolchain exceeds the linter's build version, choose a compatible
`GOTOOLCHAIN` or install a compatible linter; never silently skip lint.

## Coverage matrix

| Area | Evidence |
| --- | --- |
| Configuration | Strict schema, UTF-8, size/depth, aliases, duplicates, version, defaults, escaping paths and health URLs |
| Planning | Immutable source/config copies, exact digest, restore, mutable-input changes, invalid evidence |
| Engine | All stage acknowledgement failures, deadlines/cancellation, script errors, uncertain/mismatched starts, process exits, readiness redirects/failures, cleanup errors |
| Output | Bounds, split/overlapping secret values, truncation, UTF-8 chunk boundaries, late callbacks |
| Git | Real local repository, moved HEAD after resolution, pinned checkout, symlink/oversized config, failures, timeout and bounded/redacted output |
| Local runtime | Real processes, detached lifetime, finite cancellation, group cleanup, process/tag identity, close and observed exit codes |
| envd runtime | HTTP protocol fixtures, stream framing, authentication, PID/exit events, inspection/signals, stale and ambiguous tags, missing/malformed/oversized responses, redirects, lost-PID cleanup, stop deadlines, real supervisor/child cleanup |
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
