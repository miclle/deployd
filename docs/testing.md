# Testing

[中文](testing.zh.md)

`make lint` checks gofmt, errcheck, govet, ineffassign, staticcheck, and unused code.
`make test` runs race detection without cached results. `make coverage` emits
`coverage.out` (ignored by Git) and per-function coverage.

Contract tests cover bounded strict configuration, immutable plans, and serializable
process references. Execution and adapter tests must additionally cover failed
stages, cancellation, missing/early process exits, cleanup failures, stale process
identity, malformed remote replies, and output limits. Local real-process tests and
protocol fixtures are distinct from a live remote-runtime acceptance test.
