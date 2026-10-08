# Adapters

[中文](adapters.zh.md)

## Git source

`source/git.New` accepts a credential-free HTTPS repository URL and a branch, tag,
full SHA, or `HEAD`. Git must be installed on the controller and runtime. Resolution
uses a temporary repository, a five-minute default deadline, bounded blob reads,
and regular-file configuration only. Materialization fetches the saved SHA and
checks the resulting HEAD; it never uses the original mutable reference.

Local absolute paths require `AllowLocal: true` and are for trusted local execution.
`Env` and `CheckoutEnv` provide separate transient Git authentication environments;
a controller credential helper is not automatically available on a remote target.
Callers supply allowlists for repository hosts and authorize private source access.
Git errors discard stderr and output is bounded and redacted against CheckoutEnv.

## Local POSIX runtime

`runtime/local` supports Linux and macOS. It executes as the host account and is
intended for trusted development/testing or deployment agents with external isolation.
Finite commands are cancellation-owned; detached services use independent process
groups. Stop verifies the in-memory runtime/process/tag tuple and kills the owned
group. Close stops owned workloads. Records do not survive a controller restart.

## envd runtime

`runtime/envd` talks directly to the envd Process service through Connect JSON using
standard-library HTTP. Supply an existing agent URL, runtime identity, transient
access token, user, and port-to-readiness-origin function. Agent calls disable
redirects and cap each response/envelope at 1 MiB. Output is not accumulated without
bounds. Start closes observation after PID confirmation; the agent must keep the
process alive when the stream disconnects. No continuous log subscription is owned.

The Linux agent must provide `setsid` and a POSIX shell whose `kill` supports
negative process-group IDs. A supervisor starts the workload in its own session.
Stop sends TERM to that supervisor, which kills/reaps its group before exiting;
normal exit also clears remaining children. Foreground scripts must not daemonize
or escape to another session. Finite-command failures reconcile a unique tag even
when the initial PID response was lost. Observed nonzero exits retain their code
without retaining the agent error string.

Inspect/Stop verify runtime, PID, and tag; uncertain starts can be reconciled by a
unique tag. Agent APIs do not provide atomic compare-and-signal fencing: callers
must serialize actions for one execution and must never reuse an operation tag.
No direct SSH adapter or infrastructure provisioning is included.
