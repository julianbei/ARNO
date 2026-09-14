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

- `delete_symbol`, `references`, `rename`, `history` and `context` require
  `path` but accept *either* `symbolName` or `symbolId`. JSON Schema's
  `required` cannot express "one of these two", so the server enforces it and
  returns a clear error.
- `create_file` refuses to overwrite; `replace_file` refuses to create. Neither
  takes a force flag — choosing the tool is the explicit act.
- `grep` searches *text*; `find` finds declarations by name and returns their
  bodies. Each description says when to prefer it over the other.

## The frozen surface

31 tools. `cmd/jade-mcp/contract_test.go` is the authority; this table is for
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
| `jade.grep` | — (`query` or `queries`, enforced by the server) |
| `jade.history` | `path` |
| `jade.insert` | `path`, `text` |
| `jade.job_output` | `id` |
| `jade.job_status` | `id` |
| `jade.outline` | `path` |
| `jade.read_range` | — (`path` or `ranges`, enforced by the server) |
| `jade.references` | `path` |
| `jade.rename` | `newName`, `path` |
| `jade.replace_file` | `content`, `path` |
| `jade.replace_symbol` | `newCode`, `symbolId` |
| `jade.replace_text` | `newText`, `oldText`, `path` |
| `jade.retrieve` | `query` |
| `jade.revert` | `checkpointId` |
| `jade.run_command` | — |
| `jade.run_tests` | — |
| `jade.capabilities` | — |
| `jade.telemetry` | — |
| `jade.workspace_tree` | — |

## The edit contract

What every write tool guarantees, and the test that holds each guarantee.
`TestEditContractNamesOnlyTestsThatExist` fails if a test named here is
renamed or deleted, so the contract cannot weaken without this table
changing with it.

### Preconditions — checked before anything is written

| Guarantee | Held by |
|---|---|
| An edit given `expectedRevision` is refused if Jade's revision moved on | `TestApplyRejectsStaleRevision`, `TestReplaceSymbolRejectsStaleRevision` |
| An edit given `expectedDigest` is refused if the file changed since that read, by anyone | `TestAnEditIsRefusedWhenTheFileChangedSinceItsRead` |
| An anchor must match exactly once; an ambiguous one is refused | `TestReplaceTextRefusesAmbiguousAnchor`, `TestInsertRefusesAmbiguousAnchor`, `TestApplyRejectsAmbiguousAnchorBeforeWritingAnything` |
| An anchor that is not there is refused | `TestReplaceTextRefusesMissingAnchor`, `TestInsertRefusesMissingAnchorText` |
| No path outside the workspace is read or written, symlinks included | `TestEveryPathToolRefusesToLeaveTheWorkspace`, `TestEscapesAreRefusedAndNameTheRoot` |
| `create_file` never overwrites; `replace_file` never creates | `TestCreateFileRefusesToOverwriteExisting`, `TestReplaceFileRefusesToCreate` |
| An unknown `apply` op is refused before any edit is written | `TestApplyRejectsUnknownOpBeforeWriting` |

### Atomicity

| Guarantee | Held by |
|---|---|
| A single write is atomic, keeps permissions and follows symlinks | `TestFileKeepsPermissionsAndFollowsSymlinks` |
| `apply` lands every edit or none | `TestApplyRollsBackEverythingWhenOneEditFails` |
| A write across files — a language server's rename — lands in all or none | `TestFilesWritesAllOrNone` |
| Edits write only through the one write path | `TestEditsWriteOnlyThroughThisPackage` |

### Postconditions

| Guarantee | Held by |
|---|---|
| An edit advances the revision; `apply` advances it once | `TestApplyBumpsRevisionOnce`, `TestReplaceFileBumpsTheRevision` |
| The index reflects the write at once | `TestSymbolLocationsAreRecalculatedAfterWriteWithNoStaleCache` |
| The response leads with the revision transition and carries diagnostics | `TestEditLeadsWithRevisionAndSurfacesDiagnostics` |
| Files an `apply` touched are formatted when the repository declares a formatter | `TestApplyFormatsTouchedFiles` |
| A continuation handle cut before an edit is refused after it | `TestGrepContinueRefusedAfterAnEdit` |
| Two sessions on one workspace: an edit based on a read the other session invalidated is refused, neither loses the other's edit, and concurrent applies to different files both land | `TestTwoSessionsOnOneWorkspaceDoNotLoseEachOthersWork` |
| Every session's checkpoints see every write to the workspace | `TestEveryObserverOfARootIsTold` |

