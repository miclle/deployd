# deployd

[中文](README.zh.md)

A small Go deployment execution kernel for an already provisioned runtime.
Bind an immutable Git commit to validated execution parameters, execute the
installation/build script, start a foreground service, and wait for HTTP readiness.

The import name is `deploy`; the module is `github.com/miclle/deployd`.
Go 1.25 or newer is required. The built-in local runtime supports Linux and macOS.

## Use

```sh
go get github.com/miclle/deployd
```

Applications read their own configuration from files, databases, APIs, or other
sources and map it to execution parameters:

```go
spec := deploy.Spec{
    WorkingDirectory: ".",
    InstallCommand: "npm ci",
    StartCommand: "npm run start -- --host 0.0.0.0 --port 3000",
    Port: 3000,
    Healthcheck: deploy.Healthcheck{Path: "/health", TimeoutSeconds: 60},
}
```

`Prepare` validates the parameters and resolves the source once; `Apply` checks out
the saved commit and executes the plan in the supplied runtime:

```go
source, err := gitsource.New(repositoryURL, gitsource.Options{Ref: "main"})
// Handle err before proceeding.
plan, err := deploy.Prepare(ctx, source, spec)
// Persist plan.Snapshot() and plan.Spec() after checking err.
// Supply an already provisioned Runtime.
result, err := deploy.Apply(ctx, source, runtime, plan, deploy.Options{
    WorkRoot: "/srv/deployments",
    OperationID: operationID, // Fresh and unique within this runtime.
})
// Retain partial result even on error; check StageError.Cleanup.
// A ready service keeps running after ctx is canceled.
err = deploy.Stop(cleanupCtx, runtime, result.Process)
```

Use an absolute, dedicated, caller-owned `WorkRoot`. Keep the runtime alive for the
service lifetime: closing a local runtime stops all of its owned processes.
Use a bounded cleanup context and handle every error. See the [complete,
compile-checked Go example](example_test.go),
[execution architecture](docs/architecture.md),
[controller integration](docs/controller-integration.md), [execution parameters](docs/configuration.md), and [adapters](docs/adapters.md).
The Git adapter, local process runtime, and envd Process runtime are separate
packages; callers can also implement `Source` and `Runtime`.

For repository configuration, resolve the commit first, read the application's
configuration at that exact commit, then call `deploy.NewPlan(resolved, spec)`.
Persist the normalized Spec and Snapshot; `deploy.Restore(snapshot, spec)` requires
no original configuration bytes and does not resolve the source again. Saved Spec
values contain commands; protect their storage and keep them out of lifecycle
events. See the [planning and persistence examples](plan_example_test.go) and
[migration notes](docs/controller-integration.md) for the breaking change from the
previous YAML-based API and persistence format.

## Ownership

Applications own configuration formats/parsing, authentication, resource
provisioning, task scheduling, durable state, concurrency, retry decisions, quotas,
expiration, ingress, and resource destruction. The library owns immutable
source/execution-parameter evidence and the execution protocol.
A deployment stop terminates the application process while
retaining its workspace and runtime. Scripts execute trusted repository code in
the target account; choose isolation appropriate for that code.

Each attempt uses a fresh workspace and unique operation ID. Scripts are never
retried automatically. Failed starts/probes attempt bounded process cleanup and
return partial evidence, including uncertain starts. Readiness is a point-in-time
2xx HTTP response with a running process; redirects are not followed. It does not
prove public ingress or continuously monitor health.
Output callbacks receive bounded, redacted stage output. Optional live execution
output and envd log attachment are described in [logs](docs/logs.md); applications
own log subscription lifetimes and storage.

For controller takeover, persist `Options.OnCheckpoint` evidence before returning
from the callback, then reconcile the saved process under exclusive ownership.
`Restore` restores a plan, not an execution cursor; it does not make replay safe.
See [controller takeover](docs/architecture.md#controller-takeover) and the
[compile-checked recovery examples](recovery_example_test.go).

## Development

Install golangci-lint 2.13 or newer, compiled with Go 1.26.6 or newer.
`make lint` selects Go 1.26.6 independently of your default Go toolchain; Go downloads
it on first use if needed. Override it with `make lint LINT_GOTOOLCHAIN=go1.26.6`
when changing the lint toolchain, using a version supported by your linter.
Git and POSIX tools described in [adapters](docs/adapters.md) are required for
integration tests.

```sh
make check
```

`make` defaults to `make check`: dependency-file consistency, lint, and race tests
with coverage gates. Use `make gomod`, `make fmt-check`, `make lint`, `make test`,
or `make coverage` to run individual checks. `make fmt` formats Go files in place.

`make coverage` runs race tests and enforces at least 95% statement coverage for the
core package and 90% for each adapter and internal helper package. CI runs that
gate on Go 1.25, 1.26, and 1.27
on Linux and Go 1.27 on macOS, checks dependency-file consistency on Go 1.25, and
pins golangci-lint to 2.14.0. See [testing](docs/testing.md) for the coverage matrix
and the distinction between local integration and live remote acceptance.
