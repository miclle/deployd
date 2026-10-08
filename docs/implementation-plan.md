# Execution integration plan

[中文](implementation-plan.zh.md)

## Goal and boundary

Make the execution kernel easier to embed in a durable deployment controller
without adding accounts, databases, schedulers, resource provisioning, automatic
script retries, or distributed fencing. Preserve exact source evidence, fresh
attempt workspaces, bounded output, process ownership, and independent cleanup.

Each phase updates English and Chinese documentation and is committed separately
after `make lint` and affected race tests pass. Execution and adapter phases also
pass `make coverage` (core >=95%, every adapter/helper >=90%). Commits remain local.

## Phases

| Phase | Deliverable and acceptance | Status |
| --- | --- | --- |
| 1 | Acknowledged execution checkpoints containing start intent and partial result; immutable callback copies; failure/cancellation tests; compiled controller recovery example | Complete |
| 2 | Compiled exact-byte plan persistence/restore example, shared protocol fixtures, migration notes covering original YAML, stable repository identity, strict 2xx readiness and caller-owned Host injection | Complete |
| 3 | Opt-in acceptance tests against an existing isolated envd; credentials from environment only; detached lifetime, reconstructed adapter, exact-tag stop and finite cancellation; remote limitations documented | Implemented; live target pending |
| 4 | Typed command exit errors and safe Git error categories without stderr parsing or automatic retry claims; failure-path tests and error handling docs | Complete |
| 5 | Optional bounded live output subscription with cross-chunk redaction; adapter-level log attachment separate from Start/Stop; cancellation and failure tests | Pending |

## Evidence

Before implementation, `go test -race -count=1 ./...` passed for deployd.
Relevant comparison tests passed in the deployment controller's configuration,
Sandbox executor, and Worker packages. Those results do not prove live envd
compatibility. Remote acceptance requires explicit environment configuration for
an existing isolated target; no test provisions or destroys infrastructure.

## Recovery contract

An interrupted attempt is never resumed by calling `Apply` with the same operation
ID. The controller persists intent before starting, fences/serializes ownership,
inspects and stops the saved execution when required, then decides whether a fresh
attempt is safe. `Restore` restores the immutable plan only. Installation commands
can have external side effects; process cleanup alone never makes replay safe.

Phase 1: compatible-toolchain lint and full race coverage passed; core 98.8%,
envd 94.9%, local 95.6%, Git 96.6%, redaction 100%.

Phase 2: compatible-toolchain lint and core race tests passed; reusable protocol fixtures and runnable exact-byte restore example passed.

Phase 3: compatible-toolchain lint and full race coverage passed. Live tests compile and skip without opt-in; no isolated agent is configured in this shell, so remote acceptance remains unverified.

Phase 4: compatible-toolchain lint and full race coverage passed; core 98.8%, Git 97.6%. Target checkout errors now retain uncertain process cleanup identity without exposing provider text.
