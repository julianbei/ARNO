# JADE Dogfooding Feature Tasks (MVP)

Purpose: define concrete feature work that will be implemented using JADE primitives first, then compared against a shell/file baseline.

## Task Set

### Task 1: Symbol Disambiguation Responses

Objective:

Implement explicit outcomes for symbol reads:

- exact match
- ambiguous match (multiple candidates)
- not found

Target files:

- [internal/code/index.go](../internal/code/index.go)
- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/transport/internalapi/server.go](../internal/transport/internalapi/server.go)
- [internal/transport/mcp/server.go](../internal/transport/mcp/server.go)

Acceptance criteria:

- Symbol reads never silently guess when duplicates exist.
- API response includes candidate IDs when ambiguous.
- Behavior is consistent in both internal API and MCP transport.

### Task 2: File Outline Sections

Objective:

Return separated outline sections for:

- imports
- types/classes
- functions/methods

Target files:

- [internal/code/index.go](../internal/code/index.go)
- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/transport/internalapi/server.go](../internal/transport/internalapi/server.go)
- [internal/transport/mcp/server.go](../internal/transport/mcp/server.go)

Acceptance criteria:

- Outline response is structured by section, not one flat list.
- Existing consumers remain functional.
- Output remains bounded and deterministic.

### Task 3: Checkpoint/Revert Primitives

Objective:

Add state primitives for checkpoint and revert.

Target files:

- [internal/workspace/manager.go](../internal/workspace/manager.go)
- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/transport/internalapi/server.go](../internal/transport/internalapi/server.go)
- [internal/transport/mcp/server.go](../internal/transport/mcp/server.go)

Acceptance criteria:

- Workspace can create a checkpoint ID.
- Workspace can revert to a checkpoint ID.
- Event stream includes checkpoint created/reverted events.

### Task 4: Decisive Validation Summaries

Objective:

Summarize key failure lines from async job output before raw logs.

Target files:

- [internal/jobs/runner.go](../internal/jobs/runner.go)
- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/diagnostics/service.go](../internal/diagnostics/service.go)

Acceptance criteria:

- Job result contains a concise decisive summary.
- Raw output remains available separately.
- Summary format is consistent across test/build/check jobs.

### Task 5: Tree-sitter Read Boundary Spike

Objective:

Add initial tree-sitter-backed boundary detection for at least Go.

Target files:

- [internal/code/index.go](../internal/code/index.go)
- [internal/languages/golang/adapter.go](../internal/languages/golang/adapter.go)

Acceptance criteria:

- Go symbol range can be read via parser-backed path.
- Regex fallback is still available behind a feature switch.
- Boundary mismatches are documented.

## Execution Rule

For each task above:

1. Run once with shell/file workflow (baseline).
2. Run once with JADE primitives first.
3. Record both runs in [docs/dogfooding-run-log.md](dogfooding-run-log.md).
4. Summarize comparison in [docs/dogfooding-report.md](dogfooding-report.md).
