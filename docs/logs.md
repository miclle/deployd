# Live output and service logs

[中文](logs.zh.md)

## Execution output

`Options.OnOutput` retains its stage-end behavior. Optional `OnLiveOutput` emits
installation/start output as it arrives, without waiting for stage completion.
Both redact supplied literals and transient `Options.Env` values, share the same
64 KiB raw budget per stage across stdout/stderr, and emit valid UTF-8 events of at
most 4 KiB. Enabling both produces two views of the same output: do not persist
them as if they were distinct records. The Git adapter currently releases checkout
output only at completion, after its own credential redaction.

Live redaction delays a suffix of up to the longest secret minus one byte, plus
incomplete UTF-8 bytes. This prevents secrets split across transport chunks from
leaking and preserves overlapping masks. A very long secret can delay output until
completion. At observation end, a possible incomplete credential prefix is masked
conservatively. Stdout/stderr ordering may differ because their suffixes settle
independently. Redaction does not recognize transformed secrets or terminal markup.

Callbacks are synchronous, serialized within their output subscription, must
return promptly, and must not reenter runtime APIs. Applications own any bounded
queue, text rendering, storage, timestamps, correlation IDs and backpressure.

## Service observation

`LogSource` is an optional capability; the required `Runtime` interface is unchanged.
The envd adapter implements it through the Process `Connect` contract. It verifies
runtime/PID/tag with `List`, selects the unique tag and requires matching PID
confirmation before delivering any output. No observation path signals a process.
The built-in local adapter does not provide historical/reconnectable service logs.

Use `FollowLogs` rather than forwarding raw `LogSource.Logs` output. A subscription
has a caller-owned cancellation/deadline and at most 64 KiB of raw input, optionally
lowered with `LogOptions.MaxBytes`. Include agent tokens and application secrets in
`Redact`; unlike Apply, a reattached subscription has no transient application
environment to infer them from. Events have an empty `Stage`, because the service
may outlive deployment stages. See the [compiled example](../logs_example_test.go).

Exceeding the budget emits one truncation marker and returns `ErrLogLimit`.
Cancelling or closing a subscription **only ends observation**. The controller
must call `Stop` separately to terminate a workload. Provider failures retain
`errors.Is` identity through a safely printable `LogError`. A normal observed exit
ends log collection successfully; lifecycle state must be reconciled separately.

Agent log buffering, replay, retention and cursors are provider-specific. This
contract does not guarantee gap-free delivery or exactly-once records, and blindly
reconnecting can duplicate earlier output. Log persistence, replay policy and SSE
remain controller concerns. Live agent compatibility still requires the opt-in
[remote acceptance suite](envd-acceptance.md).
