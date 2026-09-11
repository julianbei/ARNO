# JADE Dogfooding Run Log

Use this file to record baseline and JADE-first runs per feature task.

## How to fill

- One section per task and run mode.
- Keep raw facts only.
- Put conclusions in [docs/dogfooding-report.md](dogfooding-report.md).

## Template

### Task: 1 / Symbol Disambiguation Responses

Run mode: baseline-shell-file

Date: 2026-09-11

Commit start: 16eb8ba

Commit end: 16eb8ba

Build/test result: pass

Wall-clock minutes: <1

Tool calls (count): 2

LLM turns (count): 1

Estimated token usage: low

Files touched: none

Notes:

- Used rg + file read to find symbol occurrences in [docs/fixtures/ambiguous.ts](docs/fixtures/ambiguous.ts).
- Ambiguity was visible to a person but not represented as a structured API outcome.

---

### Task: 1 / Symbol Disambiguation Responses

Run mode: jade-first

Date: 2026-09-11

Commit start: 16eb8ba

Commit end: fac9482 (implementation in progress in working tree)

Build/test result: pass

Wall-clock minutes: ~10

Tool calls (count): 4

LLM turns (count): 1

Estimated token usage: low-medium

Files touched:

- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/code/index.go](../internal/code/index.go)
- [internal/transport/internalapi/server.go](../internal/transport/internalapi/server.go)
- [internal/transport/mcp/server.go](../internal/transport/mcp/server.go)
- [cmd/jade/main.go](../cmd/jade/main.go)
- [docs/fixtures/ambiguous.ts](fixtures/ambiguous.ts)

Notes:

- `go run ./cmd/jade api-read-symbol docs/fixtures/ambiguous.ts refresh` returned `status=ambiguous` with candidate IDs.
- `go run ./cmd/jade api-read-symbol docs/fixtures/ambiguous.ts uniqueName` returned `status=exact`.
- `go run ./cmd/jade api-read-symbol docs/fixtures/ambiguous.ts missingSymbol` returned `status=not_found`.

---

### Task: <task-id / task-name>

Run mode: baseline-shell-file | jade-first

Date:

Commit start:

Commit end:

Build/test result: pass | fail

Wall-clock minutes:

Tool calls (count):

LLM turns (count):

Estimated token usage:

Files touched:

Notes:

---

### Task: <task-id / task-name>

Run mode: baseline-shell-file | jade-first

Date:

Commit start:

Commit end:

Build/test result: pass | fail

Wall-clock minutes:

Tool calls (count):

LLM turns (count):

Estimated token usage:

Files touched:

Notes:
