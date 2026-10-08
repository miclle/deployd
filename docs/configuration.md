# Configuration protocol

[中文](configuration.zh.md)

Version 1 accepts `version`, `workingDirectory`, `installCommand`, `startCommand`,
`port`, and `healthcheck` (`path`, `timeoutSeconds`). Installation includes any
required build step. The service must stay in the foreground and bind to a reachable
interface, normally `0.0.0.0`.

Only one UTF-8 YAML document, up to 64 KiB, is accepted. Unknown or duplicate keys,
aliases/merges, excessive nesting, invalid ports, escaping paths, and unsafe health
paths are rejected. Work directories default to `.`, readiness paths to `/`, and
readiness deadlines to 60 seconds. An explicit nonpositive deadline is rejected;
execution caps readiness at 300 seconds. Repository config paths are selected by
the caller; the library has no product-specific filename convention.

Do not put credentials in versioned configuration or commands. Runtime environment
variables are transient inputs, separate from immutable source evidence.

Reusable protocol fixtures live in `testdata/protocol`; see the
[controller migration notes](controller-integration.md).
