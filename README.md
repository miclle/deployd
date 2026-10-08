# deployd

[中文](README.zh.md)

A small Go deployment execution kernel for an already provisioned runtime.
Resolve an immutable Git commit, strictly parse deployment YAML, execute the
installation/build script, start a foreground service, and wait for HTTP readiness.

The import name is `deploy`; the module is `github.com/miclle/deployd`.
Go 1.25 or newer is required. The built-in local runtime supports Linux and macOS.

## Use

```sh
go get github.com/miclle/deployd
```

The caller selects the configuration path, commonly `deploy.yaml`:

```yaml
version: 1
workingDirectory: .
installCommand: npm ci
startCommand: npm run start -- --host 0.0.0.0 --port 3000
port: 3000
healthcheck:
  path: /health
  timeoutSeconds: 60
```

The execution flow is:

```go
source, err := gitsource.New(repositoryURL, gitsource.Options{Ref: "main"})
// Handle err before proceeding.
plan, err := deploy.Prepare(ctx, source, "deploy.yaml")
// Persist plan.Snapshot() and plan.ConfigBytes() after checking err.
// Supply an already provisioned Runtime.
result, err := deploy.Apply(ctx, source, runtime, plan, deploy.Options{
    WorkRoot: "/srv/deployments",
    OperationID: operationID, // Fresh and unique within this runtime.
})
// Retain partial result even on error; check StageError.Cleanup.
// A ready service keeps running after ctx is canceled.
err = deploy.Stop(cleanupCtx, runtime, result.Process)
```

Use a bounded cleanup context and handle every error. See the [complete, compiled
Go example](example_test.go), [controller takeover](docs/architecture.md#controller-takeover),
[execution architecture](docs/architecture.md),
[configuration protocol](docs/configuration.md), and [adapters](docs/adapters.md).
The Git adapter, local process runtime, and envd Process runtime are separate
packages; callers can also implement `Source` and `Runtime`.

## Ownership

Applications own authentication, resource provisioning, task scheduling, durable
state, concurrency, retry decisions, quotas, expiration, ingress, and resource
destruction. The library owns immutable source/configuration evidence and the
execution protocol. A deployment stop terminates the application process while
retaining its workspace and runtime. Scripts execute trusted repository code in
the target account; choose isolation appropriate for that code.

Each attempt uses a fresh workspace and unique operation ID. Scripts are never
retried automatically. Failed starts/probes attempt bounded process cleanup and
return partial evidence, including uncertain starts. Readiness is a point-in-time
2xx HTTP response with a running process, rather than a continuous health promise.
Output callbacks receive bounded, redacted stage output; live application log
streaming is owned by the embedding application.

## Development

Install golangci-lint 2.13 or newer, compiled with a Go version compatible with your
toolchain. Git and POSIX tools described in [adapters](docs/adapters.md) are required
for integration tests.

```sh
make check
make coverage
```

`make coverage` runs race tests and enforces at least 95% statement coverage for the
core package and 90% for each adapter. CI runs that gate on Go 1.25, 1.26, and 1.27, and
pins golangci-lint to 2.14.0. See [testing](docs/testing.md) for the coverage matrix
and the distinction between local integration and live remote acceptance.

