# Tool surface contract — 0.0.1

This is what jade promises to callers, and what it does not.

## Why this is frozen at all

The MCP tool catalog is fixed at connection time. A client holds whatever
surface it saw when it connected, so tool names and required arguments become a
published interface the moment anyone integrates — and an accidental change is
invisible until someone else's session breaks, in a way that looks like their
bug rather than ours.

Two practical consequences:

- **A new tool does not appear until the client reconnects.** Not a bug; it is
  how the protocol works. Every "brand-new tool is missing" report in this
  project's own development turned out to be this.
- **Renaming a tool is not a rename.** Old clients keep calling the old name
  until they reconnect, so from their side it is a deletion.

## Enforcement

`cmd/jade-mcp/contract_test.go` holds the frozen list and fails on any drift.
It is deliberately a golden list rather than a generated one: the point is to
make a change *deliberate*, and a test that regenerates its own expectation
cannot do that.

`make test` runs it. `run_command docs-check` separately verifies every served
tool appears in the README.

## Stability policy

| Change | Compatible? | What callers see |
|---|---|---|
| Add a tool | Yes | Nothing, until they reconnect |
| Add an optional argument | Yes | Nothing |
| Make a required argument optional | Yes | Nothing |
| Change response *wording* | Yes | Text differs; see below |
| Remove or rename a tool | **No** | Calls fail at the next call |
| Make an optional argument required | **No** | Calls fail at the next call |
| Change a response's *structure* | **No** | Parsing breaks |

Breaking changes need a version bump and a line in the release notes.

## Required arguments are a promise the server enforces

A required argument the implementation ignores is worse than either choice
alone: a client that trusts the schema sends a value it never needed, and one
that does not is told it is wrong when it is not.

This was true of `expectedRevision` on `replace_text` and `replace_range`,
which declared it required while the server treated an empty value as "no
precondition" — the same as `apply` and `replace_symbol`. Fixed at the freeze
by making the schema match the behaviour, since the behaviour was the right
one: a one-shot edit should not have to fetch a revision first.

`expectedRevision` remains **optional and recommended** everywhere it appears.
Supplying it turns a lost-update race into a clean `stale revision` rejection.

## Response format

Responses are **plain text, not JSON**, shaped to lead with the decisive line.
See [response-style.md](response-style.md) for the rules and the benchmark that
motivated them (JSON cost 5.63x the tokens of the equivalent shell commands;
plain text brought it to 0.85x).

What is promised:

- The first line carries the verdict or the identifier a caller needs next —
  `pass tests`, `r7 · 3 files, +40 -12`, `not found: X`, `ambiguous: X matches …`.
- Failures are stated, never implied by absence.
- Truncation is always announced, with the true total (`… 48 more files`).
- Line-oriented output stays `path:line: text`, which is greppable and
  diffable.

What is **not** promised: exact wording. Treat responses as text for a model to
read, not as a format to parse. `JADE_JSON=1` switches every response to JSON
if you need something machine-readable; that JSON mirrors the Go structs in
`internal/protocol/types.go` and is subject to the same stability table above.

## Known asymmetries, deliberately kept

- `delete_symbol` and `read_symbol` require `path` but accept *either*
  `symbolName` or `symbolId`. JSON Schema's `required` cannot express
  "one of these two", so the server enforces it and returns a clear error.
- `create_file` refuses to overwrite; `replace_file` refuses to create. Neither
  takes a force flag — choosing the tool is the explicit act.
- `search` ranks declaration *names*; `grep` searches *text*; `find` does both
  and returns bodies. They overlap and that is intended, but `search` will not
  find a struct field or a string literal. Its description says so.

## The frozen surface

34 tools. `cmd/jade-mcp/contract_test.go` is the authority; this table is for
reading.

| Tool | Required arguments |
|---|---|
| `jade.apply` | `edits` |
| `jade.changes` | — |
| `jade.check` | — |
| `jade.checkpoint` | — |
| `jade.context` | `path` |
| `jade.create_file` | `content`, `path` |
| `jade.declare_command` | `name` |
| `jade.delete_file` | `path` |
| `jade.delete_symbol` | `path` |
| `jade.diff` | — |
| `jade.events` | — |
| `jade.find` | — (`query` or `queries`, enforced by the server) |
| `jade.grep` | `query` |
| `jade.history` | `path` |
| `jade.insert` | `path`, `text` |
| `jade.job_output` | `id` |
| `jade.job_status` | `id` |
| `jade.outline` | `path` |
| `jade.read_range` | — (`path` or `ranges`, enforced by the server) |
| `jade.read_symbol` | `path` |
| `jade.references` | `path` |
| `jade.rename` | `newName`, `path` |
| `jade.replace_file` | `content`, `path` |
| `jade.replace_range` | `endLine`, `newCode`, `path`, `startLine` |
| `jade.replace_symbol` | `newCode`, `symbolId` |
| `jade.replace_text` | `newText`, `oldText`, `path` |
| `jade.repository_map` | `query` |
| `jade.retrieve` | `query` |
| `jade.revert` | `checkpointId` |
| `jade.run_command` | — |
| `jade.run_tests` | — |
| `jade.search` | `query` |
| `jade.search_nudge` | `command` |
| `jade.telemetry` | — |
| `jade.workspace_tree` | — |

## What is explicitly not frozen

- **Response wording**, as above.
- **`.jade/commands.json` and `.jade/telemetry.jsonl` formats.** Both are
  local files, neither is a wire format, and telemetry in particular is
  expected to grow fields.
- **Symbol ID spelling** (`path::Name@line`). Treat it as opaque and obtain it
  from a response rather than constructing it.
- **Anything under `internal/`.** Go's own visibility rules already say so.
