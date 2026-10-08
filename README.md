# deployd

[中文](README.zh.md)

A small Go deployment execution kernel for an already provisioned runtime.
Prepare an immutable source snapshot, strictly validate its YAML configuration,
and execute installation, foreground service startup, and HTTP readiness checks.

The import name is `deploy` and the module is `github.com/miclle/deployd`.
Go 1.25 or newer is required. Licensed under [MIT](LICENSE).

## Ownership

Applications own authentication, resource provisioning, task scheduling, durable
state, retry decisions, quotas, expiration, and resource destruction. The library
owns source/configuration evidence and the execution protocol. Repository commands
are trusted code; execute them in an appropriately isolated environment.

## Configuration

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

See [configuration](docs/configuration.md), [architecture](docs/architecture.md),
and [testing](docs/testing.md).

## Development

Install golangci-lint 2.13 or newer, compatible with your Go toolchain.

```sh
make check
make coverage
```

CI runs race tests on Go 1.25 and 1.26 and pins lint to version 2.14.0.
