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

Run mode: baseline-shell-file

Date: 2026-09-11

Commit start: f60da96

Commit end: f60da96

Build/test result: pass

Wall-clock minutes: <1

Tool calls (count): 1

LLM turns (count): 1

Estimated token usage: low

Files touched: none

Notes:

- Used `rg` pattern scan on [docs/fixtures/outline_sections.ts](docs/fixtures/outline_sections.ts) to manually infer sections.
- Result was human-readable but not returned in a structured API shape.

---

### Task: 2 / File Outline Sections

Run mode: jade-first

Date: 2026-09-11

Commit start: f60da96

Commit end: in-progress

Build/test result: pass

Wall-clock minutes: ~8

Tool calls (count): 2

LLM turns (count): 1

Estimated token usage: low-medium

Files touched:

- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/code/index.go](../internal/code/index.go)
- [internal/transport/internalapi/server.go](../internal/transport/internalapi/server.go)
- [internal/transport/mcp/server.go](../internal/transport/mcp/server.go)
- [cmd/jade/main.go](../cmd/jade/main.go)
- [docs/fixtures/outline_sections.ts](fixtures/outline_sections.ts)

Notes:

- `go run ./cmd/jade api-outline-sections docs/fixtures/outline_sections.ts` returned:
	`imports=2 types=1 classes=1 functions=1 methods=1 other=0`
- Flat outline remains present for compatibility while structured sections are now returned.

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

Run mode: baseline-shell-file

Date: 2026-09-11

Commit start: d3387e2

Commit end: d3387e2

Build/test result: pass

Wall-clock minutes: <1

Tool calls (count): 1

LLM turns (count): 1

Estimated token usage: low

Files touched: none

Notes:

- Baseline shell visibility was limited to repository state (`git rev-parse`, `git status`).
- No runtime checkpoint/revert API semantics were available through shell-only inspection.

---

### Task: 3 / Checkpoint/Revert Primitives

Run mode: jade-first

Date: 2026-09-11

Commit start: d3387e2

Commit end: in-progress

Build/test result: pass

Wall-clock minutes: ~10

Tool calls (count): 2

LLM turns (count): 1

Estimated token usage: low-medium

Files touched:

- [internal/workspace/manager.go](../internal/workspace/manager.go)
- [internal/protocol/types.go](../internal/protocol/types.go)
- [internal/transport/internalapi/server.go](../internal/transport/internalapi/server.go)
- [internal/transport/mcp/server.go](../internal/transport/mcp/server.go)
- [cmd/jade/main.go](../cmd/jade/main.go)

Notes:

- `go run ./cmd/jade api-checkpoint-demo` output:
	`before=r1 paths=0`
	`checkpoint=cp-1 revision=r1`
	`during=r2 paths=1`
	`after=r1 paths=0 restored=cp-1`
	`checkpoint_events=2`
- Event stream included both `CHECKPOINT_CREATED` and `CHECKPOINT_RESTORED`.

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
