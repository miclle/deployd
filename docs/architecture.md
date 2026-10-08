# Architecture

[中文](architecture.zh.md)

## Immutable planning

`NewPlan` binds resolved source evidence to a validated, normalized Spec before
provisioning. `Prepare` validates a supplied Spec and resolves the source once.
Applications own configuration parsing and must read repository configuration at
the resolved commit. Save `plan.Snapshot()` and `plan.Spec()` in caller-owned
storage; both expose value copies. `Restore` checks the evidence version,
canonical parameters and their digest without contacting a source provider.
Applying a plan revalidates its evidence before effects and never resolves its
original branch again. See [execution parameters](configuration.md).

`Source` separates immutable resolution from materialization in an empty target
workspace. `Runtime` separates finite commands, detached processes, inspection,
stop, and readiness origins. Process references hold runtime, process, and unique
execution tag identities. Credentials stay in adapters or transient `Options.Env`.

## Execution

1. `preparing`: reserve `WorkRoot/OperationID` exclusively. Existing paths fail with
   `ErrConflict`; no workspace is deleted or reused.
2. `cloning`: fetch and check out the saved full commit.
3. `verifying`: check physical working-directory confinement within the workspace.
   No deployment configuration file is required.
4. `installing`: run the installation/build command with transient environment.
5. `starting`: recheck directory confinement after installation; confirm
   a detached foreground process with the expected runtime and operation tag.
6. `probing`: inspect process liveness and poll the readiness origin plus health
   path. Accept only 2xx responses, without redirects; recheck liveness after HTTP
   success. The configured readiness deadline is capped at 300 seconds.
7. `ready`: return UTC readiness time and retained source/process evidence.

`OnStage` runs before each stage's effects and may persist an acknowledgement.
`OnCheckpoint` then receives a copy of the partial result and, from `starting`,
a tag-only `StartIntent`. It must durably acknowledge that evidence before
returning. `probing` carries the confirmed process; `ready` also carries endpoint
and UTC readiness time. Callback failures stop advancement and compensate an
already started process. A failed ready acknowledgement returns no `ReadyAt`. Defaults bound cloning to five minutes,
installation to fifteen minutes, preparation/verification/start to one minute each,
and compensating process cleanup to five seconds. Caller cancellation also bounds
execution. Successful startup is detached from the Apply context lifetime.

## Failure and lifecycle

Retain `Result` even when Apply returns an error. `StageError` reports the failed
stage; `errors.Is` preserves causes and cleanup failures. Its printable message
omits commands, execution parameters, and provider bodies. A failed start may
return a tag-only process reference: the runtime can inspect/stop that unique tag if PID confirmation
was lost. Never reuse operation IDs/tags; serialize actions for the same attempt.

Failure after an uncertain/confirmed start attempts Stop with an independent,
bounded cleanup context, including when the caller context was canceled. A cleanup
error remains in `StageError.Cleanup`; the application owns reconciliation. Library
calls do not implement durable orchestration, distributed leases, atomic fencing,
automatic script retries, or exactly-once execution.

`Stop` terminates the saved application execution. Workspaces remain for caller-
owned diagnosis/retention, and the runtime is not destroyed. A ready service
continues until explicitly stopped, the runtime closes, or the process exits.
Readiness neither proves public ingress nor continuously monitors availability.

## Output

Each stage retains at most 64 KiB of raw output across both streams. Stage-end
callbacks merge each stream before replacing explicit `Redact` values and transient
environment values, including secrets split across transport chunks.
Overlapping matches and incomplete credential prefixes at a truncated stream
boundary are redacted before output is emitted. Events contain
at most 4 KiB of valid UTF-8, stdout before stderr, plus a truncation marker when
needed. Exact stream interleaving is not retained. Startup observation ends after
PID confirmation; a late output callback is discarded after stage completion.

Callbacks must return promptly. Repository output is untrusted and should be
rendered as text. Redaction covers supplied literal values, not arbitrary secret
transformations. Applications own log subscription lifetimes and durable event
storage; configuration and commands should not embed credentials.

## Controller takeover

Persist checkpoints in an attempt journal, separate from saved execution
parameters and transient credentials. After a crash, acquire exclusive ownership, reconstruct
the same runtime incarnation, and inspect/stop `Result.Process` or, if its response
was lost, `StartIntent`. A start intent is not proof of existence or ownership of
a rejected conflicting start. Never reuse tags or clean up another attempt.

`Restore` restores a plan, not an execution cursor. Repeating `Apply` with an
existing workspace fails with `ErrConflict`. Complete reconciliation before
deciding whether a new operation ID may run scripts again; external installation
effects can make replay unsafe. The library does not fence stale workers or
persist failure/cleanup completion: retain the final partial result and
`StageError.Cleanup` as well as checkpoints. See the compiled
[recovery examples](../recovery_example_test.go).

See [error handling](errors.md) for typed exit codes and safe source classifications.

Optional [live output and service observation](logs.md) preserve the stage-end
callback contract and keep observation cancellation separate from process stop.
