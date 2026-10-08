# Architecture

[中文](architecture.zh.md)

## Immutable planning

`Prepare` resolves a source and validates configuration before provisioning.
Save `plan.Snapshot()` and `plan.ConfigBytes()` in caller-owned storage; both expose
copies. `Restore` rechecks that exact YAML digest and protocol without contacting a
source provider. Applying a plan never resolves its original branch again.

`Source` separates immutable resolution from materialization in an empty target
workspace. `Runtime` separates finite commands, detached processes, inspection,
stop, and readiness origins. Process references hold runtime, process, and unique
execution tag identities. Credentials stay in adapters or transient `Options.Env`.

## Execution

1. `preparing`: reserve `WorkRoot/OperationID` exclusively. Existing paths fail with
   `ErrConflict`; no workspace is deleted or reused.
2. `cloning`: fetch and check out the saved full commit.
3. `verifying`: check the saved configuration digest, regular-file configuration,
   and physical config/working-directory confinement within the workspace.
4. `installing`: run the installation/build command with transient environment.
5. `starting`: recheck the configuration and directory after installation; confirm
   a detached foreground process with the expected runtime and operation tag.
6. `probing`: inspect process liveness and poll the readiness origin plus health
   path. Accept only 2xx responses, without redirects; recheck liveness after HTTP
   success. The configured readiness deadline is capped at 300 seconds.
7. `ready`: return UTC readiness time and retained source/process evidence.

`OnStage` runs before each stage's effects and may persist an acknowledgement.
Callback failures stop advancement. Defaults bound cloning to five minutes,
installation to fifteen minutes, preparation/verification/start to one minute each,
and compensating process cleanup to five seconds. Caller cancellation also bounds
execution. Successful startup is detached from the Apply context lifetime.

## Failure and lifecycle

Retain `Result` even when Apply returns an error. `StageError` reports the failed
stage; `errors.Is` preserves causes and cleanup failures. Its printable message
omits commands, YAML, and provider bodies. A failed start may return a tag-only
process reference: the runtime can inspect/stop that unique tag if PID confirmation
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
environment values, including secrets split across transport chunks. Events contain
at most 4 KiB of valid UTF-8, stdout before stderr, plus a truncation marker when
needed. Exact stream interleaving is not retained. Startup observation ends after
PID confirmation; a late output callback is discarded after stage completion.

Callbacks must return promptly. Repository output is untrusted and should be
rendered as text. Redaction covers supplied literal values, not arbitrary secret
transformations. Applications own any continuous log subscription and durable event
storage; configuration and commands should not embed credentials.