### Checkpoint and revert

| Guarantee | Held by |
|---|---|
| A checkpoint records git's HEAD | `TestCheckpointRecordsTheCommitItWasTakenAt` |
| Revert restores contents and existence — edited, deleted, created since — as a new revision | `TestRevertRestoresExistenceAndAdvancesTheRevision`, `TestRevertWithoutAnInterveningCommitRestoresFiles` |
| A revert that cannot restore every file changes nothing | `TestRevertThatCannotRestoreEverythingChangesNothing` |
| Revert refuses once a commit has landed since the checkpoint | `TestRevertRefusesAcrossACommitAndLeavesFilesAlone` |
| An unknown checkpoint is reported as not found | `TestRevertToAnUnknownCheckpointIsNotFound` |
| All of the above hold together in one scripted session through the MCP transport | `TestTheChangeTransactionHoldsAcrossAScriptedSession` |

Not guaranteed yet: that the revision also moves for changes made outside
Jade or by another session — each session counts its own edits, and only the
digest precondition sees the rest — and that `changes` attributes an edit to
the session that made it.

## Removed in 0.0.8

Deprecated in 0.0.7, when each description started with
`Deprecated, removed before 0.1.0:` and named its replacement; removed in
0.0.8, a breaking change stated in its release notes. The internal functions
behind them remain where other tools use them (`retrieve`, `context`, and
`apply`'s `replace_range` edit).

| Tool | Use instead | Evidence |
|---|---|---|
| `search` | `find` for a declaration by name, `grep` for text | 0 calls in 36 benchmark runs and 672 calls at home; overlaps both, and cannot find a field or a string |
| `search_nudge` | — | A harness hook, not an agent tool; never called |
| `repository_map` | `retrieve` | 0 calls; `retrieve` ranks files and symbols within the same kind of token budget |
| `read_symbol` | `find` | 8 calls at home, all 8 `not_found`; `find` returns the declaration and its body, now with `budget` |
| `replace_range` | `replace_text`, or `apply` with a `replace_range` edit | 0 calls; line numbers move under earlier edits, anchors do not |

Kept although rarely called, because they are the change transaction rather
than an overlap: `checkpoint`, `revert`, `changes`, `diff`, `history`,
`references`, `rename`, `job_status`, `job_output`, `events`. The benchmark's
tasks did not need them; that says the tasks were small, not that the tools
are redundant.

## Budgets and provenance — 0.0.5 design

*Implemented in 0.0.5 for `grep`, `find`, `references`, `read_range` (and its
`ranges`), `workspace_tree`, `history`, `diff` and
`job_output`; `cmd/jade-mcp/contract_test.go` holds each to it.* `outline`
stays whole — a file's declarations are a short list. `events` keeps its
`after` cursor, which already continues. Two conventions that every list- or
body-returning response will share. Both are additive: new optional
arguments and new response lines, so neither breaks a caller. The per-tool
size arguments they replace are deprecated in 0.0.5 and removed before 0.1.0.

### Budget and continuation

Today each tool has its own size argument — `limit` on `grep`, `find`,
`search`, `history` and `events`, `maxLines` on `find` and `read_symbol` —
and `read_range` cuts silently at 20,000 bytes in the middle of the file.
The replacement:

- **`budget`**, an optional integer in tokens, on every tool that returns a
  list or a body: `grep`, `find`, `search`, `retrieve`, `references`,
  `outline`, `read_range`, `read_symbol`, `workspace_tree`, `repository_map`,
  `context`, `history`, `events`, `changes`, `diff`, `job_output`. Tokens are
  counted as bytes / 4, the estimate the benchmark already uses. Each tool
  keeps today's effective default; a server-wide cap bounds the largest
  budget.
- **Cut at whole items**: a match, a declaration, a file entry, a line of a
  range. Never mid-line, never mid-body without saying so.
- **One closing line when cut**, with the true total and a handle:
  `shown 24 of 87 references · continue=c3f9a1`.
- **`continue`**, an optional string argument on the same tools: the same
  call's next page, under the same or a new `budget`. Other arguments are
  ignored when `continue` is given.
- **Handles are opaque and server-side**, kept per session, least recently
  used first out. A handle names the revision it was cut at; after the
  workspace moves, it is refused with both revisions named
  (`continue=c3f9a1 was cut at r12; the workspace is at r14 — repeat the
  call`), never answered from a different state.
- **`limit` and `maxLines` stay accepted** through 0.0.x and are read as a
  budget in items or lines. `context` on `grep` is not a size argument and
  stays.

### Provenance

An answer that can come from more than one backend, or be incomplete, says
which on one compact line — `exact · gopls · complete`. Three parts, each a
closed set defined by core. A backend reports its class and its limits;
core renders the words, so no backend chooses its own confidence wording.

| Part | Values |
|---|---|
| Certainty | `exact` (a compiler, language server or literal text match), `structural` (a tree-sitter parse), `approximate` (name matching in the text index), `text fallback` (heuristic scan, no grammar) |
| Source | the backend that answered: `gopls`, `rust-analyzer`, `tree-sitter`, `text index`, … — never a tool name or argument |
| Completeness | `complete`, `cut` (by the budget; a `continue` handle follows), `may be incomplete` (server still indexing, files skipped), `parse errors`, `stale` |

Where it goes: appended to the first line after ` · `, so the verdict or
identifier still leads — `12 references to Store · exact · gopls · complete`.
Tools whose answer has one possible source and cannot be partial — an exact
`read_range` within budget, `diff`, `checkpoint` — carry no provenance.
Edit responses keep `checked: <source>`, using the same source names.

In `JADE_JSON=1` output both conventions are new fields — `Provenance`
(`Certainty`, `Source`, `Completeness`) and `Continue` — added to the
response structs, not replacing any.

### Order of work

1. Provenance types and rendering in core; `references`, `find`, `outline`
   and `grep` first, since they already distinguish exact from approximate.
2. The continuation store and `budget` on `grep`, `find`, `references` and
   `read_range`, the tools the benchmark agents called most.
3. The remaining tools; `limit` and `maxLines` marked deprecated in their
   schema descriptions.
4. Contract tests: every list-returning tool accepts `budget` and
   `continue`; every provenance value comes from the closed sets.

## The 0.1.x stability promise

*Draft. Binding from the `v0.1.0` tag, for every 0.1.x release.* Until then
the stability policy above applies, and the five deprecated tools are removed
before 0.1.0.

Frozen for 0.1.x — a change to any of these waits for 0.2.0 and is called
out in its release notes:

| Surface | What is frozen |
|---|---|
| Tool names | Every tool in the 0.1.0 catalog, spelled `jade.<name>` or `jade_<name>` |
| Arguments | Required arguments stay required and no optional argument becomes required |
| Profiles | The names `core` and the full catalog, and that an unlisted tool stays callable |
| Budgets | `budget` in tokens (four bytes each), cutting at whole items; `continue=<handle>` returning the next page; a handle refused after an edit |
| Provenance | The certainty set (`exact`, `structural`, `approximate`, `text fallback`) and the completeness set (`complete`, `cut`, `may be incomplete`, `parse errors`, `stale`) |
| Validation outcomes | `passed`, `failed`, `unavailable`, `running`, `timed out` |
| Capability report | Its fields: per language the structure, server and server state, formatter and references provenance; git; validation commands; declared commands and kinds; providers |
| The edit contract | Every guarantee in *The edit contract* above, with its test |

Still free to change in 0.1.x: response wording and layout, symbol ID
spelling, the `.jade/*` file formats (with migration), telemetry fields, and
anything under `internal/`. New tools, new optional arguments, new provenance
sources and new capability-report fields may be added.

## What is explicitly not frozen

- **Response wording**, as above.
- **`.jade/commands.json` and `.jade/telemetry.jsonl` formats.** Both are
  local files, neither is a wire format, and telemetry in particular is
  expected to grow fields.
- **Symbol ID spelling** (`path::Name@line`). Treat it as opaque and obtain it
  from a response rather than constructing it.
- **Anything under `internal/`.** Go's own visibility rules already say so.
