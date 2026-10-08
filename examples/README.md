# Examples

[中文](README.zh.md)

## Local deployment

From the repository root, run:

```sh
go run ./examples/local
```

Requires Go 1.25 or newer, Git, and `/bin/sh` on Linux or macOS. No external
repository, Node.js, agent, or credentials are needed. Port 8080 must be available;
select another port with `go run ./examples/local -port 8081`.

The example copies the bundled [HTTP service](local/service/main.go) into a fresh
temporary Git repository and commits it. It prepares a plan pinned to that commit,
saves the normalized Spec and Snapshot in `plan.json` with owner-only permissions,
then uses the local runtime to check out the commit, build the service, start it,
and probe `/health`. `result.json` retains the full or partial execution evidence.
All example-owned files stay under the printed temporary directory; the project
checkout is not modified. The service listens only on `127.0.0.1`.

After `Ready: http://127.0.0.1:8080` appears, use another terminal:

```sh
curl http://127.0.0.1:8080/
# Hello from deployd's local example!
curl http://127.0.0.1:8080/health
# ok
```

The startup context has a two-minute deadline and is canceled after deployment;
the ready service continues running. Press Ctrl+C in the example terminal to stop
the exact saved process with an independent five-second cleanup context. The
runtime closes before the temporary directory is removed. If execution or cleanup
fails, the command exits nonzero and retains the directory for inspection. Once
any remaining process has been reconciled, remove that printed directory manually.
The library's `Stop` retains workspaces; directory removal here is owned by the
example application. Forcefully killing the example bypasses its cleanup.

This is a development demonstration of one in-process local runtime, not a durable
controller: it does not implement restart recovery, leases, or automatic retries.
See [controller integration](../docs/controller-integration.md) for those boundaries.

## API examples

Small Go API examples remain beside the package they document:

| File | Topic | Default verification |
| --- | --- | --- |
| [apply_example_test.go](../apply_example_test.go) | Prepare, Apply, and Stop | Compile only; uses a placeholder repository |
| [plan_example_test.go](../plan_example_test.go) | NewPlan and Restore | Execute and check output |
| [recovery_example_test.go](../recovery_example_test.go) | Checkpoint persistence and process reconciliation | Compile only; uses a placeholder agent |
| [logs_example_test.go](../logs_example_test.go) | FollowLogs with bounded, redacted output | Compile only; uses a placeholder agent |

`make examples` compiles the complete programs without running them. `make coverage`
also compiles them and runs the API example tests; programs under `examples/` are
excluded from library coverage thresholds. Default checks do not start this demo
or contact external services.
