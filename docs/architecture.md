# Architecture

[中文](architecture.zh.md)

`Prepare` resolves a source and validates configuration before provisioning.
`Restore` reconstructs a plan from saved evidence and exact configuration bytes.
Plans expose value copies and never resolve a branch during execution.

The `Source` contract separates immutable resolution from target materialization.
The `Runtime` contract separates finite commands from long-running processes,
inspection, stopping, and readiness origins. Process references carry runtime,
process, and execution tag identities; credentials stay in adapters.

A runtime is already provisioned. Deployment stop terminates the application, not
the runtime. Applications own durable orchestration and compensation for uncertain
resource creation. Successful readiness is point-in-time evidence, not continuous
health monitoring or a guarantee of public access.
