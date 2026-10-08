---
name: envd-acceptance
description: Run or diagnose deployd acceptance against an existing isolated envd target when validating a real agent or a new agent version. Excludes ordinary local tests and resource provisioning.
---

# envd acceptance

Use this workflow for live envd compatibility evidence. Creating or invoking this
skill does not itself authorize remote execution or resource provisioning.

## Prepare

Read [live acceptance](../../../docs/envd-acceptance.md) for the maintained input
table, test commands, cleanup behavior, and coverage limits. Check the relevant
tests in [acceptance_test.go](../../../runtime/envd/acceptance_test.go) if diagnosing
a failure or if the documented procedure and implementation differ. Keep commands
and input definitions in those sources rather than duplicating them here.

Use the task's existing authorization and configured shell or secret manager.
Confirm the target is an existing disposable, isolated Linux agent with no
concurrent deployment workers. Check required inputs without displaying secrets
or command values. Runtime identity must identify the same agent incarnation
after adapter reconstruction. The fixture must be trusted and pinned to a full
commit. If execution scope or target configuration is missing, request only the
missing information; do not provision a replacement or switch accounts/targets.

## Execute and reconcile

Run the process and deployment suites from the repository root using the commands
in the acceptance document. Report each suite separately as passed, failed,
skipped, or not run. Both must pass before declaring full remote acceptance.
A skipped test, protocol fixture, or successful compilation is not live evidence.

If cleanup fails or process ownership is uncertain, stop further live execution
and reconcile the saved runtime/process/tag evidence within the authorized target.
Do not signal a bare PID, reuse an operation tag, or retry scripts automatically.
Retain deployment workspaces for caller-owned diagnosis and retention; do not add
blanket deletion or resource destruction to the workflow.

## Report

Record the test date, agent version, configured user, source commit, per-suite exit
status, and cleanup result. Mark unavailable evidence as unknown. Exclude tokens,
environment dumps, application commands, and repository output from the report.

Keep the acceptance document's uncovered scenarios explicit. Passing these suites
does not prove private-source access, token refresh, lost-PID fault injection,
atomic fencing, controller crash recovery, or public traffic cutover. Record the
actual run evidence separately; do not turn a single result into a permanent
guarantee in this skill.
