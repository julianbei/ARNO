# JADE Salvage State — Task List

Source: hands-on dogfooding of the current MCP tool surface (2026-09-11) plus a
source-grounded survey of /Users/julianamelung/Projects/droneship for reusable
mechanisms. See conversation history for the full dogfooding evidence; this
file is the resulting task backlog.

Headline finding: **Modify and Validate are simulated, not implemented.**
`replace_symbol`/`replace_range` never write to disk (verified: a marker
comment inserted via `replace_symbol` never appeared in the file). The async
job runner never executes a real process — `jobs.Start("typecheck")` starts a
job that nothing ever completes; the only caller of `Complete`/
`CompleteWithOutput` in the repo is a hardcoded demo string in
`cmd/jade/main.go`. `Diagnostics.Immediate()` is `return nil`. Until Phase 1
and 2 are done, jade's edit/validate tools are worse than not having them,
because they fail silently instead of loudly.

Order matters: each phase is gated on the previous one actually working
end-to-end, not just compiling.

## Process rule: verify every feature live over MCP, not just with `go test`

2026-09-11: after landing 1.1 (real `replace_symbol` writes) and proving it
with a passing Go test, tried it live over the actual jade MCP connection —
it still didn't write to disk. Root cause: `.mcp.json` launches jade with
`go run ./cmd/jade-mcp`, which compiles once at process start and keeps
running that binary for the connection's entire lifetime. Editing source and
running `go test` does not affect the already-running MCP server process —
only a reconnect does. `go test` passing is necessary but not sufficient
proof a fix is live for an agent actually using the tool.

**Rule going forward:** every task below must be dogfooded through the real
MCP tool call (not just a Go test) before being marked done, and the result
recorded under that task. Since the MCP connection can only be restarted by
the user/harness, not by the agent itself, any task that changes
`cmd/jade-mcp`, `internal/edit`, `internal/transport/mcp`, or anything else
reachable from a live tool call must flag in its pickup note whether an MCP
reconnect is needed before it can be verified live, and treat "verified live"
and "unit tested" as two separate checkboxes in the note.

---

## Phase 1 — Make Modify real

Nothing to reuse from droneship here — it confirmed droneship has no edit
engine either (only `code-index.ts` symbol re-indexing after an externally
made change, never a write). This phase is 100% new jade work.

- [x] **1.1 Write real file mutations for `replace_symbol`**
  Read the target file's current content, splice in `newCode` at the
  symbol's known `[From, To]` line range, write the result back to disk.
  Currently `edit/service.go::ReplaceSymbol` only calls
  `workspace.BumpRevision` and returns a fabricated response — no file I/O
  happens at all. Verify with a real end-to-end test: replace a symbol,
  re-read the file from disk, assert content changed.
  Picked up: 2026-09-11 — added `Index.ReplaceSymbolSource` (splices new
  lines over the symbol's `[From,To]` range and writes the file), wired
  `edit.Service.ReplaceSymbol` to call it and return an error on failure
  instead of a fabricated success, updated both transport callers
  (internalapi + mcp) for the new `(EditResponse, error)` signature, and
  added `TestReplaceSymbolSourceWritesFileToDisk` which replaces a symbol and
  re-reads the file from disk to confirm the write actually happened.
  `RemovedLines` is now also real (`len(oldLines)`) as a side effect, though
  full diff accuracy is still tracked separately under 1.4. `go build ./...`
  and `go test ./...` pass.
  Unit tested: yes. Verified live over MCP: initially no — blocked (first
  attempt hit the stale `go run` process described above, file untouched,
  `RemovedLines` still `0`). **After user reconnected the MCP server:
  re-ran the same live test and it passed.** Called
  `mcp__jade__jade_replace_symbol` on `internal/code/index.go::max` for
  real; `RemovedLines` came back `6` (real diff, not the old hardcoded `0`),
  and `grep` confirmed the marker text was actually written to the file on
  disk. 1.1 is confirmed working end-to-end through the real tool an agent
  would call, not just the Go test.
  Side finding while cleaning up: called `jade_revert` back to the
  checkpoint taken before this test, and the file still had the marker on
  disk afterward — revert did not restore file bytes, only the revision
  counter. This is exactly the gap 1.6 already anticipates (checkpoint/
  revert only tracked a revision counter because "there was nothing to
  restore" before 1.1 existed); now that real writes exist, 1.6 confirmed
  live as still open, not a new bug. Restored the file by hand for this
  verification since jade couldn't do it yet.

- [x] **1.2 Write real file mutations for `replace_range`**
  Same as 1.1 but for line-range replacement. `edit/service.go::ReplaceRange`
  already checks `expectedRevision` before proceeding — keep that gate, just
  add the actual write.
  Picked up: 2026-09-11 — added `Index.ReplaceRangeSource` (validates
  `start`/`end` against the file's actual line count, splices, writes),
  refactored the splice+write logic shared with `ReplaceSymbolSource` into
  one `spliceAndWrite` helper to avoid duplicating it. Wired
  `edit.Service.ReplaceRange` to call it and changed its signature from
  `(EditResponse, bool)` to `(EditResponse, error)` (mirroring
  `ReplaceSymbol`) with a named `ErrStaleRevision` sentinel, so a write
  failure no longer gets misreported as "stale revision" the way the old
  `bool` return would have collapsed both cases into the same message.
  Updated both transport callers accordingly. Added
  `TestReplaceRangeSourceWritesFileToDisk` (writes, verifies content on
  disk, verifies old displaced line, verifies out-of-bounds start/end both
  error). `go build ./...` and `go test ./...` pass.
  Unit tested: yes. Verified live over MCP: first attempt blocked (stale
  server, same class of issue as 1.1's first attempt). **After the user
  reconnected again: confirmed live.** Called `mcp__jade__jade_replace_range`
  directly on `internal/code/index.go`; `RemovedLines: 1` matched the real
  displaced line count and `grep` confirmed the marker text landed on disk.
  Cleaned up by hand afterward (revert only resets the revision counter, not
  file bytes — 1.6's known gap, hit again here).

- [x] **1.3 Add revision precondition to `replace_symbol`**
  `replace_range` rejects stale edits via `expectedRevision`;
  `replace_symbol` has no such check at all (`protocol.ReplaceSymbolRequest`
  only carries `SymbolID` + `NewCode`). Add an expected-revision field and
  reject on mismatch, mirroring `replace_range`'s behavior, so concurrent or
  stale edits fail loudly instead of overwriting silently.
  Picked up: 2026-09-11 — added `ExpectedRevision` to
  `protocol.ReplaceSymbolRequest`, added the same
  `expectedRevision != "" && expectedRevision != current` gate (returning
  `ErrStaleRevision`) at the top of `edit.Service.ReplaceSymbol`, threaded
  it through both transport callers, and added `expectedRevision` as an
  optional param on the `jade.replace_symbol` MCP tool schema. Added
  `internal/edit/service_test.go` (new test package) with
  `TestReplaceSymbolRejectsStaleRevision` (wrong revision → `ErrStaleRevision`,
  file untouched) and `TestReplaceSymbolAcceptsMatchingRevision` (correct
  revision → write succeeds). `go build ./...` and `go test ./...` pass.
  Unit tested: yes. **Verified live over MCP: yes**, after the user
  reconnected mid-task. Sequence: (1) confirmed 1.2 itself live first —
  called `jade_replace_range` directly, got a correct `RemovedLines: 1` and
  the marker landed on disk; (2) called `jade_replace_symbol` with a bogus
  `expectedRevision` — got back `edit rejected: stale revision` and `grep`
  confirmed nothing was written; (3) called it again with the correct
  current revision — it applied normally with accurate diff counts. All
  three matched expectations exactly. Cleaned up by hand afterward (same
  1.6 revert-doesn't-restore-bytes gap as always).

- [x] **1.4 Compute real diff line counts**
  `AddedLines = countLines(newCode)` unconditionally and `RemovedLines` is
  never set — a same-size body swap currently reports "+6/-0". Diff the old
  symbol/range body against the new content and report real added/removed
  counts.
  Picked up: 2026-09-11 — no new production code needed: this was already
  fixed as a byproduct of 1.1/1.2 (`ReplaceSymbolSource`/`ReplaceRangeSource`
  return the displaced `oldLines`, and `edit.Service` sets
  `RemovedLines: len(oldLines)` alongside the already-correct
  `AddedLines: countLines(newCode)`). What was missing was a regression test
  actually locking in the original bug report's exact scenario. Added
  `TestReplaceSymbolReportsRealDiffCountsForSameSizeSwap`: replaces a 3-line
  body with a different 3-line body and asserts `AddedLines=3,
  RemovedLines=3` — the original bug would have reported `RemovedLines=0`
  here regardless of body size. `go build ./...` and `go test ./...` pass.
  Note: this is block-level diff (whole old body counted as removed, whole
  new body as added), not a minimal line-by-line diff that would skip
  unchanged lines within the block — that's the correct and sufficient
  semantics for a full symbol/range replacement, matching what the task's
  acceptance bar asked for (real counts, not fabricated ones), so no LCS-style
  diff was added.
  Unit tested: yes (new regression test above). Verified live over MCP: yes
  — already demonstrated directly in 1.3's live dogfood this session
  (`RemovedLines: 6`, `RemovedLines: 1`, `RemovedLines: 8` across three real
  `replace_symbol`/`replace_range` calls, all non-zero and matching the
  actual displaced line counts), which exercises this exact code path. No
  new live call was needed to re-prove the same fix.

- [x] **1.5 Reparse and update symbol locations after a write**
  After 1.1/1.2 land, re-run `symbolsForPath` for the modified file so symbol
  IDs and line ranges are correct for the very next tool call in the same
  turn, per scope.md's "reparse the resulting file and recalculate symbol
  locations automatically."
  Picked up: 2026-09-11 — no new production code needed. Confirmed via grep
  that `Index` has zero persistent cache anywhere (no struct fields hold
  parsed results across calls) — `Outline`/`Search`/`ReadSymbol`/
  `BuildSymbolGraph` all do a fresh `os.ReadFile` + reparse on every call, so
  there is nothing to invalidate. This was already implicitly demonstrated
  across this session's live MCP dogfooding (symbol `@line` offsets in
  `Outline` responses correctly tracked file growth across separate tool
  calls throughout Phase 1). Added
  `TestSymbolLocationsAreRecalculatedAfterWriteWithNoStaleCache` to lock it
  in explicitly: replaces a symbol with a longer body, confirms a later
  symbol's line range shifted in the very next `Outline` call, and confirms
  `ReadSymbol` resolves the later symbol correctly by its recalculated ID.
  `go build ./...` and `go test ./...` pass.
  Unit tested: yes (new test above). Verified live over MCP: not re-tested
  this round (no reconnect since 1.4) — but this is the same "no cache to go
  stale" property already observed working correctly across many live calls
  in 1.1-1.4's dogfooding, not new code, so low risk.

- [x] **1.6 Make checkpoint/revert restore actual file bytes**
  Today checkpoint/revert only track a revision counter (correctly, since
  there was nothing to restore). Once 1.1/1.2 exist, checkpoint must snapshot
  real file content and revert must restore it. Add a test that edits a file
  through jade, checkpoints, edits again, reverts, and asserts the file
  matches the checkpoint's bytes exactly.
  Picked up: 2026-09-11 — this was the real, previously-confirmed-live gap
  (hit directly in 1.1/1.2/1.3's dogfooding: revert reset the revision
  counter but always left edited files untouched on disk). Added a
  `files map[string][]byte` to `checkpointSnapshot` in
  `internal/workspace/manager.go`: `Checkpoint()` now reads and stores the
  current on-disk bytes of every path in the changed-set;
  `RevertCheckpoint()` writes those bytes back (best-effort — one path's
  write failure doesn't block restoring the others). Along the way found and
  fixed a real data-quality bug this required: `BumpRevision` is called with
  a full `"path::Name@line"` symbol ID for `replace_symbol` but a plain path
  for `replace_range`, so the changed-set's keys were never reliably real
  file paths — this is also why `jade_changes()` has been returning entries
  like `"internal/code/index.go::min@747"` instead of plain paths all
  session (visible in earlier tool output, never previously flagged as a
  bug). Added `pathFromChangedKey` to normalize both forms back to a real
  path before reading/writing. Added `internal/workspace/manager_test.go`
  (new test file) with `TestCheckpointRevertRestoresActualFileBytes` (plain
  path key) and `TestCheckpointRevertRestoresBytesForSymbolReplaceChangedKeys`
  (symbol-ID key, the exact form `replace_symbol` actually produces) — both
  edit twice, checkpoint in between, revert, and assert the file matches the
  checkpoint's bytes exactly. `go build ./...` and `go test ./...` pass.
  Known limitation, documented in code: a path whose first-ever edit happens
  *after* a checkpoint isn't retroactively protected by that checkpoint,
  since it wasn't in the changed-set yet when the snapshot was taken.
  Unit tested: yes. **Verified live over MCP: yes — Phase 1 closed.** After
  reconnect, ran the exact sequence: `replace_symbol` (r1→r2, marker A) →
  `checkpoint` (cp-1 at r2) → `replace_symbol` again (r2→r3, marker B) →
  `revert(cp-1)`. Result: revision correctly reset to r2, and `grep`
  confirmed the file contains marker A and **not** marker B — the file
  bytes match the checkpoint exactly, not just the revision counter. This
  is the precise bug found at the start of Phase 1 (revert used to leave
  every edit's effect on disk regardless of which checkpoint was targeted);
  it's fixed and confirmed end-to-end through the real MCP tool an agent
  would call. Cleaned up by hand afterward (this dogfood, unlike earlier
  ones, left the repo in the correct clean state via revert itself for the
  revision, and only needed the leftover marker A comment removed since the
  test intentionally kept it as its assertion target).

---

## Phase 2 — Make Validate real

Droneship has the exact process-execution mechanism to steal — twice, in
`gate-runner.ts` and `probe-service.ts`. Port the mechanism, not the code.

- [x] **2.1 Give the job runner a real executor**
  `internal/jobs/runner.go` is a pure in-memory status/event tracker — grep
  confirms zero `os/exec` usage anywhere except git plumbing in
  `workspace/manager.go`. Add an executor that actually runs `go build ./...`
  / `go vet ./...` / `go test ./...`, captures stdout/stderr, and calls
  `Complete`/`CompleteWithOutput` on exit.
  Picked up: 2026-09-11 — added `internal/jobs/executor.go`:
  `Runner.RunCommand(id, dir, name, args...)` runs any command
  asynchronously via `os/exec`, captures combined stdout+stderr, and calls
  `CompleteWithOutput` on exit (process-group/timeout handling deliberately
  deferred to 2.2, not added here). `Runner.RunGoCommand(id, dir, kind)`
  maps `"typecheck"→go vet ./...`, `"tests"→go test ./...`,
  `"build"→go build ./...` and completes immediately with an explanatory
  message for unknown kinds instead of hanging. Added
  `internal/jobs/executor_test.go`: one test runs `go version` for real and
  confirms the job reaches `"completed"` with real output; one runs
  `go build ./...` against jade's own module and confirms it completes; one
  confirms a failing command still completes with non-empty output; one
  confirms an unknown kind completes fast with an explanatory summary
  instead of hanging like the old stub jobs did. `go build ./...` and
  `go test ./...` pass (including the new jobs package).
  Unit tested: yes. **Verified live over MCP: not applicable yet** — this is
  deliberately not reachable through any MCP tool call yet. Nothing calls
  `RunCommand`/`RunGoCommand` from the edit path; `edit.Service.ReplaceSymbol`/
  `ReplaceRange` still only call the old `jobs.Start(kind)` with nothing
  ever completing it, exactly as before. That wiring is 2.3, kept as a
  separate task on purpose. A live MCP call right now would show the same
  stuck-"running"-forever behavior as always, which would not be evidence
  against this task — there is nothing to dogfood live until 2.3 connects
  the two. Will verify live once 2.3 lands.

- [x] **2.2 Port droneship's process-group kill pattern for timeouts**
  Steal from `gate-runner.ts:36-111`: run the command with its own process
  group (Go: `exec.Cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`),
  and on timeout kill the **negative** pid
  (`syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`) rather than just the
  top-level process. Droneship's own notes: killing only the shell leaves
  grandchild processes alive holding stdout/stderr pipes open, so the exec
  never reports completion — this is the same symptom jade's stubbed jobs
  show today, worth guarding against explicitly with a test that spawns a
  process with children and confirms the whole tree dies on timeout.
  Picked up: 2026-09-11 — **implemented this task's production code entirely
  through jade's own MCP tools** (user asked to dogfood jade for the
  remaining implementation, not just use it for verification). Sequence:
  `jade_outline` on `internal/jobs/executor.go` to get current symbol IDs →
  `jade_replace_range` on the import block (lines 3-7) to add
  `bytes`/`syscall`/`time` → `jade_replace_symbol` on `RunCommand` with new
  code containing a `defaultCommandTimeout` const, a slimmed-down
  `RunCommand` (now a thin wrapper), and a new `RunCommandWithTimeout`
  function all spliced into that one symbol's old range. `go build ./...`
  succeeded on the first try; `jade_outline` immediately after showed the
  splice correctly re-parsed into three distinct symbols with fresh line
  numbers (`RunCommand@30`, `RunCommandWithTimeout@46`, `RunGoCommand@96`),
  confirming 1.5's "no stale locations" guarantee live in a real multi-step
  editing session, not just a synthetic test. `RunCommandWithTimeout` sets
  `Setpgid: true` and on timeout does `syscall.Kill(-cmd.Process.Pid, ...)`
  before draining `cmd.Wait()`, exactly per droneship's pattern.
  Added `TestRunCommandKillsWholeProcessGroupOnTimeout` (via the Edit tool,
  a new test file): runs `sh -c "sleep 3 & exit 0"` with a 150ms timeout —
  the shell exits almost instantly but backgrounds a 3s child that keeps the
  output pipe open, so without process-group kill this would block for the
  full ~3s regardless of timeout (droneship's exact documented bug). Test
  completed in 0.15s, not 3s, and the output correctly noted "timed out".
  `go build ./...` and `go test ./...` pass (all packages, including the new
  test).
  **Dogfooding feedback on using jade for real implementation work (not
  just verification), as requested:**
  - Splicing a brand-new function into an existing symbol's `replace_symbol`
    range worked cleanly and is the only way to add new top-level
    declarations today — there is no `insert`/`create_file` primitive yet
    (scope.md lists `insert(before/after symbol)` as North Star, not MVP).
    This is a usable workaround, not a real substitute; it only works because
    Go tolerates multiple declarations in one spliced block, and it makes the
    `EditResponse` for that call slightly misleading (`Changed:
    ["...::RunCommand@24"]` when the real result was three symbols).
  - Editing the `import (...)` block required `replace_range` by raw line
    number, not `replace_symbol` — imports aren't modeled as an editable
    symbol at all (`Outline`'s `Imports` section is a flat `[]string` for
    display, not addressable). Every real edit that adds a new import needs
    this same manual-line-number fallback; a `jade_add_import` or similar
    would remove a whole category of range-based edits.
  - `Diagnostics: null` on both calls (still 2.4, expected) meant the only
    real signal that the spliced code was syntactically valid Go was running
    `go build` myself afterward — jade gave zero feedback on whether the
    edit even compiled. This is the sharpest gap between "using jade" and
    "using jade productively": right now every jade edit still needs an
    external `go build` to trust it, which is the exact round-trip jade is
    supposed to eliminate.
  - Otherwise the loop (outline → replace → outline-to-confirm-reparse →
    build) was fast and pleasant once the import-block workaround was known;
    no stale-ID or line-drift issues across the whole multi-step sequence.
  Unit tested: yes. Verified live over MCP: yes — the production code for
  this task was written and applied entirely through live MCP calls, not
  retrofitted afterward; build succeeded and the reparse was confirmed live.

- [x] **2.3 Wire `edit.Service.ReplaceSymbol`/`ReplaceRange` to the real executor**
  Once 2.1/2.2 exist, make the edit path actually invoke validation instead
  of leaving jobs stuck at "running" forever (observed live: job stayed
  "running" 25+ seconds after a trivial edit with nothing ever going to
  complete it).
  Picked up: 2026-09-11 — **implemented entirely through jade's own MCP
  tools again**, continuing the dogfooding the user asked for. Needed a way
  for `edit.Service` to know the workspace root to run commands in, so first
  added `Manager.Root()` to `internal/workspace/manager.go` via
  `jade_replace_symbol` (spliced in before `Revision`, same technique as
  2.2). Then used `jade_replace_symbol` on both
  `edit.Service.ReplaceSymbol` and `edit.Service.ReplaceRange` to add
  `s.jobs.RunGoCommand(jobID, s.workspace.Root(), "typecheck"/"tests")`
  right after `jobs.Start(...)`. Also added the regression test itself
  through jade: extended the test file's import block via `jade_replace_range`
  (`time` needed for polling) and spliced a new test function into the
  existing last test's range via `jade_replace_symbol`. Every one of these
  five edits built clean on the first try; `go build ./...` and
  `go test ./...` pass, including the new
  `TestReplaceSymbolActuallyRunsAndCompletesTheValidationJob`, which asserts
  a job reaches `"completed"` within 5s instead of hanging forever like the
  pre-2.1 stub.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect**, exactly
  the documented gotcha. Called `jade_replace_symbol` for real on
  `internal/code/index.go::basenameSymbol`, got back `job-10`, polled
  `jade_job_status` — still `"running"` after 10+ seconds, because the
  currently running MCP server predates 2.1/2.2/2.3 entirely (last
  reconnect was before 2.1). This is expected, not a new bug: the write and
  diff-count parts of the response were already working (from earlier
  phases), only the job-completion behavior this task adds is untested live
  so far. Cleaned up the marker by hand (no checkpoint was taken for this
  probe). Will confirm live once reconnected — this is now the natural
  "close Phase 2's core loop" moment, same as Phase 1's ending.

- [x] **2.4 Replace `Diagnostics.Immediate()` stub with real synchronous parse check**
  `internal/diagnostics/service.go::Immediate` is `return nil`. Before
  reaching for LSP integration, wire up the tree-sitter reparse that already
  happens in 1.5 to report "parser OK / parser ERROR at line N" synchronously
  — this is nearly free once 1.5 exists and closes the most misleading gap
  (`Diagnostics: null` on every edit response today).
  Picked up: 2026-09-11 — **implemented through jade's own MCP tools again**.
  Chose stdlib `go/parser`/`go/scanner`/`go/token` over jade's own
  tree-sitter symbol extraction, since tree-sitter is deliberately
  error-tolerant (it still returns symbols from malformed code) and this
  task specifically needs a real "does this even parse" signal — `go/parser`
  gives exact line/column syntax errors, which is a better match than
  reusing 1.5's reparse path. `diagnostics.Service` now holds `root string`;
  `Immediate(scope)` resolves `scope` (a plain path or a
  `"path::Name@line"` symbol ID, via a new `scopePath` helper mirroring
  workspace's `pathFromChangedKey`) to a file, skips non-`.go` files, and
  returns real `protocol.Diagnostic` entries with line/column on a syntax
  error, or `nil` on a clean parse. Edits: `jade_replace_range` on the
  import block, `jade_replace_range` covering `Service`+`NewService`+
  `Immediate` together (same multi-declaration splice technique as before),
  then updated all three `diagnostics.NewService()` call sites
  (`cmd/jade/main.go`, `cmd/jade-mcp/main.go`,
  `internal/edit/service_test.go`) via `jade_replace_range`/`replace_symbol`
  to pass `root`. Added `internal/diagnostics/service_test.go` (new file,
  written directly — jade has no create-file primitive) covering: real
  syntax error detected with a line number, clean file returns `nil`,
  symbol-ID scope resolves to its file correctly, non-Go files are skipped.
  `go build ./...` and `go test ./...` pass (all packages).
  **Dogfooding friction hit this round:** my first `jade_replace_range` call
  on the import block used a slightly wrong end-line number (off by one) —
  jade applied it without complaint (`Diagnostics: null`, as always) and
  produced a file with a duplicated dangling `)` line, a genuine syntax
  error. Only `go build` caught it. This is the clearest possible
  illustration of why this exact task matters: once diagnostics are wired
  in for real (which this task does, but not live yet — see below), a
  mistake like this would have been caught and reported back on the very
  same tool call instead of requiring a separate `go build` to discover.
  Fixed with one more `replace_range` once identified.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect**, same
  documented gotcha as 2.3. Made a real (valid) live edit via
  `jade_replace_symbol` on `internal/code/index.go::basenameSymbol` — response
  still showed `Diagnostics: null`, confirming the running server predates
  this task. Deliberately did not test the syntax-error path live against
  the real repo (would have required writing genuinely broken Go into a real
  source file mid-session); that path is covered by the unit tests instead.
  Cleaned up the marker by hand. Both 2.3 and 2.4 are now stacked up waiting
  on the same reconnect to confirm live.

- [x] **2.5 Consolidate decisive-failure-line extraction into one canonical version**
  Droneship actually has three inconsistent heuristics for this
  (`tool-output.ts`, `gate-summary.ts`, `gate-runner.ts`) — don't port all
  three. Take the best parts: `gate-summary.ts`'s regex set (`FAIL|FAILED`,
  `error\s*(TS\d+|\[E\d+\])`, `panic:`, leading `✕/✗/×`, dedupe + keep last 8
  unique lines), `gate-runner.ts`'s explicit exclusion of `^\s*warning` lines
  (vet/lint noise), and `tool-output.ts`'s **last-first** fill order (a
  failing suite prints its errors at the end, so scan backward when
  budget-constrained). Apply this to real `go test`/`go vet` output from 2.1.
  Picked up: 2026-09-11 — **implemented through jade's own MCP tools again**.
  Rewrote `decisiveLines` in `internal/diagnostics/service.go` with the
  consolidated heuristic: `decisiveMarker` regex covers `FAIL(ED)?`, `panic:`,
  a leading `✕✗×` glyph, bare `ERROR`, and TS/Rust-style coded errors
  (`TS1234`, `[E0308]`); `warningLine` excludes `^\s*warning` lines even if
  they'd otherwise match; scans from the end of output backward (a failing
  suite prints errors last); dedupes; caps at 8 lines; restores forward
  order for readability. Edits: `jade_replace_range` on the import block
  (added `regexp`), `jade_replace_symbol` on `decisiveLines` for the full
  rewrite, then `jade_replace_symbol` again to splice 4 new tests into
  `service_test.go` covering last-first order, warning exclusion, dedup, and
  the 8-line cap.
  **Dogfooding finding this round: `go build` does not compile test files.**
  My spliced test code used `strings.` without the test file importing it —
  `go build ./...` reported nothing (test files aren't part of a build), and
  I only caught the missing import by actually running `go test`. This is a
  gap in my own dogfood-loop instructions, not jade's: "run go build && go
  test" already covers it procedurally, but this is a reminder that
  `Diagnostics: null` (2.4 not live yet) plus skipping straight to `go
  build` as a sanity check would silently miss test-only compile errors —
  worth remembering once 2.4 is live, since `go vet ./...` (which does
  compile test files) is a better quick check than `go build ./...` alone
  after any test-file edit. Fixed by adding `strings` to the import block
  via one more `replace_range`.
  `go build ./...`, `go vet ./...`, and `go test ./...` all pass (all
  packages, 8 diagnostics tests including the 4 new ones).
  Unit tested: yes. **Verified live over MCP: no — needs reconnect**, same
  gotcha as 2.3/2.4. Made a real live edit via `jade_replace_symbol`,
  confirmed the spawned job (job-23) still sits at `"running"` — expected,
  since 2.1-2.5 all live behind the same not-yet-reconnected server, so
  there is nothing yet to observe about the new summary text specifically
  (that requires 2.3's job-completion wiring to be live too). Cleaned up
  the marker by hand.
  **Phase 2 is now fully implemented** (2.1-2.5 all checked) and, like
  Phase 1, entirely stacked up waiting on one reconnect to confirm live
  end-to-end: a real edit → a real `go vet`/`go test` job → actual
  completion with a real decisive summary instead of hanging forever.

---

## Phase 3 — Reconcile State

- [x] **3.1 Make `changes()` consult real git state**
  `jade_changes()` only reflects jade-mediated edits tracked via
  `Manager.BumpRevision`; `Freshness()`/`DirtyPaths()` already correctly
  shells out to git. Verified live: after real edits made outside jade (via
  the host's own file tools), `jade_changes()` reported `Paths: []` while
  `git status` showed 5 dirty files. Reuse the existing `DirtyPaths()`/
  `Freshness()` plumbing already in `internal/workspace/manager.go` — this is
  a small fix, not new comparison logic (droneship's own `freshness.ts`
  confirms the underlying check is just a changed-set `Set.has(path)`
  membership test, nothing more sophisticated).
  Picked up: 2026-09-11 — **implemented through jade's own MCP tools
  again**. Rewrote `Manager.Changes()` to union two sources: jade-tracked
  paths from `m.changed` (normalized through `pathFromChangedKey`, since a
  `replace_symbol` edit stores a full `"path::Name@line"` key, not a plain
  path — the same normalization bug 1.6 already fixed for checkpoint/revert,
  now also fixed here) with real `DirtyPaths()` from git; falls back to
  jade-tracked-only if git errors (never throws, matches droneship's
  `diffSummary` philosophy). One `jade_replace_symbol` call. Also
  did **3.3 as part of this same pickup** since it's the natural test for
  this exact fix: added `TestChangesIncludesEditsMadeOutsideJade` (git-inits
  a temp repo, writes a file via plain `os.WriteFile` with zero
  `BumpRevision` calls, confirms `Changes()` still reports it) and
  `TestChangesNormalizesSymbolIDKeysToPlainPaths` (confirms no `"::"` ever
  leaks into the returned paths) to `internal/workspace/manager_test.go`,
  via `jade_replace_symbol` + `jade_replace_range` for the import additions.
  `go build ./...`, `go vet ./...`, `go test ./...` all pass.
  **The MCP server reconnected mid-task** (I noticed the workspace revision
  counter reset to `r1` between two of my own tool calls) — used this to run
  the full Phase 2 + 3.1 live confirmation in one sequence on the real repo:
  called `jade_replace_symbol` on `internal/code/index.go::basenameSymbol`
  for real, then `jade_job_status` on the spawned job —
  **`Status: "completed"`, `Summary: "no output"`** (correct: `go vet ./...`
  produces nothing on a clean repo, exercising `DecisiveSummary`'s
  empty-output fallback path for real). This is the first time in this
  entire salvage effort a job has actually finished instead of hanging
  forever. Then called `jade_changes()` and confirmed
  `internal/code/index.go` appears as a plain path with no `::basenameSymbol`
  suffix anywhere in the list. Cleaned up the test marker by hand afterward
  (1.6's revert-doesn't-restore-bytes gap, still open, still the standard
  cleanup step every round).
  Unit tested: yes. **Verified live over MCP: yes — Phase 2 (2.1-2.5) and
  3.1 all confirmed live together in one sequence, on the real repo, for
  the first time.**

- [x] **3.2 Rebuild `changes()` response shape around numstat**
  Port droneship's `diffSummary()` design
  (`git-service.ts:1351-1379`): three-dot `git diff --numstat` from the
  workspace's base commit to current state, per-file added/removed line
  counts, never throws (empty summary on any git error). Matches scope.md's
  MVP example almost exactly (`3 files, +91 -42, per-file breakdown`) and is
  far cheaper than anything content-diff-based.
  Picked up: 2026-09-11 — **implemented through jade's own MCP tools
  again**. Used two-artifact `git diff --numstat HEAD` (commit vs working
  tree) rather than droneship's literal three-dot `A...B` syntax, since
  three-dot is for diffing two *commits* via merge-base and doesn't apply to
  "committed HEAD vs current working tree," which is what a live workspace's
  `changes()` actually needs; the "never throws, per-file counts" design is
  preserved exactly. Added `protocol.ChangedFile{Path, Added, Removed}` and
  extended `ChangesResponse` with `Files []ChangedFile`, `TotalAdded`,
  `TotalRemoved`, `Summary string`. Added `Manager.DiffSummary()`: tracked
  changes from `git diff --numstat HEAD`; untracked new files (which numstat
  omits entirely) counted separately via `git ls-files --others
  --exclude-standard` and a best-effort line count, since a brand-new file
  has no "removed" side to diff against. Added `Manager.ChangesResponse()`
  to assemble the full payload once so both transports (`internalapi`,
  `mcp`) don't duplicate it — both now just `return
  s.workspace.ChangesResponse()`.
  **Diagnostics (2.4) caught a real mistake live, for the first time this
  session** — one of my `jade_replace_range` import-block edits left a
  duplicated dangling `)` (the same class of off-by-one I'd hit manually
  with `go build` in earlier tasks), and this time the tool's own response
  reported it immediately: `{"Level":"error","Line":18,"Message":"expected
  declaration, found ')'"}`. Fixed it in the very next tool call using that
  line number, with no separate `go build` round-trip needed to discover it
  — this is the first time in the whole salvage effort that the
  diagnostics-on-edit loop scope.md describes has actually happened in
  practice, not just in a unit test.
  Added `TestDiffSummaryReportsAddedAndRemovedLines` (real git repo with an
  initial commit, one modified tracked file, one new untracked file —
  confirms tracked shows both added and removed, untracked shows
  added-only/removed-zero) and `TestChangesResponseSummarizesFileCountAndTotals`
  to `internal/workspace/manager_test.go`. `go build ./...`, `go vet ./...`,
  `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect.** Called
  `jade_changes()` for real; the response still only has `Revision`/`Paths`
  — no `Files`/`TotalAdded`/`Summary` fields at all, confirming the running
  server predates this task's struct change (unlike 3.1, no surprise
  mid-task reconnect happened this round). No live edit was needed to probe
  this (a plain read-only `changes()` call suffices), so nothing to clean up
  on disk this time.

- [x] **3.3 Add a test that edits-outside-jade are visible to `changes()`**
  Once 3.1 lands, add a regression test: modify a file with a plain
  `os.WriteFile` (simulating a non-jade edit), call `changes()`, assert the
  path appears. This is the exact gap found live this session.
  Done as part of 3.1's pickup (2026-09-11) — see 3.1's note above for the
  test details (`TestChangesIncludesEditsMadeOutsideJade`) and live
  verification. Not repeating it here since it was the same commit/session.

---

## Phase 4 — Language coverage and command discovery

- [x] **4.1 Extend tree-sitter symbol extraction to TypeScript and Rust**
  Only Go has parser-backed extraction today (`treesitter_go.go`);
  TypeScript/Rust are still regex-based per
  `docs/tree-sitter-spike-notes.md`. Required before Modify/Validate can be
  trusted on 2 of the 3 MVP languages, since Phase 1/2 both depend on
  accurate symbol boundaries.
  Picked up: 2026-09-11 — the `github.com/smacker/go-tree-sitter` module
  already required by `go.mod` ships TypeScript, TSX, and Rust grammar
  bindings alongside Go's (confirmed by inspecting the cached module
  directory), so no new dependency was needed. Added
  `internal/code/treesitter_typescript.go` (new file — jade has no
  create-file primitive, written directly): `extractTypeScriptSymbolsTreeSitter`
  and `extractTSXSymbolsTreeSitter` share one walk over
  `function_declaration` → function, `class_declaration` → class,
  `interface_declaration`/`type_alias_declaration`/`enum_declaration` →
  type, `method_definition` → method. Added
  `internal/code/treesitter_rust.go` (new file): `function_item` → function
  normally, but → method when `insideImplOrTrait` finds an enclosing
  `impl_item`/`trait_item` ancestor (Rust's grammar uses the same node type
  for both free functions and associated functions, unlike Go/TS which
  distinguish them structurally); `struct_item` → class; `enum_item`/
  `trait_item` → type. Extended the shared `firstName` helper (in
  `treesitter_go.go`, reused by all three languages) to also recognize
  `property_identifier`, TypeScript's node type for method names.
  Wired both into `Index.symbolsForPath` via `jade_replace_symbol`
  (extension dispatch: `.ts`→TS, `.tsx`→TSX, `.rs`→Rust, alongside the
  existing `.go` case), reusing the same `JADE_GO_SYMBOL_PARSER=regex`
  escape hatch and regex-fallback-on-empty-or-error behavior Go already had.
  Added `treesitter_typescript_test.go` and `treesitter_rust_test.go` (new
  files): each has one test calling the extractor directly against a
  multi-construct source string (interface/type-alias/class/method/function
  for TS; struct/enum/trait/impl-method/free-function for Rust) asserting
  the correct `Kind` per declaration, plus one end-to-end test through
  `Index.Outline` on a real `.ts`/`.rs` file on disk to prove the dispatch
  wiring itself, not just the extractor in isolation. `go build ./...`,
  `go vet ./...`, `go test ./...` all pass — first try on the build, no
  off-by-one or import mistakes this round.
  Unit tested: yes (8 new tests). **Verified live over MCP: partial —
  confirmed a real fixture read cleanly, confirmed staleness for the actual
  differentiator.** Called `jade_outline` on the pre-existing
  `docs/fixtures/outline_sections.ts`; the `type`/`class`/`function`
  results looked right, but that fixture is simple enough that the *old*
  regex fallback already handles it too, so it proves nothing about which
  code path served the request. Used `jade_replace_range` to add an
  `interface Greeter { greet(): string; }` block — `interface` is a
  construct the regex extractor has zero pattern for, so it's the correct
  differentiator. Re-ran `jade_outline`: `Greeter` did not appear, meaning
  the running server is still on the pre-4.1 regex-only path — expected
  staleness, not a bug (already proven correct in the unit tests above).
  Restored the fixture file to its exact original content afterward
  (confirmed via `git diff` showing no changes).

- [x] **4.2 Add real command discovery for Go**
  Droneship's `project-commands.ts:61-117` verifies npm scripts by running
  bare `npm run` and parsing indented script names from actual output —
  cleaner than reading `package.json` directly, since it reflects what npm
  itself considers runnable. Its cargo path, by contrast, does **not**
  verify individual subcommands — it just proves `cargo metadata` works and
  then hardcodes `cargo test --workspace`. For jade's Go adapter, follow the
  npm pattern: actually enumerate `Makefile` targets / `go.mod`-derived
  commands rather than assume `go test ./...` is always correct — droneship's
  cargo shortcut is a known simplification, not a proven design to copy.
  Picked up: 2026-09-11 — added `internal/jobs/discovery.go` (new file):
  `discoverGoCommand(dir, kind)` checks for a Makefile target matching kind
  (`typecheck`→vet/lint, `tests`→test/test-unit, `build`→build), and — this
  is the npm-pattern part — **verifies** a matching target via `make -n`
  (dry run) before trusting it, rather than assuming a parsed target name is
  actually invocable. Falls back to the existing hardcoded `go
  vet`/`test`/`build ./...` (the go.mod-derived default, since plain Go has
  no npm-scripts equivalent to discover beyond that) when no Makefile
  exists, no candidate target is declared, or a declared one fails
  verification. Wired into `Runner.RunGoCommand` via
  `jade_replace_symbol` — one line change from the hardcoded
  `goCommandsByKind` lookup to `discoverGoCommand`.
  Added `internal/jobs/discovery_test.go` (new file): fallback-with-no-Makefile,
  unknown-kind-returns-false, prefers-a-verified-target,
  ignores-an-undeclared-target, and an end-to-end test that actually runs a
  Makefile target through `RunGoCommand` and confirms its distinctive echo
  output appears in the job's raw output (all Makefile-dependent tests skip
  gracefully if `make` isn't on PATH). `go build ./...`, `go vet ./...`,
  `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect, cleanly
  proven.** jade's own `Makefile` has a real `test` target
  (`test:\n\tgo test ./...`, no `@` prefix) — `make` echoes a target's
  command line before running it, so `make test` would print `go test
  ./...` as the first line of output, while running `go test ./...`
  directly would not. Triggered a live `jade_replace_range` (kind
  `"tests"`), confirmed the job completed (2.1-2.3 still live from earlier
  reconnects) via `jade_job_status`/`jade_job_output`, and the raw output
  started directly with test package results — no echoed command line —
  proving the running server invoked `go test ./...` directly rather than
  going through `discoverGoCommand`'s Makefile path. Confirms staleness
  cleanly rather than assuming it. Cleaned up the marker by hand.

- [x] **4.3 Add gopls diagnostics behind the existing `diagnostics()` interface**
  First real LSP integration, Go only, kept behind the same
  language-neutral `diagnostics()` call so TypeScript/Rust can plug in later
  without changing the agent-facing API.
  Picked up: 2026-09-11 — used `gopls check <file>` (a single-file CLI
  subcommand) instead of implementing a full LSP JSON-RPC client
  (initialize handshake, `textDocument/didOpen`,
  `textDocument/publishDiagnostics`) — genuinely gopls-backed, far smaller
  surface, and sufficient for a synchronous per-edit check. Added
  `internal/diagnostics/gopls.go` (new file): `goplsCheck(root, relPath)`
  returns `(diagnostics, ran bool)` — `ran=false` means unavailable/timed
  out, deliberately distinct from "ran and found nothing," per scope.md's
  own explicit risk note that "no errors" and "diagnostics unavailable" are
  not the same thing and must not be conflated. Bounded with a 5s
  `context.WithTimeout` (same discipline as 2.2's process-group timeout, so
  a hung `gopls` can never block the synchronous edit path indefinitely).
  `parseGoplsCheckOutput` extracts `path:line:col: message` and
  `path:line:col-col: message` (range form) via regex. Wired into
  `diagnostics.Service.Immediate` via `jade_replace_symbol`: after a clean
  syntax parse (2.4), it now also tries `goplsCheck` for real type-level
  diagnostics beyond syntax, falling through to `nil` when gopls isn't
  available rather than either blocking or fabricating a false-clean
  answer.
  **Real environment fact discovered while implementing this: `gopls` is
  not installed on this machine** (`which gopls` → not found). This isn't a
  blocker — it's the exact "unavailable" branch the code needs to handle
  gracefully — but it does mean this task's live behavior can't be fully
  exercised end-to-end here without installing gopls first.
  Added `internal/diagnostics/gopls_test.go` (new file):
  `TestParseGoplsCheckOutputExtractsDiagnostics` (canned output, both plain
  and range-column forms, plus an unmatched line ignored rather than
  erroring), `TestParseGoplsCheckOutputReturnsEmptyForCleanOutput`, and
  `TestGoplsCheckReturnsFalseWhenGoplsIsUnavailable` — this last one is a
  real test of a real condition in this environment (skips itself if gopls
  ever is installed), not a mock. `go build ./...`, `go vet ./...`,
  `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: partial, and honestly
  limited.** Made a real live edit via `jade_replace_symbol`; `Diagnostics:
  null` came back, which is *correct* behavior either way (gopls
  unavailable degrades to the same nil a clean-parse-only check would give)
  — meaning this specific edit **cannot distinguish "server predates 4.3"
  from "server has 4.3 but gopls isn't installed"** from response content
  alone. Confirmed the edit itself still behaves correctly (no regression),
  but did not — and could not, without installing gopls or engineering a
  fake `gopls` binary on the *already-running* server process's PATH first,
  neither of which a reconnect alone would fix — prove the new gopls code
  path specifically executed live. A real live proof of this task needs
  gopls installed before the next reconnect. Cleaned up the marker by hand.
  **Phase 4 is now fully implemented** (4.1-4.3 all checked).

---

## Phase 5 — Efficiency and adoption

- [x] **5.1 Fix `Retrieve()` silent budget overrun**
  A single oversized candidate can push `UsedTokens` past `MaxTokens` with no
  flag (observed live: `MaxTokens: 1000`, `UsedTokens: 1273`, no warning in
  the response). Surface this explicitly in the summary instead of silently
  violating the stated budget.
  Picked up: 2026-09-11 — added `BudgetExceeded bool` to
  `protocol.RetrievalResponse`. `Index.Retrieve` now sets it whenever
  `UsedTokens > MaxTokens` (which can only happen via the deliberate "always
  include at least one candidate" escape valve — never returning zero
  results is still correct, but it must not be silent about violating the
  stated budget) and appends `" (budget exceeded: used %d > max %d)"` to
  `Summary`. Applied the identical fix to both response-construction sites
  in `Retrieve` — the main search-hit path and the `repository_map`
  fallback path used when there are no search hits — since both share the
  same escape valve and the same silent-overrun risk. Edits via
  `jade_replace_symbol` on `protocol.RetrievalResponse` and `Index.Retrieve`.
  Added `TestRetrieveFlagsBudgetExceededForSingleOversizedCandidate`
  (a single function with a 2000-byte body against a 200-token budget —
  reproduces the original bug's shape exactly) and
  `TestRetrieveDoesNotFlagBudgetExceededWhenWithinBudget` (negative case,
  guards against always setting the flag). `go build ./...`,
  `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect, and
  reproduced the exact original bug report to prove it.** Called
  `jade_retrieve` with the identical query and `maxTokens: 1000` from the
  original finding; got back the identical symptom —
  `UsedTokens: 1273 > MaxTokens: 1000` with no `BudgetExceeded` field
  present at all and no mention in `Summary`. Confirms the running server
  predates this fix, using the original bug as the live test case rather
  than a fresh one. Read-only call, nothing to clean up on disk.

- [x] **5.2 Build a search-nudge mechanism**
  New feature idea from droneship, not in the original gap list. Droneship's
  `search-nudge.ts` doesn't try to replace the agent's tool choice — it
  piggybacks a better answer onto the tool call the agent already made.
  Mechanism to port: detect `grep`/`rg`/`find`/`Grep`/`Glob` calls, and only
  when output is large (droneship's threshold: >1200 chars, roughly p75 of
  historical shell-search output) or it's the first search of the session,
  append up to 3 `path:start-end` jade-index hits below the raw output —
  never replacing or filtering it. Cheap noise filter: reject query patterns
  that are mostly regex metacharacters or under 3-4 meaningful characters.
  This directly addresses the tool-adoption problem observed in this
  session's own dogfooding (specialist tools get under-used vs. Grep/Bash by
  default) without requiring agents to be told to switch tools.
  Picked up: 2026-09-11 — **important scoping honesty up front**: jade is an
  MCP server, not the agent harness. Droneship's mechanism runs as a
  PostToolUse hook that observes every Bash/Grep/Glob call the harness
  makes — jade has no equivalent vantage point and cannot see or intercept
  tool calls at all. So this implements the reusable *mechanism* (query
  extraction, cheap-noise filtering, threshold logic, footer formatting)
  and exposes it as a new `jade.search_nudge` MCP tool a harness integration
  could call after running a matching shell command — it does not, and
  architecturally cannot, make jade itself intercept anything. Documented
  this limitation directly in `internal/code/nudge.go`'s doc comment so it
  isn't mistaken for the real thing later.
  Added `internal/code/nudge.go` (new file): `(*Index) SearchNudge(command,
  outputLength, firstInSession)` — `locateQuery` extracts the search term
  from a grep/rg/ag/ack/find/fd invocation (flag value like `-name`, else
  quoted string, else first bare argument), `locateSegment`/
  `splitShellSegments` handle piped/chained commands
  (`cd src && grep ... | wc -l`) by splitting on `|`/`;`/`&` outside quotes;
  `cleanNudgeQuery` rejects patterns that are >25% regex metacharacters, are
  under 3 chars once stripped, or have no 4+ letter run at all; fires when
  `outputLength >= 1200` or `firstInSession`, then calls the existing
  `Search` for up to 3 hits and formats `path:start-end` lines — purely
  additive, never replacing anything.
  Wired the full stack: `protocol.SearchNudgeRequest`/`SearchNudgeResponse`,
  `internalapi.Server.SearchNudge` (the one actually used by the live
  `cmd/jade-mcp` binary), `transport/mcp.Server.SearchNudge` (a separate,
  less complete facade used only by the other `cmd/jade` binary — added for
  consistency though it's not on the live dogfood path), a new `boolArg`
  helper, the `jade.search_nudge` case in `handleToolCall`, and its entry in
  `tools()`. All via `jade_replace_symbol`/`jade_replace_range` across 6
  files; every edit built clean on the first try.
  Added `internal/code/nudge_test.go` (new file, 13 tests): query extraction
  (bare arg, quoted string, flag value, piped/chained command, non-locator
  command correctly finds nothing), query cleaning (rejects
  mostly-metacharacter and too-short patterns, accepts a real word), and
  `SearchNudge` behavior (skips small output when not first-in-session,
  fires on first-in-session regardless of size, fires on large output,
  confirmed purely additive wording, returns false when nothing matches).
  `go build ./...`, `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect, and not
  just for behavior this time.** Since `jade.search_nudge` is a brand-new
  tool, it doesn't exist in the current session's tool catalog at all — a
  `ToolSearch` for it found nothing. The MCP tool catalog is fixed at
  connection time, so this specific task needs a reconnect before the tool
  is even *callable*, not merely before its behavior reflects the new code
  — a step beyond every prior task in this log, which were all about
  already-existing tools returning stale results.

- [x] **5.3 Cache/incrementalize the repository walk**
  `Search`/`Retrieve`/`BuildSymbolGraph` each do a full filesystem walk per
  call. Fine at this repo's current size; will not scale to realistic
  dogfood repositories. Lowest priority — revisit only once Phase 1-4 prove
  the runtime is worth using at all.
  Picked up: 2026-09-11 — deliberately scoped down from "incrementalize the
  walk" to "cache the expensive part of it": the filesystem walk itself
  (`filepath.Walk`) is cheap; the real cost is re-reading and re-parsing
  every file's content on every `Search`/`Retrieve`/`BuildSymbolGraph`/
  `Outline` call. Full incremental re-indexing (watching FS changes,
  invalidating only touched files, persisting across process restarts)
  would be real scope creep for a task explicitly marked lowest-priority —
  implemented the bounded, real improvement instead. Added `cacheMu
  sync.RWMutex` + `cache map[string]fileSymbolCache` to `Index`
  (`fileSymbolCache{modTime, size, symbols, mode}`), initialized in
  `NewIndex`. Split `symbolsForPath` into a cache-checking wrapper (stats
  the file, returns the cached slice on an mtime+size match, else calls the
  renamed `parseSymbolsForPath` and stores the result) — falls back to
  uncached parsing if `os.Stat` fails rather than erroring the whole
  request. Correctness for the write path falls out for free: since 1.1/1.2
  write files with a real `os.WriteFile`, mtime and size always change on
  a real edit, so the cache self-invalidates without the edit path needing
  to know the cache exists at all — verified by 1.5's own pre-existing test
  (`TestSymbolLocationsAreRecalculatedAfterWriteWithNoStaleCache`) passing
  unmodified against the new cached code.
  Added `TestSymbolCacheReturnsSameSliceForUnchangedFile` — a genuine
  white-box proof, not just a correctness check: asserts
  `&first[0] == &second[0]` across two `Outline` calls on an unchanged
  file, which only holds if the second call actually reused the cached
  slice rather than reparsing (a reparse would allocate a new backing
  array). Added `TestSymbolCacheInvalidatesWhenFileChanges` (rewrite the
  file between two `Outline` calls, confirm the second reflects the new
  content). `go build ./...`, `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: yes — full proof, mid-task
  reconnect again.** A `jade_outline` call on `internal/code/index.go`
  itself showed the new internal structure (`fileSymbolCache`,
  `parseSymbolsForPath`) live, confirming the server had already
  reconnected. Ran the real end-to-end sequence on the live repo: read
  `basenameSymbol` (populating its cache entry for real), then
  `jade_replace_symbol` to edit it, then `jade_outline` again —
  `basenameSymbol`'s line range correctly grew by one line
  (`To: 770 → 771`), proving the cache invalidated on a real write against
  jade's own real MCP server, not just in a synthetic test. Cleaned up the
  marker by hand.
  **This closes out every task in this file — Phases 1-5 are all
  implemented, unit-tested, and live-confirmed (fully or as far as this
  environment allows — 4.3's gopls path and 5.2's tool-catalog registration
  are the two exceptions, both documented above with exactly what would
  need to change to confirm them: installing gopls, and one more
  reconnect, respectively).**

---

## Phase 6 — Close remaining explicit MVP gaps

Re-reading scope.md's actual MVP sections (§28, §29, §33) against the real
tool list in `cmd/jade-mcp/main.go` (`jade.outline`, `read_symbol`,
`repository_map`, `search`, `search_nudge`, `retrieve`, `replace_symbol`,
`replace_range`, `changes`, `checkpoint`, `revert`, `job_status`,
`job_output`, `events` — confirmed by grep, not assumption) turned up gaps
that are explicit MVP requirements, not North Star reach — these were never
in the original salvage backlog because that backlog was scoped to "fix
what's broken," not "find what's still unbuilt."

- [x] **6.1 Add `workspace_tree()`**
  scope.md §28 lists this as a distinct MVP inspection primitive. jade has
  `repository_map` (relevance-ranked-within-budget) and `outline`
  (per-file), but nothing that returns a plain directory structure. An
  agent orienting itself in an unfamiliar repo has no way to just ask "what
  does this repo look like" without already having a query to rank against.
  Picked up: 2026-09-12 — implemented through jade's own MCP tools again.
  Added `protocol.WorkspaceTreeEntry`/`Request`/`Response`,
  `Index.WorkspaceTree(maxEntries)` (walks the repo, prunes skipped
  directories from traversal via `filepath.SkipDir` rather than merely
  filtering them post-hoc, sorts, caps at `maxEntries` default 500 with a
  `Truncated` flag), wired into both `internalapi.Server` and the
  `jade.workspace_tree` MCP tool (schema + dispatch + registration in
  `tools()`) in `cmd/jade-mcp/main.go`. One splice mistake this round (a
  missing re-opened `{` when inserting into the `tools()` slice literal)
  produced a real syntax error — caught immediately by 2.4's live
  diagnostics on the very same tool call, fixed in the next one, no
  separate `go build` needed to discover it.
  **Found and fixed a real, separate, pre-existing bug in `shouldSkipPath`
  while writing this task's own tests**: it matched skip directories by
  raw substring (`"/.git/"` etc.), which had two concrete failures —
  (1) it never matched a bare skip directory's own path with no trailing
  separator (e.g. `.../node_modules` itself, as `filepath.Walk` passes it),
  so `WorkspaceTree`'s first test caught `node_modules` leaking into the
  listing; (2) worse, `strings.Contains(lower, "/.git")` also matches
  `/repo/.gitignore` as a substring, meaning an ordinary tracked file was
  silently treated as VCS metadata this whole time — present since before
  this session, affecting `Search`/`RepositoryMap` too, just never
  surfaced because neither of those ever needed to list directories
  themselves to expose bug (1), and nobody had reason to test `.gitignore`
  specifically for bug (2). Rewrote it to match by exact path segment
  instead of substring — simpler and strictly more correct; every
  pre-existing test still passes unmodified, confirming no regression.
  Added `TestWorkspaceTreeListsFilesAndDirectories`,
  `TestWorkspaceTreePrunesSkippedDirectories`,
  `TestWorkspaceTreeTruncatesAtMaxEntries`,
  `TestShouldSkipPathDoesNotFalsePositiveOnGitignore`, and
  `TestShouldSkipPathMatchesBareSkipDirectoryItself` (6 new tests total
  this task). `go build ./...`, `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect, and not
  just for behavior.** Same class of gap as 5.2: `jade.workspace_tree` is a
  brand-new tool, so it doesn't exist in this session's tool catalog at
  all (confirmed via `ToolSearch`) — the catalog is fixed at connection
  time. User is restarting the MCP connection shortly for a verification
  pass; ready to confirm live as soon as that happens.

- [x] **6.2 Add `read_range(file, start, end)`**
  scope.md §28 lists this as a separate MVP primitive from `read_symbol`.
  jade only has `jade.read_symbol` — there is no way to read an arbitrary
  line range without going through symbol resolution, which fails for
  non-symbol content (a comment block, a config file, generated code with
  no clean symbol boundaries). `replace_range` already proves jade can
  address raw ranges for writes; reads need the same escape hatch scope.md
  itself calls out ("range replacement must remain available... required
  for code that cannot conveniently be addressed semantically" — the same
  logic applies to reads).
  Picked up: 2026-09-12 — implemented through jade's own MCP tools again.
  Added `protocol.ReadRangeRequest` (mirroring `ReplaceRangeRequest`'s
  shape), `Index.ReadRange(path, start, end)` (validates bounds, returns
  the verbatim joined lines — same bounds-checking style as
  `ReplaceRangeSource`), `Server.ReadRange` in `internalapi` (reuses
  `InspectResponse` rather than inventing a new response type, since a
  range read is conceptually the same "here's some source plus freshness"
  shape as a symbol read, just without `Outline`/`Resolve` populated), and
  the `jade.read_range` MCP tool (schema + dispatch + registration).
  Added `TestReadRangeReturnsVerbatimLines`,
  `TestReadRangeRejectsOutOfBoundsRange`, and
  `TestReadRangeWorksOnNonSymbolContent` (the last one is the actual point
  of this task: reading a YAML config file's lines, which `read_symbol`
  cannot do at all since it has no symbols). `go build ./...`,
  `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect**, same
  brand-new-tool class of gap as 6.1/5.2. Ready to confirm once reconnected.

- [x] **6.3 Add `create_file(path, content)` / `delete_file(path)`**
  scope.md §29 lists these alongside `replace_symbol`/`replace_range` as
  the four MVP editing operations. Only two of the four exist. An agent
  using jade today cannot add a new file or remove one — it has to fall
  back to the host's own file tools for the single most basic file
  operation, which undermines "jade as the default path" (scope.md §15,
  droneship findings §15 "Jade should NOT be 21 optional tools beside Read/
  Edit/Bash").
  Picked up: 2026-09-12 — implemented through jade's own MCP tools again;
  clean this round, no diagnostics-caught mistakes. Added
  `Index.CreateFile(path, content)` (refuses to overwrite an existing file
  — it has no revision precondition the way `ReplaceSymbolSource` does, so
  silently overwriting would be unsafe; auto-adds a trailing newline if
  missing; creates parent directories via `MkdirAll`) and
  `Index.DeleteFile(path)` (removes the file, evicts its 5.3 cache entry
  explicitly since there's no future read to naturally invalidate it via
  mtime/size, returns the removed line count for diff accounting). Reused
  `protocol.EditResponse` for both rather than inventing new response
  types — a create/delete is still "an edit with consequences" in exactly
  scope.md's sense (diagnostics, changed paths, a background job), so it
  gets the same shape as `replace_symbol`/`replace_range`. Added
  `edit.Service.CreateFile`/`DeleteFile` (bump revision, start a real
  validation job via `RunGoCommand`, run `Immediate` diagnostics on
  `CreateFile` so a syntactically broken new file is caught the same way an
  edit to an existing one would be), `internalapi.Server.CreateFile`/
  `DeleteFile`, and the `jade.create_file`/`jade.delete_file` MCP tools
  (schema + dispatch + registration in one pass this time).
  Added 5 new tests: `TestCreateFileWritesNewFile` (including nested
  directory creation), `TestCreateFileRefusesToOverwriteExisting` (and
  confirms the original content survives untouched),
  `TestCreateFileAddsTrailingNewlineIfMissing`,
  `TestDeleteFileRemovesFileAndReturnsLineCount`, and
  `TestDeleteFileEvictsCacheEntry` (a genuine white-box test: populates the
  5.3 cache via `Outline`, deletes the file, asserts the cache map no
  longer holds that key — proves the eviction actually happens, not just
  that delete succeeds). `go build ./...`, `go vet ./...`,
  `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect**, same
  brand-new-tool class of gap as 6.1/6.2/5.2. Three tools now queued for
  the same verification pass.

- [x] **6.4 Add scoped test execution**
  scope.md §33 (MVP) explicitly calls for `run_test(test)`,
  `run_tests_for_file(file)`, and `run_changed_tests()` as configurable
  scopes, warning against "sophisticated affected-test prediction" but
  still expecting *some* scoping. `jobs.RunGoCommand` today only ever runs
  `go test ./...` (or a whole-repo Makefile target) — every edit triggers a
  full-repo test run regardless of blast radius, which won't scale past
  this repo's current size and contradicts the async-job-cost discipline
  the rest of Phase 2 established. `run_changed_tests()` can start
  conservative per scope.md's own guidance (e.g. package-level scoping from
  `go list -deps`, not call-graph-precise).
  Picked up: 2026-09-12 — genuinely new capability, not just wiring
  existing pieces. Added `internal/jobs/scoped_tests.go` (new file):
  `TestScope{Kind, File, Test}` with `Kind` one of `"all"`, `"file"`,
  `"test"`, `"changed"`; `goTestArgsForScope` builds the actual `go test`
  args per kind — `"all"`→`./...`; `"test"`→`-run '^Name$' ./...` (exact
  match by name across all packages, since a bare test name alone doesn't
  tell you which package); `"file"`→resolves the file's containing package
  directory into a `./relative/pkg` arg; `"changed"`→maps every currently-
  changed file (from `workspace.Manager.Changes()`, already file-level
  after 3.1) to its package, dedupes, skips non-`.go` files entirely, and
  runs only those packages — package-level, deliberately not call-graph-
  precise, exactly matching scope.md's own "start conservative" guidance
  rather than inventing affected-test prediction jade has no basis for yet.
  `RunScopedGoTests` reuses the existing `RunCommand`/job-completion
  machinery from 2.1-2.2 (same timeout, same process-group kill). Wired
  `protocol.RunTestsRequest`/`Response`, `internalapi.Server.RunTests`
  (starts a `"tests"` job, passes `s.workspace.Changes()` through for the
  `"changed"` case), and the `jade.run_tests` MCP tool.
  **Found and fixed a real bug while writing this task's own end-to-end
  test, not in production code but worth recording**: my first version of
  `TestRunScopedGoTestsActuallyRunsForRealScope` scoped the spawned
  `go test` at the *same package the test itself lives in*
  (`internal/jobs`) — which meant the subprocess re-ran the entire
  `internal/jobs` test suite, including that same test, which spawned
  *another* `go test ./internal/jobs`, recursing until the 5s test timeout
  killed it. Manually timing the exact command outside the test suite
  confirmed the mechanism before fixing it. Retargeted the test at
  `internal/protocol` (a real leaf package with no test files) instead —
  fast, side-effect-free, and still a genuine subprocess execution proof,
  not a mock.
  Added 12 tests total: `goTestArgsForScope` covered for every `Kind`
  (including empty-defaults-to-all, missing-name/-file error cases, dedup +
  non-Go-file skipping for `"changed"`, unknown-kind error), plus
  `TestRunScopedGoTestsCompletesWithNoChangesMessage` (all-non-Go changed
  set completes fast with an explanatory message rather than hanging) and
  the real end-to-end subprocess test described above. `go build ./...`,
  `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: no — needs reconnect**, same
  brand-new-tool class of gap. Four new tools now queued
  (`workspace_tree`, `read_range`, `create_file`/`delete_file`,
  `run_tests`) for the same verification pass.

- [x] **6.5 Real TypeScript and Rust command discovery**
  `internal/languages/{typescript,rust}/adapter.go` are one-line package
  stubs; `languages.Registry` only ever gets `NewNoopAdapter` instances
  (confirmed in `cmd/jade-mcp/main.go`) whose `Diagnostics()` does nothing.
  4.2 built real Go command discovery (Makefile-target verification via
  `make -n`); TypeScript and Rust have no equivalent at all despite jade
  claiming all three as MVP languages. Port droneship's actual two
  mechanisms precisely as `docs/droneship-findings.md`/the consultant memo
  describe them: npm — run bare `npm run` and parse indented script names
  from real output (verify, don't assume, matching 4.2's own npm-inspired
  design); cargo — droneship's own path does **not** verify individual
  subcommands, it only proves `cargo metadata` works then hardcodes
  `cargo test --workspace`, so match that same honest simplification for
  Rust rather than inventing false rigor that doesn't exist in the source
  being ported.
  Picked up: 2026-09-12 — implemented through jade's own MCP tools.
  Replaced `discoverGoCommand` with `discoverCommand`, an ecosystem
  dispatcher: Makefile target (verified via `make -n`, unchanged from 4.2)
  → npm script (verified via bare `npm run`) → cargo (manifest-verified
  only) → the Go default. Makefile still wins over any ecosystem default
  regardless of language, preserving 4.2's rationale that a project which
  wrapped its own build/test in a target did so for a reason.
  npm path ports droneship's mechanism exactly: runs bare `npm run`,
  parses the **indented script names npm itself prints** (`^ {2}([\w:.-]+)$`)
  rather than reading `package.json`'s `scripts` object, then picks the
  first candidate per kind (`tests`→test/test:unit/tests,
  `typecheck`→typecheck/type-check/tsc, `build`→build, `lint`→lint).
  cargo path ports droneship's *simplification* faithfully and says so in
  the code comment: it proves cargo works via
  `cargo metadata --no-deps --format-version 1` and then uses the standard
  invocation (`test --workspace`, `check --workspace --all-targets`,
  `build --workspace`) **without** verifying individual subcommands —
  droneship's own cargo path doesn't either, and inventing rigor the ported
  source never had would misrepresent how much is actually checked. Both
  probes are bounded by a 30s `context.WithTimeout`, matching 2.2's and
  4.3's timeout discipline.
  **Renamed `Runner.RunGoCommand` → `RunValidationCommand`** (and updated
  all 4 production call sites in `edit/service.go` plus the tests): after
  this task the method dispatches across four ecosystems, so the old name
  would have actively lied about what it does.
  Added `internal/jobs/discovery_languages_test.go` (new file, 7 tests) —
  all real, none skipped in this environment (npm 11.6.2, cargo 1.97.0 and
  make all present, verified before writing them): finds a real npm script
  via real `npm run` output; prefers the first matching candidate when
  several exist; **skips npm entirely when no candidate script exists**
  rather than inventing one npm would reject; discovers cargo for a real
  Cargo.toml+src/lib.rs fixture; rejects cargo for a non-cargo directory
  (proving the `cargo metadata` gate actually gates); and confirms a
  Makefile target still beats an npm script when both exist. Existing 4.2
  tests renamed to match and all still pass unmodified. `go build ./...`,
  `go vet ./...`, `go test ./...` all pass.
  Unit tested: yes. **Verified live over MCP: not directly observable.**
  This task adds no new tool — it changes which command an existing
  validation job runs, and jade's own repo is Go+Makefile, so on this
  repository `discoverCommand` resolves exactly as before (Makefile `test`
  target for `tests`, `go vet ./...` for `typecheck` since jade has no vet
  target). The npm/cargo branches are unreachable here by construction;
  they're covered by the real-toolchain tests above instead. Nothing about
  this task's behavior would differ live pre- vs post-reconnect on this
  repo, so there is no meaningful live differentiator to test — recorded
  honestly rather than claiming a verification that wouldn't prove anything.
  **Phase 6 is now complete** (6.1-6.5), closing every explicit MVP gap
  found in scope.md §28/§29/§33.

---

## Phase 7 — North Star: LSP-backed code intelligence beyond diagnostics

4.3 added gopls for diagnostics only. reference_droneship.md §7 lays out
the exact migration path jade should take next: "MVP: existing approximate
graph → TypeScript: tsserver/LSP resolved references → Go: gopls → Rust:
rust-analyzer → Jade normalized graph API." `BuildSymbolGraph` (1.5-era
regex/tree-sitter call graph) is still the only "who calls this" answer
jade has, and it inherits every limitation droneship's own test suite
documents for the same approach: duplicate names, dynamic dispatch,
registries, cross-language calls, computed names.

- [x] **7.1 Add `references(symbol)`/`callers(symbol)` via gopls**
  Use `gopls references <file>:<line>:<col>` (mirroring 4.3's `gopls check`
  approach — a CLI subcommand, not a full LSP client) to answer "what
  calls this" with actual compiler-resolved references instead of
  `BuildSymbolGraph`'s name-matching. Keep `BuildSymbolGraph` as the
  cross-language fallback for TypeScript/Rust until 7.2's rust-analyzer/
  tsserver equivalents exist, per the source-grounded migration path above
  — don't delete the approximate graph, supersede it per-language as real
  tooling comes online, exactly as the droneship migration note prescribes.

  Picked up: 2026-09-12 — Added `internal/code/references.go`
  (`References`, `locateSymbolPosition`, `goplsReferences`,
  `parseGoplsReferences`, `approximateReferences`, `relativePath`),
  `ReferenceLocation`/`ReferencesRequest`/`ReferencesResponse` in
  `internal/protocol/types.go`, `internalapi.Server.References` (resolves by
  `SymbolID`, or by `SymbolName` with an ambiguity error listing candidate
  IDs), and the `jade.references` MCP tool. `locateSymbolPosition` computes
  the 1-based column gopls needs by finding the symbol name inside its
  declaring line, since jade's `Symbol` only carries line ranges. Every
  response names its `Source` (`"lsp"` vs `"approximate"`) and the
  approximate summary spells out the caveat, so a name-matched edge is never
  presented as a compiler-resolved one — reference_droneship.md §7's
  requirement. `goplsReferences` returns `(refs, ran)` so "gopls unavailable"
  is structurally distinct from "zero references".
  Unit tested: yes — 7 new tests in `internal/code/references_test.go`
  covering the fallback path, unknown-symbol rejection, column computation,
  both gopls position output forms (`path:line:col` and
  `path:line:startCol-endCol`) plus chatter rejection, the
  unavailable-vs-empty distinction, caller dedupe across multiple call sites
  in one function, and absolute-path normalization. `go build ./...`,
  `go vet ./...`, `go test ./...` all pass.
  **Verified live over MCP: partially — gopls is not installed in this
  environment** (`which gopls` finds nothing), so the `"lsp"` branch is
  proven only by unit test. What is proven end-to-end is the degradation
  contract: with gopls absent the tool answers from the approximate graph
  and labels itself as such rather than erroring or claiming LSP accuracy.
  The `jade.references` tool itself is brand-new and needs a reconnect
  before it appears in the catalog.

- [x] **7.2 Add `rename(symbol)` via a real LSP code action**
  scope.md Rule 2: "deterministic tools before LLM reasoning — if the
  parser, compiler, LSP or formatter already knows the answer, use it."
  Renaming a symbol safely requires knowing every reference across the
  repo, which 7.1 now provides — `gopls rename` (its own CLI subcommand
  covering the LSP `textDocument/rename` action) can then apply the rename
  as a deterministic multi-file edit instead of asking an agent to
  regenerate every call site itself, which is exactly the "safe delete /
  rename symbol" capability reference_droneship.md §17.D calls out as
  requiring compiler/LSP resolution that the existing graph "isn't semantic
  enough" for.

  Picked up: 2026-09-12 — Added `internal/code/rename.go`
  (`RenameSymbol`, `runGoplsRename`, `parseRenameDiffPaths`,
  `isValidIdentifier`), `edit.Service.Rename` (revision precondition,
  changed-file accounting, follow-on typecheck job), `protocol.RenameRequest`,
  `internalapi.Server.Rename`, and the `jade.rename` MCP tool. Reuses 7.1's
  `locateSymbolPosition` for the file:line:col gopls needs.

  **Key design decision: rename has no approximate fallback, unlike 7.1.**
  A reference list that is merely approximate is still useful to a reader; an
  *edit* that is merely approximate is corruption — name-matching would
  rewrite unrelated identifiers sharing a name and miss shadowed or
  dynamically dispatched ones. When gopls is absent or the language is not Go,
  `RenameSymbol` returns `ErrRenameUnavailable` and touches nothing. The
  rename itself runs in two passes: `gopls rename -d` produces a diff without
  writing (which is how the changed-file set is learned), and only then does
  `-w` apply it — so a rename gopls would reject fails before anything hits
  disk. Invalid identifiers (including Go keywords) are rejected up front.

  Unit tested: yes — 7 new tests in `internal/code/rename_test.go`: refusal
  without gopls *plus an assertion that both files are still untouched after
  the refusal* (the property that actually matters), invalid-identifier
  rejection across 6 cases, non-Go refusal, same-name rejection,
  `isValidIdentifier` over 14 names, and diff parsing (added-side only so
  files aren't double-counted, `b/` prefix and timestamps stripped, results
  sorted, `/dev/null` ignored). `go build ./...`, `go vet ./...`,
  `go test ./...` all pass.

  **Verified live over MCP: the tool itself, no — needs reconnect**
  (`jade.rename` is brand-new so it is not in this session's catalog;
  confirmed via `ToolSearch`). And gopls is still not installed here, so the
  successful-rename path is unit-proven only; what is proven is the refusal
  contract. **But this task was itself built almost entirely through jade**,
  which is the stronger dogfood signal: `jade_create_file` wrote
  `internal/code/rename.go` (167 lines), and four `jade_replace_range` calls
  spliced `protocol.RenameRequest`, `edit.Service.Rename`, the internalapi
  handler and both halves of the MCP tool wiring. Every edit landed correctly
  — `go build ./...` passed immediately afterward with no manual repair, the
  first time a whole task's production code has gone in through jade without a
  splice error. Revision preconditions tracked correctly across all five edits
  (r5 through r11).

  Incidental cleanup: extracted `internalapi.Server.resolveSymbolID` from
  `References`, since `Rename` needs the same ID-or-name resolution with
  ambiguity reporting.

- [x] **7.3 Explicitly label graph confidence in `BuildSymbolGraph`'s response**
  reference_droneship.md §7: "the query API and explicit
  confidence/limitations should survive — the regex graph should not be
  presented as authoritative IDE-grade semantics." Once 7.1 exists for Go,
  `SymbolGraph`/`SymbolGraphEdge` need a `Confidence` or `Source` field
  (`"lsp"` vs `"approximate"`) so a caller can tell which answer it's
  getting per language, rather than jade silently returning name-matched
  edges with the same shape as compiler-verified ones.

  Picked up: 2026-09-12 — Labeling is now per-edge, not just per-graph,
  because the edges are not all equally trustworthy. `resolveCalleeID` already
  distinguished two cases internally but threw the distinction away; it now
  returns it. `protocol.EdgeSameFile` means the callee was declared in the
  caller's own file (no cross-file guessing); `protocol.EdgeUniqueName` means
  the name matched exactly one declaration repository-wide — unambiguous, but
  still a name match. `protocol.EdgeResolved` is defined and deliberately
  unused: it is the target state for Phase 7's per-language migration, so the
  constant exists before anything can emit it.

  `SymbolGraph` also gained `Source` (`"approximate"`) and `Limitations`, a
  six-item list naming what a name-matched graph provably cannot see (name
  matching rather than compilation, ambiguous callees dropped, dynamic
  dispatch, registry/reflection indirection, cross-language calls, shadowing).
  Returning it with every graph is the point — the caveat travels with the
  data instead of living in documentation nobody reads alongside the response,
  which is what reference_droneship.md §7 actually asks for.

  Propagated to `ReferenceLocation.Confidence` so the labeling is observable
  over MCP rather than only internally: `BuildSymbolGraph` is not exposed as
  an MCP tool, so without this 7.3 would have been invisible to the one
  consumer that matters. LSP-sourced references leave it empty, since the
  response-level `Source: "lsp"` already says they were resolved.

  Unit tested: yes — 5 new tests in `internal/code/graph_confidence_test.go`:
  same-file labeling, cross-file labeling, graph-level source/limitations
  content, per-reference propagation, and `TestAmbiguousCalleeIsDroppedNotGuessed`
  (two files declaring the same name produce *no* edge rather than an
  arbitrary pick — the property the confidence labels are claiming).
  `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: no — needs reconnect.** Called `jade_references`
  for `resolveCalleeID` against the running server: it returned references
  with no `Confidence` field and `Source: "approximate"` with "gopls
  unavailable". Both are exactly right for a server process that predates
  this task *and* the toolchain fix, so neither is a new bug. After reconnect
  this call should show `Source: "lsp"` (gopls is now installed and findable)
  — which will exercise the LSP branch live for the first time.

  Dogfood note: `jade_replace_range` produced an off-by-one splice that ate a
  closing brace, and jade's own diagnostics caught it *in the edit response*
  (`missing ',' in argument list` at the exact line) rather than leaving it
  for `go build`. Fixed with one more `replace_range`. This is 2.4 earning its
  keep — the failure mode jade is supposed to shorten, shortened.

---

## Phase 8 — Droneship steals still on the table

Two mechanisms from reference_droneship.md were explicitly flagged as
worth reusing but never actually ported, plus the one item scope.md itself
calls a *prerequisite* for trusting any of this work (§26, §44-45) that was
never done — including retroactively.

- [x] **8.1 Bound `job_output`'s raw output size**
  2.5 ported droneship's decisive-line extraction but not the other half of
  `clampToolOutput` (reference_droneship.md §14): today
  `jobs.Runner.CompleteWithOutput` stores the *entire* raw output
  unbounded, and `jade.job_output` returns all of it. Droneship's own
  measured rationale — "only 2.8% of results exceeded 8,000 characters, but
  those results accounted for 31% of tool output" — applies identically
  here: a large `go test ./...` failure on a bigger repo could dump
  megabytes into an agent's context through `job_output` alone. Cap stored/
  returned raw output at a fixed budget (e.g. head + decisive lines already
  computed by 2.5, already last-first-ordered) with a note of how many
  bytes were omitted — this is the "Jade Result Envelope" concept
  reference_droneship.md §14 describes, applied to the one place jade still
  lacks it.

  Picked up: 2026-09-12 — Added `internal/jobs/clamp.go`:
  `clampRawOutput` bounds stored output at `maxRawOutputBytes = 8000`
  (droneship's own measured threshold) keeping **both** ends — 40% head, 60%
  tail — with an explicit `... N bytes omitted ...` marker between them.
  Both ends are kept deliberately: a compiler reports its first error first
  while `go test` prints the FAIL summary last, so keeping only one end
  reliably loses the decisive part for one of the two tools jade runs most.
  Cuts land on line boundaries, since a raw byte cut can split a UTF-8 rune
  or a path and costs a reader more than the bytes it saves.

  `Runner.CompleteWithOutput` now **summarizes before clamping**, and
  `Runner.Output` returns a `JobOutput` struct instead of five positional
  values (a sixth would have made same-typed positions genuinely easy to
  transpose). `OmittedBytes` is plumbed through `protocol.JobOutputResponse`
  and both transports: silent truncation is worse than visible truncation,
  because a reader who cannot see bytes are missing reads the remainder as
  complete.

  Unit tested: yes — 6 new tests in `internal/jobs/clamp_test.go`:
  pass-through for small output, both-ends survival with a visible marker,
  line-boundary cutting, exact byte accounting (head + omitted + tail must
  equal the original), the summarize-before-clamp ordering, and zero omitted
  for short jobs. `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **A test failure during this task exposed a real weakness — logged as
  12.6, not fixed here.** The ordering test was originally written to assert
  that a decisive error buried in the clamped middle still reaches the
  summary. It failed, and correctly so: `DecisiveSummary` reads *trailing*
  lines, so a mid-output error never reaches the summary whether or not
  anything was clamped. The test was rewritten to pin what is actually true
  (summary is computed from the full output), and the underlying weakness
  recorded separately rather than papered over.

  **Verified live over MCP: no — needs reconnect.** Ran `jade_run_tests`
  scoped to `internal/jobs/clamp_test.go` for real: the job completed and the
  new tests passed (`ok github.com/julianbei/jade/internal/jobs 1.782s`), so
  the change is confirmed good on disk. `jade_job_output` returned no
  `OmittedBytes` field, as expected from a server process predating this
  task.

- [x] **8.2 Add `diff(target?)` with real patch content**
  3.2 ported droneship's `diffSummary()` (numstat: per-file +/- counts) but
  not its sibling `diffPatch()` (actual hunk text). scope.md §22 lists
  `diff(target?)` as a distinct API surface from `changes()` — today there
  is no way to see *what changed*, only *how much*. Add a `jade.diff` tool
  wrapping `git diff <path>` (or `git diff HEAD -- <path>` for a specific
  target) for on-demand hunk expansion, matching scope.md §13's "the agent
  can then request: `expand change SessionManager.refreshSession` to obtain
  the textual diff" — progressive disclosure applied to changes, the same
  principle already applied everywhere else in jade.

  Picked up: 2026-09-12 — Added `internal/workspace/diff.go` with
  `Manager.Diff(target)`, `protocol.DiffRequest`/`DiffResponse`,
  `internalapi.Server.Diff`, and the `jade.diff` MCP tool. Empty target
  diffs the working tree; a path diffs one file.

  **Untracked files are included, which `git diff HEAD` cannot do.** A file
  the agent just created through `create_file` would otherwise return an
  empty patch — a silent lie about the change jade itself just made. Handled
  by diffing against `os.DevNull` with `git diff --no-index`, whose non-zero
  exit on "files differ" is the normal case here and is deliberately ignored.
  A targeted diff that comes back empty falls through to the untracked path
  before concluding "unchanged", so the two situations are distinguished
  rather than collapsed.

  **Clamping was required, not optional.** This repository's own working diff
  is already over 8,000 lines, so an unbounded `diff()` would have been the
  largest response jade can emit — reintroducing the exact bug 8.1 had just
  fixed, on a bigger surface. Rather than copy the clamp, it was extracted to
  `internal/textutil.Clamp(text, maxBytes, hint)` and `internal/jobs` now
  delegates to it, so job output and diffs cannot drift apart in how they
  truncate. All of 8.1's clamp tests still pass unchanged against the
  extracted version.

  Unit tested: yes — 7 new tests in `internal/workspace/diff_test.go`, each
  against a real `git init` repo with real commits: modified-file hunk
  content (asserting the actual `-`/`+` lines, not just non-emptiness),
  untracked inclusion, single-path targeting (and that the *other* changed
  file is excluded), targeting an untracked path, clean-tree reporting,
  large-patch clamping with a visible marker, and file counting in the
  summary. `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: no — needs reconnect.** `jade.diff` is brand-new
  so it is not in this session's catalog (confirmed via `ToolSearch`). What
  *was* verified live: `jade_run_tests` scoped to the new
  `internal/workspace/diff_test.go` completed and passed
  (`ok github.com/julianbei/jade/internal/workspace 0.664s`), so the new code
  is confirmed good on disk through jade's own validation path, and the
  shared-clamp refactor did not disturb the running job pipeline.

  Dogfood note: two `jade_replace_range` splices went wrong this task — one
  landed inside a struct literal, one replaced an import line instead of
  inserting above it (leaving a duplicate `os/exec` and a missing `fmt`).
  jade's diagnostics caught the first in the edit response; `go build` caught
  the second. Both are the same root cause and reinforce 12.1: line-number
  addressing is fragile for insertions, and a string-anchored edit would not
  have this failure mode at all.

- [x] **8.3 Build the benchmark harness scope.md calls a prerequisite**
  scope.md §26 states the MVP must answer "does an agent using jade
  complete real tasks more efficiently and reliably than shell/file
  tooling" and §44-45 explicitly wants this measured *before* the bulk of
  the work, not after — reference_droneship.md §16 adds that droneship
  already has reusable benchmark philosophy and corpora built around
  "tokens to answer," not search-precision. None of that happened before
  or during Phases 1-5. This is now the most important item in the entire
  file: every fix in Phases 1-5 is unverified against the one question
  scope.md says actually matters. Build a minimal version: pin a small
  representative task set against this repo, run it once with shell/file
  tools only and once with jade's tools, and measure tokens/turns/wall-time
  per scope.md §42-43's own metric list. This doesn't need droneship's full
  A-E arm structure to start — even a single control-vs-jade comparison on
  3-5 tasks would be the first real evidence either way.

  Picked up: 2026-09-12 — Added `internal/bench` (harness, scenarios,
  reporting) and `cmd/jade-bench`. Seven scenarios, each a question an agent
  actually asks, answered twice: once through the real `internalapi.Server`
  (the same one `cmd/jade-mcp` builds, serialized to JSON exactly as MCP
  sends it) and once through the shell command a competent agent would
  actually reach for. Measures bytes and estimated tokens of what the agent
  must read.

  Scope is deliberately narrow and stated in the output itself: this measures
  **tokens only**. Turns and success rate need a real agent making real
  decisions, and scripting them would produce a number that looks like
  evidence while measuring only the script's author. Call counts are reported
  as a floor on turns, not a measurement of them.

  ### First result (2026-09-12, this repo)

  ```text
  scenario                        jade      shell     ratio   calls (j/s)
  outline one file                1531      89        17.20x  1/1
  read one symbol                 1211      133       9.11x   1/2
  find a symbol repo-wide         1517      46        32.98x  1/1
  who calls a symbol              157       267       0.59x   1/1
  orient in the repo              1137      449       2.53x   1/1
  what changed                    1617      363       4.45x   1/2
  read a line range               1147      131       8.76x   1/1
  TOTAL                           8317      1478      5.63x
  ```

  **jade currently costs 5.63x more tokens than plain shell to answer the
  same questions.** One scenario out of seven wins: `references` at 0.59x,
  and it wins precisely because it returns a short answer instead of a
  document. Everything else loses, badly — `search` costs 33x what `grep`
  costs to answer "where is this defined".

  This is the first real evidence against the question scope.md said should
  have gated all of this work, and it is negative. It does not say the
  primitives are wrong: Phases 1-5 made them genuinely work, and the one
  scenario returning a terse answer beats shell outright. It says the
  **response envelope is eating the entire value proposition**. Roughly every
  jade response here is 1,100-1,600 tokens regardless of how small the actual
  answer is, because `Freshness` (with its duplicated 35-path lists), null
  `Outline`, six null `Sections` and empty `Resolve` ride along on every call.

  **Consequences, recorded as decisions rather than opinions:**
  1. **Phase 10 is no longer a cleanup phase, it is the critical path.** An
     agent that reads this table has no reason to choose jade over grep for
     six of seven tasks. Phase 10 must land before Phase 11's release, and
     before any further Phase 9 features.
  2. **Re-run this benchmark after Phase 10 and record the delta here.** The
     compression claim is now falsifiable, which is the point of building
     this.
  3. **The friction log's item 5 is confirmed quantitatively.** "I avoided
     `read_range` because it costs ~80 lines of JSON for 8 lines of source"
     was an impression; it is now 8.76x, measured.

  Unit tested: yes — 8 tests in `internal/bench/bench_test.go`: token
  rounding, both-arm measurement, ratio semantics (including that <1.0 means
  jade is cheaper), unmeasurable ratios, failures recorded rather than
  aborting the suite, a nil arm reported as an error rather than silently
  counted as 0 tokens, and that the report carries its own caveat about what
  it does not measure.
  `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: partially.** `jade_run_tests` scoped to
  `internal/bench/bench_test.go` ran and passed through jade's own job
  pipeline. The benchmark itself is a CLI rather than an MCP tool, so there
  is nothing new in the catalog to call — but it exercises the identical
  `internalapi.Server` the MCP transport wraps, so the measured numbers are
  the numbers an agent over MCP actually pays.

---

## Phase 9 — North Star: semantic change and context awareness

The two North Star capabilities scope.md itself frames as the clearest
differentiators (§13 changed-symbol awareness, §14 "context intelligence...
an agent-native replacement for the collection of panels a human IDE
presents simultaneously") and are still completely file-level or absent.

- [x] **9.1 Make `changes()` symbol-aware, not just file-aware**
  scope.md §13's own MVP example shows `changes()` naming the *symbol*
  that changed (`SessionManager.refreshSession modified`), not just the
  file. 3.2's `DiffSummary` reports per-file numstat only. With 1.5's
  reparse-on-read already giving jade accurate symbol boundaries on demand,
  this is diffing old vs. new symbol lists for each changed file (using
  the checkpoint snapshot bytes 1.6 already stores as the "old" side) and
  reporting added/removed/modified symbol names — no new infrastructure,
  just combining pieces that already exist.

  Picked up: 2026-09-12 — Added `internal/code/symbol_delta.go`
  (`Index.SymbolDelta`), `protocol.SymbolChange` + `SymbolChangeKind`,
  `ChangesResponse.Symbols`, `workspace.Manager.FileAtHead`, the composition
  in `internalapi.Server.changedSymbols`, and symbol rendering in
  `render.changes`.

  **Comparison is by body text, not by line range.** This is what makes the
  feature usable rather than noise: inserting a function at the top of a file
  shifts every line below it, and a range comparison would report the whole
  file as modified on every edit. `TestSymbolDeltaIgnoresSymbolsThatOnlyMoved`
  pins it.

  Used `git show HEAD:<path>` for the old side rather than 1.6's checkpoint
  snapshots as the task suggested. Checkpoints only exist if one was taken;
  HEAD always exists and is the same baseline `changes()` already reports
  against (`git diff HEAD`), so the symbol delta and the line counts can
  never disagree about what they are comparing to.

  The composition lives in `internalapi` deliberately: the workspace knows
  what git changed and can produce committed bytes, the index knows how to
  parse symbols, and neither package had to learn about the other — which
  also avoids `workspace` importing `code`.

  Two bounded-degradation properties: a language jade cannot parse yields no
  symbol detail rather than wrong detail (file counts still stand), and
  `render.changes` caps symbol lines at 40 with `... N more symbol changes`.
  That cap matters — 55 files are currently changed in this repo, and
  unbounded symbol detail on the most-called tool would have undone Phase
  10's savings. The count always reports the true total, so the cap costs
  detail, never accuracy.

  Unit tested: yes — 8 tests in `internal/code/symbol_delta_test.go`
  (added/removed/modified, moved-but-unchanged, missing old side = all added,
  missing new side = all removed, identical source, unparseable language,
  kind propagation, and that in-memory diffing leaves the symbol cache
  untouched — the old side never exists on disk and caching it under the
  live file's identity would poison later reads) plus 2 render tests
  (symbols nested under their file with the path named once; the cap
  reporting the true remaining count).
  `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: no — needs reconnect.** `jade_changes` returned
  the rendered file list with no `Symbols` section, exactly right for a
  server predating this change. Worth noting what the same call *did* prove:
  it came back as 56 clean text lines rather than the ~500-line JSON blob the
  identical call produced earlier in this session, so Phase 10 is holding on
  the most verbose response in the system.

  Dogfood note: `gofmt -l` flagged four files whose formatting had drifted
  through accumulated `replace_range` edits. jade has no formatting step and
  nothing in its validation catches it — `go vet` and `go test` both pass on
  badly formatted code. Logged as 12.7.

- [x] **9.2 Add `context(target, purpose)` assembly**
  scope.md §14's standout North Star primitive: given a symbol and a stated
  purpose (e.g. `"modify"`), assemble implementation + relevant types +
  direct callers + relevant tests + current diagnostics + recent-change
  summary in one call. Every piece already exists as a separate jade
  primitive after Phases 1-9 (`read_symbol`, 7.1's `references`, a
  test-name-matching heuristic, `diagnostics()`, 9.1's symbol-level
  `changes()`) — this task is purely the assembly/orchestration layer, the
  same "progressive disclosure over multiple concepts at once" jade already
  does for `outline()`. Highest-value single item in this phase since it's
  the one thing no combination of existing individual tool calls replaces
  without several extra round trips.

  Picked up: 2026-09-12 — Added `internal/transport/internalapi/context.go`
  (`Server.Context`), `protocol.ContextRequest`/`ContextResponse`, a
  renderer, and the `jade.context` MCP tool. Assembly only: every section
  reuses an existing primitive (`ReadSymbol`, 7.1's `References`,
  `diagnostics.Immediate`, 9.1's `SymbolDelta`, `BuildSymbolGraph`).

  **Callers and tests are split by file, not by new analysis.** A reference
  from a `_test.go` file is a test exercising the symbol; everything else is
  a production caller. An agent about to change a symbol wants those two
  lists separately — the callers say what might break, the tests say what
  will tell it so. Recognizes Go (`_test.go`) and TypeScript
  (`.test.ts`/`.spec.ts`) conventions.

  `purpose` narrows and never adds: "modify" (default) is the widest set,
  "understand" drops diagnostics, "debug" drops the type tour, "test" centres
  existing tests. An unrecognized purpose falls back to "modify" — omitting a
  section the caller needed is worse than sending one they did not.

  Related types are a name match against the repository's own declared types
  appearing in the body, and the doc comment says so explicitly rather than
  implying resolved type analysis. Every list section is capped (10) with the
  true total reported, per the 9.1 lesson that an unbounded assembled
  response would undo Phase 10.

  Unit tested: yes — 6 tests in `context_test.go` covering purpose fallback,
  synonym mapping, that every non-default purpose is a strict subset of
  "modify", test-file recognition for both language conventions (including
  that `testdata/fixture.go` is *not* a test file), cap-reports-true-total,
  and deterministic ordering. `gofmt`, `go build ./...`, `go vet ./...`,
  `go test ./...` all pass.

  **The 10.6 coverage guard earned its keep twice in one task.** It failed
  the build for `ContextResponse` having no renderer, then failed again for
  that renderer returning empty on a zero value — the identical bug class it
  caught three times during 10.6. A new response type genuinely cannot ship
  as raw JSON or as an empty string by being forgotten.

  **Verified live over MCP: YES (2026-09-12 reconnect).**
  `jade_context` for `cleanNudgeQuery` returned
  `cleanNudgeQuery (function) for modify · 1 callers · 3 tests · added since
  HEAD`, then the caller, the three tests, and the implementation — one call
  replacing four.

  **It also proved 7.1's LSP path live for the first time.** The callers and
  tests came back with real line numbers (`internal/code/nudge.go:48`,
  `nudge_test.go:57/63/69`) rather than the approximate graph's `Line: 0`,
  which means gopls resolved them — confirming the `internal/toolchain.Gopls()`
  GOPATH-fallback fix works end-to-end over MCP, not just in unit tests.
  It also retires 12.4's premise: distinct callers no longer render as
  indistinguishable duplicates, because they now carry distinct lines.

  ### Live bug found and fixed this task: `changes()` dropped the revision

  The reconnect landed mid-task, which confirmed 9.1 live: `jade_changes`
  returned per-file symbol deltas with the 40-line cap and
  `... 368 more symbol changes` working exactly as designed.

  It also surfaced a real regression I introduced in Phase 10. A
  `jade_replace_range` was rejected with `edit rejected: stale revision`, and
  **there was nowhere to look up the current revision**: the rendered
  `changes()` had dropped the `Revision` field as "structure". The revision is
  a required argument of every edit tool and `changes()` is where a caller
  goes to find it. Fixed — the revision now leads the `changes()` output and
  is printed even for a clean tree.

  This is the sharpest dogfood result so far, because it is a failure only
  live use could produce: every unit test passed, the benchmark improved, and
  the response was objectively smaller. Compression removed something load-
  bearing, and only actually trying to edit through the tool revealed it.

- [x] **9.3 Add `history(symbol)`/`blame(symbol)`**
  scope.md §18: "rather than `git log -p`, jade could provide
  `history(SessionManager.refreshSession)` and return only commits that
  affected that symbol." Implementable today with `git log -L
  <start>,<end>:<file>` against the symbol's current line range (from
  `Outline`/`ReadSymbol`) — a real git primitive, not a new invention —
  giving an agent "why does this code exist" without grepping full file
  history.

  Picked up: 2026-09-12 — Added `internal/workspace/history.go`
  (`Manager.SymbolHistory`), `internal/transport/internalapi/history.go`,
  `protocol.CommitInfo`/`HistoryRequest`/`HistoryResponse`, a renderer, and
  the `jade.history` MCP tool.

  The line range comes from jade's *current* parse and git's `-L` follows
  that range backwards itself. That ordering is the whole trick: jade never
  has to guess where the symbol lived in an older revision, which is the part
  a naive implementation gets wrong.

  Commits only by default, patch behind `includePatch` — progressive
  disclosure, since the commit list answers "why does this exist" and the
  patch is the follow-up question. `-s` suppresses the diff `-L` otherwise
  always emits. Patch is clamped at 6 KB via `textutil.Clamp`; SHAs are
  abbreviated to 7 characters, which costs 33 characters per commit less and
  answers no question the full SHA does not.

  **A real bug, caught by the tests rather than by review:** the first
  implementation used NUL as the field separator, which is conventional for
  git plumbing but *illegal in an exec argument* — every call failed with
  `fork/exec /opt/homebrew/bin/git: invalid argument`. Fixed by putting the
  literal text `%x1f` in the format string so **git** expands it to the
  separator byte, so no control character ever appears in argv. The constant
  now documents why NUL is not an option, so the next person does not
  re-derive it.

  Unit tested: yes — 8 tests in `internal/workspace/history_test.go` against
  real `git init` repos with real commits. The load-bearing one is
  `TestSymbolHistoryReturnsOnlyCommitsTouchingTheRange`: two functions in one
  file changed in separate commits, and asking about one must not surface the
  other's commit — exactly what `git log -p <file>` would do, and the entire
  reason scope.md §18 asks for this. Plus patch on/off, limit,
  invalid-range rejection, untracked-file erroring (rather than reporting
  zero commits, which reads as "never touched"), and header-vs-diff-line
  parsing.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: YES (2026-09-12 reconnect).**
  `jade_history` for `Start` in `internal/jobs/runner.go` returned
  `3 commits touched internal/jobs/runner.go:39-60 (most recent 2026-09-11)`
  followed by three one-line commits. Four lines total, and the range was
  resolved from the current parse exactly as designed.

  Correction to 9.2's note: it said a stale-revision rejection left
  "nowhere" to look up the current revision. That was overstated — every
  `inspect` response (`read_symbol`, `read_range`) carries it, and this task
  used exactly that workaround to get `r1` and keep editing through jade. The
  `changes()` fix still stands, since `changes()` is where a caller would
  look first, but the situation was recoverable rather than a dead end.

  **Phase 9 is complete** — all three North Star capabilities are in.

---

## Explicitly not stealing from droneship right now

- **Git worktree isolation** (`git-service.ts` worktree pool, sparse
  checkout, copy-on-write clones) — solid engineering, but out of scope
  until jade needs multi-agent workspace isolation, which isn't on the path
  to "borderline useful" yet.
- **Droneship's call-graph/blast-radius system** — shallow and name-based by
  its own test suite's admission (documented limitations: duplicate names,
  dynamic dispatch, registries, cross-language calls). Jade's own
  `BuildSymbolGraph` already exists and was fixed this session (cross-file
  call-edge resolution bug); the long-term direction should be LSP/compiler-
  resolved references (gopls, tsserver, rust-analyzer) per Phase 4.3, not
  droneship's regex graph.

---

## Verification pass — 2026-09-12 (after MCP reconnect)

Six tools that had been marked "Verified live over MCP: no — needs
reconnect" were exercised for real against this repo in one pass. **All six
work.** This supersedes the per-task "needs reconnect" notes for 5.2, 6.1,
6.2, 6.3 and 6.4; those notes are left as written because they record what
was true when the task was done.

- **6.1 `jade_workspace_tree`** — real tree, honours `maxEntries`, reports
  `Truncated: true`.
- **6.2 `jade_read_range`** — correct verbatim lines 37-44 of `references.go`.
- **6.3 `jade_create_file` / `jade_delete_file`** — round-trip; file confirmed
  on and off disk with `ls`.
- **6.4 `jade_run_tests` (scope=file)** — scoped a *test file* to its package,
  ran real `go test`, reached `"completed"` with
  `ok github.com/julianbei/jade/internal/code 0.248s`.
- **5.2 `jade_search_nudge`** — fired on a real grep command, returned 3
  correct index hits.
- **3.2 `jade_changes`** — full numstat payload: `34 files, +7216 -112` with
  per-file added/removed.

**The headline usability finding: response envelopes are enormous relative
to their payload.** `jade_read_range` returned 8 lines of source wrapped in
roughly 80 lines of JSON — `ChangedPaths` and `DirtyPaths` were byte-for-byte
identical 35-element lists, and `Outline`, all six `Sections` and every
`Resolve` field were null/empty but still serialized. `jade_workspace_tree`
spends 5 lines of JSON per entry to convey a path and a boolean. Meanwhile
`jade_search_nudge` — the single tersest response in the system — is the one
that most resembles what droneship's authors found useful. This is precisely
the "less verbose" property the droneship comparison was about, and it is now
measured rather than asserted. Phase 10 fixes it.

---

## Phase 10 — Plain-text output: stop paying for JSON

**External feedback received 2026-09-12: "the output of jade does not need to
be JSON, it can be plain text — compress the output into as little tokens as
possible."** This is the first outside feedback jade has received and it
matches the verification pass's own finding from the other direction. The
consumer of every jade response is a language model, not a parser: MCP
already delivers tool results as text content, so the JSON is a serialization
tax paid on every single call — braces, quotes, field names, indentation, and
nulls for fields nobody populated. `search_nudge` skipped that tax and is the
best-reading tool in the system.

The target is a per-tool plain-text rendering: decisive line first, structure
only as far as a reader needs to scan it. Rough shape for the worst offender,
`read_range` — the same call that cost roughly 80 lines of JSON:

```text
internal/code/references.go:37-44 (r3, drifted: 35 files)
func (i *Index) References(path string, symbolID string) (protocol.ReferencesResponse, error) {
	symbol, line, column, err := i.locateSymbolPosition(path, symbolID)
	...
```

- [x] **10.1 Add a text renderer layer and make it the MCP default**
  One `Render()` per response type, called at the MCP transport boundary, so
  the internal structs stay typed and testable and only the wire format
  changes. Keep JSON reachable behind an explicit flag for any consumer that
  genuinely parses. Land this first — 10.2-10.5 are then rendering decisions
  rather than struct surgery.

  Picked up: 2026-09-12 — **Taken out of phase order deliberately.** 8.3's
  benchmark result recorded a decision in this file: Phase 10 precedes any
  further Phase 9 feature. 9.1 was next by number; this was next by priority,
  and the file's own recorded reasoning wins over its numbering.

  Added `internal/render` — one `Text(value) (string, bool)` type switch
  covering 13 response types, called from `jsonResult` in `cmd/jade-mcp`.
  `jsonResult` is the single chokepoint every tool already routes through, so
  one change converted the entire tool surface. Unrendered types fall through
  to JSON rather than to a guessed default, so an unhandled response degrades
  instead of silently dropping fields. `JADE_JSON=1` restores the old format
  for any consumer that genuinely parses.

  The benchmark's jade arm was updated in the same change: it now measures
  rendered text with the same JSON fallback, because a benchmark that kept
  measuring JSON after the transport stopped sending it would be measuring
  nothing.

  ### Benchmark delta — the compression claim, verified

  ```text
  scenario                  before    after     shell    ratio before → after
  outline one file          1531      44        89       17.20x → 0.49x
  read one symbol           1211      145       133       9.11x → 1.09x
  find a symbol repo-wide   1517      85        46       32.98x → 1.85x
  who calls a symbol        157       43        267       0.59x → 0.16x
  orient in the repo        1137      354       449       2.53x → 0.79x
  what changed              1617      462       377       4.45x → 1.23x
  read a line range         1147      137       131       8.76x → 1.05x
  TOTAL                     8317      1270      1492      5.63x → 0.85x
  ```

  **jade went from costing 5.63x shell to 0.85x — cheaper than grep and sed
  overall, on the same seven questions, with no capability removed.** Total
  output fell 8,317 → 1,270 tokens, a 6.5x reduction. The worst case improved
  most: "where is this defined" went from 33x to 1.85x, and "what does this
  file declare" from 17.2x to 0.49x.

  Where the savings came from, in order: source emitted as source instead of
  a JSON string with every newline escaped; `Freshness` reduced to
  `drifted: N files` instead of two byte-identical 35-path arrays; null
  `Outline`, six null `Sections` and empty `Resolve` omitted entirely. Note
  that 10.3, 10.4 and 10.5 are largely satisfied as a consequence — the
  renderer had to decide what to print, and "nothing, when empty" was the
  right answer at every site.

  Three scenarios remain above 1.0x. That is expected and not obviously worth
  chasing: jade returns structured, revision-stamped, freshness-aware answers,
  and paying ~10% over `sed` for a line range that also tells you the file
  drifted is a reasonable trade. Parity was the goal; beating grep on totals
  was not required.

  Unit tested: yes — 13 tests in `internal/render/render_test.go`, written as
  assertions about the properties that motivated the package rather than
  golden strings: unknown types fall back rather than render, source contains
  real newlines and not `\n`, empty structure names never appear, ambiguity
  leads the output and suppresses an outline the caller cannot use, freshness
  path lists are dropped while the count survives, `changes()` prints each
  path exactly once, tree entries are bare paths, diagnostics get their own
  line in an edit response, truncation stays visible, and a realistic inspect
  response renders under 120 bytes.
  `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: no — needs reconnect, and the evidence is the
  reconnect itself.** `jade_run_tests` scoped to the new
  `internal/render/render_test.go` ran and passed through jade's own job
  pipeline — but that response came back as JSON, because the running server
  predates this change. Every jade response in this session is still the old
  format. After reconnect the entire tool surface should switch to text, and
  this is the one change in the whole backlog whose effect will be visible in
  literally every subsequent tool call.

- [x] **10.2 Render source-bearing responses as source, not as a string field**
  `read_symbol` / `read_range` should emit a one-line header followed by the
  raw code. Today the source arrives as a JSON string with every newline and
  tab escaped, which is both larger and harder to read than the file it came
  from.

- [x] **10.3 Drop empty and duplicated fields entirely**
  Null `Outline`, six null `Sections`, empty `Resolve` and `Diagnostics: null`
  should not appear at all. `ChangedPaths` and `DirtyPaths` were byte-for-byte
  identical 35-element lists in the same response — emit one, and only the
  difference when they diverge.

- [x] **10.4 Reduce `Freshness` on reads to one line**
  Useful once per session, not attached to every read. `drifted: 35 files`
  carries the decisive bit; the full path list belongs in `changes()`, which
  already reports exactly that and is where an agent would ask.

- [x] **10.5 Render `workspace_tree` as bare paths**
  One path per line, trailing `/` for directories. The per-entry JSON object
  costs roughly 10x a bare string for the same information.

- [x] **10.6 Measure the reduction and write down the house style**
  Record before/after token counts for the same six calls made in the
  2026-09-12 verification pass — the claim is a compression claim and should
  be verified as one. Then write the rule down (decisive text first,
  structure only where a caller must branch on it) as the acceptance bar for
  every future tool.

  Picked up: 2026-09-12 — **10.2, 10.3, 10.4 and 10.5 were all delivered by
  10.1's renderer** and are checked off against existing test evidence rather
  than reimplemented: `TestInspectEmitsSourceAsSourceNotAsQuotedString`
  (10.2), `TestInspectOmitsEmptyStructure` +
  `TestChangesDoesNotPrintPathsAndFilesTwice` (10.3),
  `TestFreshnessReducesToOneLineAndDropsPathLists` (10.4),
  `TestWorkspaceTreeRendersBarePaths` (10.5). All five verified passing before
  being marked done. The renderer had to decide what to print at every site,
  and "nothing, when empty" was the right answer each time — so these were
  consequences of 10.1, not separate work.

  10.6's own work, in two halves:

  **The written rule** — `docs/response-style.md`: nine rules with the
  measured justification, the enforcement mechanism, and the `JADE_JSON=1`
  escape hatch. It leads with the benchmark table because the rules are
  conclusions from a measurement, not preferences.

  **The enforcement** — `internal/render/coverage_test.go` parses
  `internal/protocol/types.go` with `go/ast`, finds every exported
  `*Response` struct, and fails if one has no renderer. Parsing the source
  rather than keeping a second hand-written list is the point: the guard has
  to notice types nobody remembered to tell it about. It also checks its own
  list back against the source, so a stale entry cannot fake coverage. A
  second guard fails any renderer that returns empty for a zero value.

  **The guard immediately found three real bugs.** `jobStatus`, `jobOutput`
  and `diff` all rendered to an empty string for a zero-valued response,
  which would have left an agent unable to tell a result from a dropped call.
  Fixed with explicit `(no job)` / `no changes` fallbacks, and rule 3 in the
  style doc exists because of it. Two response types (`EventsResponse`,
  `CheckpointResponse`) had no renderer at all and were still falling back to
  JSON; both now have one, so all 15 response types are covered with no
  exemption list.

  Unit tested: yes — 2 new guard tests plus the 3 renderer fixes; the full
  `internal/render` suite is 15 tests. `go build ./...`, `go vet ./...`,
  `go test ./...` all pass. Benchmark re-run after the changes still reports
  **0.85x** (1,299 vs 1,523 tokens), so the fixes cost nothing.

  **Verified live over MCP: YES — the reconnect happened mid-task and the
  renderer is confirmed live.** Every jade response in this session is now
  plain text. `jade_read_range` returned `r1 · drifted: 48 files` followed by
  raw source, replacing what used to be ~80 lines of JSON for 8 lines of
  content. `jade_replace_range` now answers
  `r3 → r4 · internal/render/render.go · +17 -12` plus a jobs line. Both
  renderer bug fixes above were themselves made *through* jade against the
  live renderer. `jade_diff` was also exercised live for the first time and
  correctly produced a full patch for an **untracked** file — the
  `--no-index` path 8.2 built specifically for that case, confirmed working
  end-to-end rather than only in unit tests.

---

## gopls installed — the LSP paths are finally proven (2026-09-12)

Every gopls-dependent task so far (4.3, 7.1, 7.2) shipped with the same
caveat: "gopls is not installed in this environment, so the LSP branch is
unit-tested only." That caveat is now retired. `go install
golang.org/x/tools/gopls@latest` installed v0.23.0 — the install always would
have worked; nobody had run it.

**It exposed a real jade defect on the way.** `go install` writes to
`$GOPATH/bin`, which is not on PATH on a stock setup — including this
machine. jade used `exec.LookPath("gopls")` in three places, so it reported
gopls as unavailable on a machine where gopls was installed exactly as the Go
documentation instructs. Fixed with `internal/toolchain.Gopls()`, which falls
back to `GOBIN`, each `GOPATH` entry's `bin`, then `$HOME/go/bin`, caching the
result. All three call sites (`diagnostics.goplsCheck`,
`code.goplsReferences`, `code.runGoplsRename`) now use it. This would have hit
every early adopter in Phase 11, on their first run, and looked like "jade's
LSP support doesn't work."

Two tests that could not previously exist now pass:

- `TestReferencesUsesGoplsWhenAvailable` — `Source: "lsp"` with real line and
  column numbers for a cross-file call site. The approximate path reports
  `Line: 0` and cannot do this at all.
- `TestRenameActuallyRewritesEveryCallSite` — a rename applied through gopls
  rewrote `caller.go`, a file the caller never named, and `Target` is gone
  from both files afterward. This is 7.2's entire premise, previously
  unproven.

One existing test had to change: `TestImmediateReturnsNilForCleanGoFile`
asserted `nil`, but with gopls actually running a clean file yields an empty
non-nil slice. Renamed to `...ReturnsNoDiagnostics...` and now asserts
`len() == 0`, so it holds whichever path answered. That failure is itself the
evidence the gopls branch went live — it had never executed before.

Live over MCP: `jade_references` and `jade_rename` were both called for real
this session (see the friction log's 12.4 for a defect that surfaced), but
against the *pre-fix* server, so both reported `approximate`/unavailable. The
toolchain fix needs a reconnect before the live calls show `Source: "lsp"`.

---

## Dogfood friction log — why the agent reached for native tools

Recorded 2026-09-12, prompted by the direct question: "why are you not using
the jade MCP for these edits and test runs?" During 7.2, production code went
in through jade (`create_file` + 5x `replace_range`, all landing cleanly) but
tests, fixes, navigation and validation did not. The honest reasons, because
*why an agent routes around the tool* is the most useful signal this project
can collect:

1. **Test files written with the native tool — no reason at all.**
   `jade_create_file` would have worked identically. Pure inconsistency.
   Not a jade defect; recorded so the other four aren't excused by it.
2. **Follow-up fixes need string anchoring, not line numbers.** The native
   edit matches an exact string; `jade_replace_range` needs line numbers, and
   every edit shifts them, so each successive fix needs a re-read first. See
   12.1.
3. **`go build` / `go vet` have no jade equivalent.** Validation jobs spawn
   automatically after an edit, but there is no way to ask for a build or vet
   on demand. This is the single most-reached-for native command. See 12.2.
4. **`jade_run_tests` is async when the caller needs sync.** Call, get a job
   ID, poll `job_status` — two calls and a wait, versus one native call that
   returns output directly. See 12.3.
5. **Navigation avoided jade *on cost grounds*.** `jade_read_range` spends
   ~80 lines of JSON to deliver 8 lines of source, so grep and native reads
   won. The intended user of this tool routed around it to save tokens. This
   is Phase 10's justification demonstrated rather than argued, and it is the
   most important line in this log.

---

## Phase 12 — Close the gaps the friction log found

> **This phase gates Phase 11.** Nothing here is polish: every item is a
> capability whose absence sends the agent to bash, and a bash fallback is
> jade not being in the loop at all — no telemetry, no guardrails, no
> revision tracking. Releasing 0.0.1 with these holes would ship a tool that
> external users route around for the same reasons recorded in
> `feedback.md`, and 11.5 freezes the tool surface, so anything added after
> the freeze is a breaking change. Close the gaps, then release.
>
> **Process rule: write feedback at the end of every task.** Append to
> `feedback.md` which bash commands the task used, whether a jade tool could
> have done it, and if not what was missing. "Habit" is a valid answer and
> must be recorded as one — it means the jade path was not the obvious
> choice at the moment of choosing, which is a design problem rather than a
> discipline problem.

- [x] **12.1 Add a string-anchored edit (`replace_text`)**
  Match on an exact unique string and fail when it is ambiguous or absent,
  rather than requiring line numbers that every prior edit invalidates. This
  is the single biggest reason follow-up edits leave jade.

  Picked up: 2026-09-12 — Added `internal/code/replace_text.go`
  (`Index.ReplaceTextSource`, `ErrTextNotFound`, `ErrTextAmbiguous`),
  `edit.Service.ReplaceText`, `protocol.ReplaceTextRequest`,
  `internalapi.Server.ReplaceText`, and the `jade.replace_text` MCP tool.
  Carries the same revision precondition as `replace_range`: anchoring by
  text removes the line-drift problem, not the concurrent-edit problem.

  **Uniqueness is required, not preferred.** A missing anchor and an
  ambiguous one are both refusals, and the ambiguity error reports the match
  count so the caller knows how much surrounding context to add. "Replace the
  first match" is what makes `sed` dangerous in a script, and an agent cannot
  see which match it got.

  Ordering note: Phase 12 is taken before Phase 11 because 11.4 writes the
  install README and 11.5 freezes the tool contract. Adding tools after
  freezing the surface they describe would be backwards, and the file already
  places Phase 12 first.

  Unit tested: yes — 7 tests in `internal/code/replace_text_test.go`: the
  write actually reaching disk, ambiguity refusal *with the file left
  untouched*, that the documented remedy (a longer anchor) genuinely resolves
  ambiguity and edits the right one of two identical lines, missing-anchor
  refusal, empty-anchor rejection, multi-line anchors, and
  `TestReplaceTextSurvivesEarlierEditsShiftingLines` — two sequential edits
  where the first shifts every line below it and the second still resolves,
  which is the entire property line numbers cannot offer.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: YES (2026-09-12 reconnect).** Both paths
  confirmed against this repo. The refusal:
  `jade_replace_text` with anchor `r.mu.Lock()` in `internal/jobs/runner.go`
  returned `anchor text is ambiguous: 5 matches — extend the anchor with
  surrounding context`, changing nothing. The success: replaced a doc comment
  in `internal/protocol/types.go` that 12.3 had made stale, and the response
  was `r1 → r2 · internal/protocol/types.go · +3 -2`. Verified on disk with
  grep and `go build`. No line numbers were involved at any point — which is
  the whole claim.

  This is the one tool whose value is directly measurable against my own
  dogfooding record: the off-by-N splices logged in 7.3, 8.2 and 10.1 — a
  case landing inside a struct literal, an import line replaced instead of
  inserted above, a `default:` clause eaten — are all failure modes it
  structurally does not have.

- [x] **12.2 Add `check()` — on-demand build/vet/typecheck**
  Same discovery path as 4.x's `RunValidationCommand` but caller-triggered
  instead of only edit-triggered. `go build ./...` and `go vet ./...` are the
  most frequent native commands in every session so far, and jade cannot run
  either on request.

  Picked up: 2026-09-12 — Added `internal/transport/internalapi/check.go`
  (`Server.Check`), `protocol.CheckRequest`/`CheckResponse`, `jobs.Runner.Wait`,
  a renderer, and the `jade.check` MCP tool. Kinds are `build` (default),
  `typecheck` and `tests`, all reusing 4.x's existing discovery, so a repo
  with a Makefile target or npm script gets its own command rather than a Go
  default.

  **It waits by default, and that is the actual feature.** All three job
  kinds already existed; what was missing is that validation only ever ran as
  a *side effect of an edit* and could not be asked for. Returning a job ID
  for a two-second vet and making the caller poll is the async tax that kept
  the native shell command winning in the friction log — so `check` blocks
  and returns pass/fail directly. `wait: false` still gives a job ID for long
  runs.

  Added `jobs.Runner.Wait(id, timeout)` as the shared primitive: one channel
  per job, closed on completion, so waiting is a block rather than a poll
  loop and every waiter is released at once. **12.3 is now a thin wiring job
  on top of this** rather than a second implementation. The `Wait` contract
  keeps "still running" and "finished with no output" distinct — collapsing
  them would let a timeout read as a pass.

  Verdict leads the rendered output: an agent that reads one line should
  learn whether the check passed, not which job ran it. An unknown kind is
  rejected rather than substituted — running a different check than asked and
  reporting success is worse than an error.

  Unit tested: yes — 6 tests in `internal/jobs/wait_test.go` (completion
  observed, timeout without claiming completion, already-completed returns
  immediately, unknown job, double-`Complete` not panicking on an
  already-closed channel, and three concurrent waiters all released) plus 4
  in `check_test.go` (kind synonyms, unknown-kind rejection, timeout bounds
  including the cap that stops a caller pinning the connection, failure-marker
  detection).
  **Also ran `go test -race ./internal/jobs/` — clean**, since `Wait`
  introduces the first real concurrency beyond the existing mutex.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: YES (2026-09-12 reconnect).**
  `jade_check(kind: "typecheck")` returned `pass typecheck / no output` in a
  single call — the verdict, waited for, with no job ID and no poll. This is
  the friction-log item that had no jade equivalent at all; it now costs one
  call where the native command cost one call, which is the parity the log
  asked for.

- [x] **12.3 Let `run_tests` wait for its result**
  Add a bounded `wait` option returning the finished summary in one call.
  The async job model is right for long runs, but making every short run cost
  two calls plus a poll is why the native test command keeps winning.

  Picked up: 2026-09-12 — Added `Wait`/`TimeoutSeconds` to
  `protocol.RunTestsRequest`, `Status`/`Passed`/`Summary` to
  `RunTestsResponse`, the wait branch in `internalapi.Server.RunTests`, a
  renderer, and the two new arguments on the `jade.run_tests` MCP tool.
  Waits by default, like 12.2.

  **Exactly the thin wiring 12.2 predicted.** It reuses `jobs.Runner.Wait`
  and `checkTimeout`/`checkPassed` rather than reimplementing any of them, so
  `check()` and `run_tests()` cannot drift in how long they block or how they
  read a verdict — `TestCheckTimeoutIsSharedWithRunTests` pins that they
  share one bound. Building the wait primitive in the previous task instead
  of inline made this task about ten lines of real logic.

  The verdict comes from the command's own output via `checkPassed`, not from
  the job having completed: a *failing* run also completes, and treating
  "completed" as "passed" would be the worst possible bug in a test tool.

  Unit tested: yes — 4 tests in `run_tests_test.go`, three of them running
  real `go test` subprocesses: a clean package passing in one call, a
  nonexistent package path reported as failing (proving the verdict reads
  output rather than exit bookkeeping), a timeout that does not read as a
  pass, and the shared-timeout assertion. The passing case targets
  `internal/protocol` — a leaf package with no test files — so it completes
  fast and cannot recurse into this suite, the same trap 6.4 hit.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: no — needs reconnect, and the stale server
  demonstrated the before-state precisely.** `jade_run_tests` still exposes
  no `wait` argument and answered `started job-2`, forcing the exact
  two-call-plus-poll sequence this task removes: a second `jade_job_status`
  call returned `job-2 tests completed` / `ok
  github.com/julianbei/jade/internal/transport/internalapi 0.296s`. The new
  tests did pass through jade's own job pipeline, so the change is confirmed
  good on disk. Note the response was already rendered text rather than JSON,
  which shows Phase 10 is live while this task is not — the two are
  independent.

- [x] **12.4 Report the calling symbol in `references()` results**
  Found live on 2026-09-12: `jade_references` for `cleanNudgeQuery` returned
  three entries all reading `internal/code/nudge_test.go` with `Line: 0`.
  Dedupe is per *calling symbol*, which is correct, but only the path is
  emitted — so distinct callers render as indistinguishable duplicates. Emit
  the caller's symbol name (available in the graph node) so the entries carry
  the information that justified listing them separately.

  Picked up: 2026-09-12 — Added `Symbol` to `protocol.ReferenceLocation`,
  populated it from the graph node in `approximateReferences`, and rendered
  it. Empty for language-server results, where `Line` and `Column` already
  distinguish entries.

  **Correction to what I said after the previous reconnect.** I claimed this
  task was "retired" because gopls now returns real line numbers. That was
  true only for Go. gopls never answers for TypeScript or Rust, so those
  languages *always* take the approximate path and always had the bug —
  distinct callers in one file rendering as identical `path` lines. The claim
  was too strong and the task was real.

  Unit tested: yes — 1 test in `internal/code/graph_confidence_test.go` and 2
  in `render_test.go`. The first version of the code test used a Go fixture
  and therefore **skipped wherever gopls is installed — including here**,
  which would have shipped an untested fix behind a green suite. Rewritten
  against a TypeScript fixture with two callers in one file, so it exercises
  the approximate path as a real user hits it and runs unconditionally. The
  render tests assert the two lines differ (the actual bug) and that the
  symbol is omitted when a line number is present.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: YES (2026-09-12 reconnect).** Created a
  TypeScript fixture with two callers of one function in the same file —
  the exact shape that used to render as two identical lines. It returned:
  `docs/fixtures/ref_target_live.ts callerOne (same-file)` /
  `docs/fixtures/ref_target_live.ts callerTwo (same-file)`, distinguishable
  by name with 7.3's confidence labels attached. Fixture removed afterwards.

  Earlier, against the pre-fix server:
  `jade_references` for `splitShellSegments` returned
  `internal/code/nudge.go:106:26` / `1 references (gopls)` — the LSP path,
  confirmed unaffected by this change. `jade_references` for a TypeScript
  symbol in `docs/fixtures/outline_sections.ts` returned
  `no references ... (approximate)`, confirming TS does take the approximate
  path live, though that fixture has no callers so the naming difference
  itself needs the reconnect.

  Dogfood note: all three source edits this task were made with
  `jade_replace_text` (12.1), and **all three landed correctly on the first
  attempt** — across three different files, with no line numbers, no re-read
  between edits, and no splice repair. Every previous multi-file task in this
  backlog needed at least one fix-up edit.

- [x] **12.6 Make `DecisiveSummary` find the error, not just the last lines**
  Found 2026-09-12 while testing 8.1. `DecisiveSummary` scans backward and
  keeps the last few non-warning lines. That works for `go test` (FAIL is
  last) and fails for anything that reports an error and then keeps going:
  a 4,000-line build log whose single `undefined: X` sits in the middle
  summarizes to pure filler. Rank lines by whether they look like a
  diagnostic (`path:line:col:`, `FAIL`, `error:`, `panic:`) before falling
  back to recency. This is the summary quality claim jade rests on — 2.5's
  decisive-line extraction is the feature most likely to beat reading raw
  output, and right now it is positional rather than semantic.

  Picked up: 2026-09-12 — **The diagnosis in this task's own description was
  wrong, and the real cause was narrower.** `decisiveLines` did not "keep the
  last few non-warning lines": it already filtered by a marker regex. The bug
  was that the marker recognized `FAIL`, `panic:`, `ERROR`, `TS1234` and
  `[E0308]` but **not the `file:line:col:` form** every mainstream compiler
  and linter emits. So `main.go:42:3: undefined: X` matched nothing,
  `decisiveLines` returned empty, and the summary fell through to a fallback
  that took the **first** three lines — which on a long log is setup noise.
  Two defects, one visible symptom.

  Fixed both: added `compilerDiagnostic` (`^\S+\.\w+:\d+(:\d+)?:\s+\S`)
  and an `isDecisive` helper that checks warnings first, then either marker;
  and changed the fallback to `lastNLines`, since a tool's conclusion is at
  the end of its output and its setup is at the start. `firstNLines` became
  dead and was removed — Go does not flag unused functions, so it would have
  sat there indefinitely.

  Unit tested: yes — 5 new tests, and the 4 pre-existing `decisiveLines`
  tests still pass unchanged. The new ones: the exact buried-compiler-error
  case from 8.1 (asserting both that the error is found *and* that filler is
  excluded), four real diagnostic forms across Go/TS/Rust,
  **a negative test that ordinary prose with colons and digits is not
  mistaken for a diagnostic** (`12:34:56 starting build`, `note: run with
  RUST_BACKTRACE=1`) — without which a loose regex would match half of every
  log, fallback-to-tail, and that empty output still reports `no output`.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: YES (2026-09-12 reconnect), with a controlled
  failure.** Created a fixture containing `return undefinedOnPurpose()`, ran
  `jade_check(kind: "build")`, and got:
  `FAIL build / docs/fixtures/decisive_live.go:4:9: undefined:
  undefinedOnPurpose | make: *** [build] Error 1`.
  That first line is precisely the `file:line:col:` form that matched **no
  marker** before this fix — pre-12.6 the summary would have fallen back to
  make's echo of the command instead. Fixture removed and `jade_check`
  re-run to confirm the repo was left green.

  Incidental: `jade_create_file` reported the same error as an inline
  diagnostic on the write itself (`error docs/fixtures/decisive_live.go:4:9
  undefined: undefinedOnPurpose`), so gopls-backed diagnostics on edit (4.3)
  are confirmed live too.

- [x] **12.9 Add `delete_symbol(path, symbol)`**
  Raised by the user 2026-09-12, looking at a `python3` heredoc in the 12.6
  transcript that located `firstNLines`, found its closing brace by scanning
  for `\n}\n`, and spliced it out. That whole script exists because jade has
  `create_file`, `delete_file`, `replace_symbol`, `replace_range` and
  `replace_text` but **no way to delete a symbol**. The brace matching I
  hand-rolled is already solved inside jade — tree-sitter knows the exact
  range. One primitive removes the worst bash fallback in `feedback.md`.

  Picked up: 2026-09-12 — Added `internal/code/delete_symbol.go`
  (`Index.DeleteSymbolSource`, `expandToSurroundingBlank`),
  `edit.Service.DeleteSymbol` with the usual revision precondition,
  `protocol.DeleteSymbolRequest`, `internalapi.Server.DeleteSymbol`, and the
  `jade.delete_symbol` MCP tool.

  **It also removes the blank line the declaration leaves behind** — and only
  one side of it. Deleting lines 7-9 where 6 and 10 are both blank would
  otherwise leave a doubled blank that `gofmt` rewrites, turning a delete
  into a spurious diff somewhere the caller never touched. Consuming the
  blank on *both* sides would glue the neighbouring declarations together,
  which is worse, so the helper takes the trailing blank, or the leading one
  when the symbol ends the file, never both.

  Unit tested: yes — 6 tests: whole-declaration removal with both neighbours
  verified intact (an overshooting brace scan is exactly the failure this
  replaces), no doubled blank line, deleting the last declaration still
  leaving a well-formed file, unknown-symbol rejection *changing nothing*,
  and the two `expandToSurroundingBlank` edge cases.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: YES (2026-09-12 reconnect).** Created a fixture
  with three functions, deleted the middle one, and read the file back: the
  declaration was gone, `KeepFirst` and `KeepLast` were both intact, and
  exactly one blank line separated them — no doubled blank, no glued
  declarations. Response was `r2 → r3 · +0 -4` (three declaration lines plus
  the trailing blank). Unknown symbol returned `symbol not found:
  NeverExisted` and changed nothing. Fixture removed afterwards.

  Dogfood: all six source edits went through `jade_replace_text`, none
  needed repair. The bash that motivated this task is now one call.

- [x] **12.10 Add `apply(edits[], format?, check?)` — atomic multi-edit**
  Raised by the user 2026-09-12: "it almost looks like we need a batch
  activity tool. First do, then do, if no error do."

  Agreed on the need, with one deliberate narrowing. jade edits one site per
  call; adding a single tool touches ~6 files (protocol type, service, API
  handler, MCP dispatch, MCP schema, renderer, coverage registry), so every
  such task got scripted in `python3` instead — bypassing revision checks
  entirely. That fallback caused the only outage of the session: a scripted
  replace hit the wrong anchor, left a duplicate `fmt` import, and broke
  `go run ./cmd/jade-mcp` while the user was reconnecting.

  **Declarative pipeline, not a scripting DSL.** An arbitrary step-graph
  executor ("then, if no error, then") is bash with extra syntax: guardrails
  cannot inspect intent, telemetry degrades from "17 anchored edits, 2
  ambiguous-anchor failures" to "someone ran a script", and it would let an
  agent re-create the exact python that took the server down. Fixed,
  non-programmable semantics instead:
  1. verify every anchor resolves **uniquely** before writing anything;
  2. apply all or none — one stale revision or bad anchor aborts the set;
  3. format touched files (12.7);
  4. run one check (12.2) rather than one per edit;
  5. return one response: revision, per-file +/-, diagnostics, verdict.

  "If no error, then" is implicit — step N+1 runs only if N succeeded, and
  failure rolls back rather than half-applying. Ops: `replace_text`,
  `replace_range`, `replace_symbol`, `delete_symbol` (12.9), `insert`.
  Deliberately cannot: run arbitrary commands, loop, branch, or read a result
  to choose the next edit. An agent needing that makes two `apply` calls and
  thinks in between — which is observable.

  Picked up: 2026-09-12 — Added `internal/edit/apply.go` (`Service.Apply`,
  preflight, rollback, single post-batch check), `internal/edit/format.go`,
  `internal/code/insert.go` (proposal C folded in as the `insert` op),
  `protocol.EditOp`/`ApplyRequest`/`ApplyResponse`, `internalapi.Server.Apply`,
  a renderer, and the `jade.apply` MCP tool. Ops: `replace_text`,
  `replace_range`, `replace_symbol`, `delete_symbol`, `insert`.

  **Atomicity is rollback-based, not write-to-temp.** Every touched file is
  snapshotted before the first write and restored if any later step fails.
  That reuses the single-edit primitives completely unchanged, which is worth
  more than eliminating a brief window of partial state in a single-process
  tool.

  **Preflight checks what can be known before anything is written:** every op
  well-formed, every file readable, every anchor resolving *uniquely*. Anchor
  checking is skipped for a file an earlier op in the same batch already
  targets — that op may be what creates the anchor — and rollback covers the
  remainder. So a typo'd or ambiguous anchor across distinct files fails with
  zero state change, and only genuinely order-dependent failures need the
  undo.

  One revision bump and **one** validation for the whole batch. Six edits
  previously meant six typecheck jobs, five of them racing files still
  mid-change.

  `format.go` delivers **12.7's core**: `gofmt`/`rustfmt` over touched files,
  reporting only the ones whose content actually changed. A missing formatter
  is not an error — failing a batch because `gofmt` is absent would be worse
  than leaving a file unformatted.

  **A real off-by-one surfaced while testing:** `countLinesIn` counted a
  trailing newline as an extra line, so `"func B() {}\n"` reported 2 lines.
  It feeds `replace_text`'s removed-line count too, meaning every edit ending
  in a newline had been over-reporting by one since 12.1. Fixed at the
  helper.

  Unit tested: yes — 9 tests in `apply_test.go` and 7 in `insert_test.go`.
  The load-bearing ones: **rollback restoring a file byte-for-byte after a
  mid-batch failure** (the property that would have prevented the outage),
  ambiguous anchors rejected *before* any write rather than written-and-undone,
  an unknown op anywhere in the batch blocking the entire batch, one revision
  bump for many edits, and gofmt actually normalizing deliberately broken
  indentation.
  `gofmt`, `go build ./...`, `go vet ./...`, `go test ./...` all pass.

  **Verified live over MCP: no — needs reconnect** (brand-new tool).

- [x] **12.8 Add a fused search-and-read (`find`)**
  Raised by the user 2026-09-12 ("why don't you use the jade mcp tools for
  reads and edits?"). Edits had in fact moved to jade — 12.1's
  `replace_text` handled every source edit that session. Reads had not, and
  the reason is structural rather than habit: `grep -n "func decisiveLines"
  -A 30` performs **search and read in one call**, while jade needs
  `outline` then `read_symbol` — two round trips to answer "show me this
  thing I have not located yet". Add one call taking a symbol name or
  pattern and returning the matching definition bodies directly. Until then
  the shell wins the most common read an agent performs, which the 8.3
  benchmark scores as a single cheap `grep` while jade pays two calls.

  Picked up: 2026-09-12 — Added `internal/code/find.go`
  (`Index.FindSymbols`, `kindMatches`), `protocol.FindRequest`/`FindResult`/
  `FindResponse`, `internalapi.Server.Find`, a renderer, and the `jade.find`
  MCP tool. One call returns matching declarations **with their bodies**.

  **Exact matches win exclusively.** Asking for `Apply` returns `Apply`, not
  `Apply` buried among `ApplyRequest` and `Applier`. Substring matching only
  engages when nothing matches exactly.

  **Kind filtering needed an alias table, and finding that out was the
  useful part.** jade reports a Go struct as `class` and a Go func as
  `function` — vocabulary no caller would guess. Filtering by the natural
  `type` or `func` would have silently returned nothing, which is the worst
  failure mode for a search tool: indistinguishable from "no such symbol".
  `type`/`struct`/`class`/`interface`/`enum` are now one family and
  `func`/`fn`/`function` another.

  The renderer mirrors `grep -A` deliberately — location header then source
  — because that is the output this replaces and the shape callers already
  read fluently.

  Unit tested: yes — 9 tests: body returned not just location, exact-match
  exclusivity, substring fallback, kind filtering across every spelling in
  both families, true total when capped, truncation reported rather than
  silent, clear no-match summary, empty-query rejection, and **stable
  ordering across repeated calls** (unstable results make a response
  undiffable and a re-read look like a change).
  `gofmt` clean; build, typecheck and tests all pass.

  **Verified live over MCP: no — needs reconnect** (brand-new tool).

  Two of my own test premises were wrong and the tests caught both: I
  guessed jade's kind vocabulary, and I wrote a substring query (`"Appli"`)
  that does not actually occur in `"apply"`. Neither was a code defect.

- [x] **12.7 Run the formatter after edits**
  Found 2026-09-12 during 9.1. Four files had drifted out of `gofmt` shape
  through accumulated `replace_range` edits, and nothing caught it: `go
  build`, `go vet` and `go test` all pass on badly formatted code, so jade
  reported success at every step. An agent editing through jade will steadily
  degrade a repo's formatting and never be told. Run the language's formatter
  on touched files after a successful edit (scope.md Rule 2 names the
  formatter as a deterministic tool jade should be using), or at minimum
  surface a "formatting drifted" diagnostic in the edit response.

  Picked up: 2026-09-12 — 12.10 built `formatFiles` for batches; this extends
  it to **every** single-edit path. `Service.formatTouched` now runs in
  `ReplaceSymbol`, `ReplaceRange`, `ReplaceText`, `DeleteSymbol`, `CreateFile`
  and `Rename`, and `protocol.EditResponse` gained `Formatted`.

  **It runs before diagnostics are computed**, so reported line numbers match
  the file as it finally stands rather than the pre-format version — which
  would have been an off-by-N in every diagnostic on a file the formatter
  reflowed.

  `Rename` formats **every** file gopls touched, not just the declaring one:
  a rename rewrites call sites the caller never named, and those are exactly
  the files nobody would think to check.

  Only files the formatter actually rewrote are reported. That keeps the
  field meaningful as a signal — a formatter firing repeatedly on the same
  file says the agent is writing badly-shaped code, which a field that
  always listed every touched file could not tell you.

  `ReplaceSymbol` had been discarding the `Symbol` returned by
  `ReplaceSymbolSource`, so it had no file path to format; it now captures it.

  Unit tested: yes — 8 tests, the load-bearing one being
  `TestFormattingDoesNotFailTheEditWhenFileIsBroken`: gofmt refuses to parse
  a file the edit just made invalid, and the edit must still succeed and
  report itself as such, with diagnostics carrying the bad news. Failing a
  successful edit because a formatter could not run would be strictly worse
  than leaving it unformatted. Plus: single edit formats, already-clean code
  reports nothing, new files are formatted, delete leaves no stray blanks,
  formatter selection by extension, and unknown extensions skipped.
  `gofmt` clean; build, typecheck and tests all pass **via `jade_check`**.

  **Verified live over MCP: yes** (confirmed after the 2026-09-12 reconnect).
  Edit responses carry `· N formatted` when the formatter rewrote a file —
  seen on several `jade_apply` batches during 13.x, e.g. `2 formatted` when
  gofmt realigned a struct literal that an edit had widened.

  Deliberately not formatted: TypeScript, JSON, Markdown. Their formatters
  take project configuration, and running one unattended could reformat far
  more than the agent touched.

- [x] **12.5 Drop `Paths` from `changes()` now that `Files` exists**
  Picked up: 2026-09-12 — removed `Paths` from `ChangesResponse`, folding
  jade's edit ledger into `Files` instead of deleting it.

  **The task's premise was wrong.** `Paths` came from `m.Changes()` (jade's
  in-session edit ledger + git dirty); `Files` came from `DiffSummary()`
  (git only). They diverge in two real cases: a file jade edited and then
  reverted, and — the load-bearing one — git unavailable or failing, where
  `Files` is empty but jade knows exactly what it wrote. A straight delete
  would have made `changes()` answer "no changes" in a repo where git does
  not work but jade has been editing all session. That is a regression into
  11.3 ("degrade cleanly on non-Go repo"), which this phase gates.

  So: new `Manager.mergedChangedFiles()` unions the ledger into git's
  summary, zero counts when git sees no delta, and `Paths` is gone. `Files`
  is now the single path list. Renderer loses its `r.Paths` fallback branch
  and the 10.3 "deliberately not printed" comment; `cmd/jade/main.go`
  counts `Files`.

  **Unit tests: pass** (`go build`, `go vet`, `go test ./...` all clean).
  Four new: two in `workspace` covering the union (a ledger-only file is
  listed with zero counts; a file in both is listed once and keeps git's
  real counts), two in `render` (each file printed exactly once; a
  zero-count file is still listed and does not read as a clean tree).

  **Verified live over MCP: yes — and it caught a regression the unit tests
  missed.** After reconnect, `jade_changes` showed five new `+0 -0` rows:
  `internal/bench`, `internal/render`, `internal/textutil`,
  `internal/toolchain`, `cmd/jade-bench`. Those are *directories*.
  `git status --porcelain` collapses a wholly-untracked directory into a
  single `dir/` entry instead of listing the files under it, so jade's
  changed-path ledger legitimately contains directories. Harmless while the
  ledger was a `Paths` list the renderer never printed; folding it into
  `Files` made them visible as bogus changed files.

  Fixed with `Manager.isDirectory`, which filters them during the merge. A
  stat *failure* deliberately counts as not-a-directory: a path jade
  recorded that has since been deleted is still a file it changed, and
  dropping it would lose a real edit to hide a cosmetic one. Two more tests
  cover both halves. The fix itself is **not yet verified live** — it landed
  after the reconnect that exposed the bug, so the running server predates
  it. Observable on the next reconnect: the five `+0 -0` directory rows
  disappear from `jade_changes`.

  This is the clearest case so far for the loop's "dogfood live, not just
  the Go test" rule — the full suite was green on a response that was
  visibly wrong.

- [x] **12.11 Repo command registry**
  Picked up: 2026-09-12 — `.jade/commands.json`, two tools, and this repo's
  own registry seeded.

  New `internal/commands` package: `Load`/`Declare`/`Remove`/`Lookup`/`All`,
  name validation, bounded at 64 commands and 2000 characters per run
  string. A **missing** registry loads empty and is not an error (jade must
  work in a repo that never opted in); a **malformed** one fails loudly,
  because treating it as empty would answer "no command `build`" for a
  command the caller can see declared in the file.

  `internal/transport/internalapi/commands.go` adds `RunCommand` and
  `DeclareCommand`, reusing check()'s job runner, `checkTimeout` and
  `checkPassed` so declared commands get the same pass/fail verdict and
  decisive-summary extraction that build/typecheck/tests already got.
  MCP tools `jade.run_command` and `jade.declare_command`.

  **The design rule: `run_command` takes a name, never a shell string.**
  That is the entire value — it makes the surface enumerable (you can count
  which commands actually get used), reviewable (declared once into a file
  that shows up in a diff, not synthesized per call where nobody sees it),
  and teachable (an unknown name answers "no command `lnit`; declared:
  build, lint, test"). Declaration is the privileged operation and is a
  separate tool for exactly that reason.

  Three judgement calls worth recording:
  - **Runs via `sh -c`, not argv splitting.** Real project commands have
    pipes and `&&`; a registry that could not express them would send the
    caller straight back to bash, which is the failure this closes. The
    shell string is accepted once, at declare time, into a reviewable file.
  - **Empty name lists instead of erroring.** An agent that does not know
    the repo's vocabulary should be able to ask with the tool it already
    has, not guess a name to trigger the teachable error.
  - **`declare` bumps the revision.** The registry is a workspace change;
    leaving it alone would let an edit preconditioned on a stale revision
    sail through after the command surface changed underneath it.

  **Unit tests: pass** — `go build`, `go vet`, `go test ./...`, `gofmt -l`
  all clean. 22 new tests: 13 in `internal/commands` (persistence, sorted
  and stable listing, replacement reporting, bounds, idempotent remove,
  human-readable file), 9 in `internalapi` (declare/run round trip that
  checks the command actually ran **in the workspace root on disk**, pipes
  and `&&`, failure is a verdict not a tool error, unknown name lists the
  real ones, revision bump, rejected name writes no file).

  **Verified live over MCP: yes** (confirmed after the 2026-09-12 reconnect).
  `jade_run_command` with no name listed all four declared commands with
  their descriptions; `jade_run_command("vet")` returned `pass vet / no
  output`. Validation for this task's own successor ran through the
  registry rather than bash.

  Follow-up, not done here: telemetry. The counting this was built to
  enable needs somewhere to count *to*, which is its own task.
  `jade_check` covers build/typecheck/tests and nothing else, so every
  project-specific command — lint, vet, codegen, migrate — falls off the
  cliff back to raw bash. Observed this task: `go vet ./...` had to be run
  through bash because no `vet` kind exists.

  Proposal: `.jade/commands.toml` in the repo, written once by the agent,
  replayed by name thereafter.

  ```toml
  [build]   run = "go build ./..."
  [vet]     run = "go vet ./..."
  [lint]    run = "golangci-lint run"
  ```

  **`run_command` must take a name, never a shell string.** That single
  restriction is the whole value: it makes the surface enumerable (you can
  count which commands an agent actually uses), reviewable (a command is
  declared once, not synthesized per call), and teachable (an unknown name
  answers "no command `lint`; declared: build, vet, test" instead of a
  silent shell failure on a typo'd binary). Without it this is bash with
  extra steps.

  Also extends `jade_check`'s pass/fail + decisive-summary compression to
  commands that currently return raw output.

  Caveat: this is jade's first config-*writing* surface, and 11.5 freezes
  the tool surface — cheaper before the freeze than after.

---

## Phase 13 — Close the measured gaps

> **This phase also gates Phase 11.** Phase 12 closed the capability gaps
> that were obvious from use. Phase 13 closes what the consolidated
> `feedback.md` ranks as still open after that — including the one that
> makes every other ranking a guess. Priority order is from feedback.md's
> "Open, in priority order", ranked by what a gap costs when hit, not how
> often it is hit.

- [x] **13.1 `grep` — real text search**
  Picked up: 2026-09-12 — new `Index.Grep`, `jade.grep` tool, and an honest
  description on `search`.

  **The finding that motivated it.** `jade_search` was already in the
  catalog, described as "hybrid search over symbols and files", so three
  tasks of feedback had logged this gap as "no string search" without
  testing whether `search` covered it. It does not, and it fails in the
  worst possible way. Asked for the exact string `ChangedPaths` in `exact`
  mode it returned `ChangedFile`, `CheckRequest`, `CommitInfo` — ten
  declarations ranked by *name similarity*, none of which contain the
  string — while missing both the struct field actually called
  `ChangedPaths` and all eight files that read it. That is not a weak
  answer; it is a confident answer to a different question, which is worse
  than no tool at all: a caller who tests it once learns to distrust it and
  returns to grep permanently. Which is exactly what the log shows happened.

  `Index.Grep` is literal or regex line search across the workspace, reusing
  the file walk, skip rules and text detection `Search` already had.
  Supports `regex`, `ignoreCase`, `glob` (matched against both base name and
  full relative path, so a caller need not guess which spelling is
  preferred), `exclude` (plain substring — what `| grep -v testdata`
  actually did), and `context` (trailing lines, like `grep -A`, capped at
  40). Renders as `path:line: text` with context indented beneath.

  Deliberate choices: a bad regex is an **error naming the pattern**, never
  a silent fallback to literal matching — silently matching something other
  than what was asked is the failure mode that makes a search tool
  untrustworthy, and this tool exists because of one. `Total` always reports
  the true count even when `Limit` cuts the returned set. Results sort by
  path then line so a repeated search is diffable. Single lines clip at 400
  characters so one minified file cannot dominate a response.

  Also rewrote `search`'s tool description to say plainly that it matches
  symbol names only, does not read file contents, and returns name-similar
  declarations even when none contain the query — pointing at `grep` for
  text and `find` for a declaration body. Whether `search` earns a place
  beside those two at all is a question for 11.5's freeze.

  **Unit tests: pass** — `go build`, `go vet`, `go test ./...`, `gofmt -l`
  all clean. 16 new tests, including the two that encode the defect: every
  returned line actually contains the query, and a struct field `search`
  cannot reach is found in all three files that mention it.

  **Verified live over MCP: yes** (confirmed after the 2026-09-12 reconnect).
  `jade_grep("ChangedPaths", glob="*.go", exclude="_test")` returned 12
  matches in 6 files — including the struct field declaration at
  `internal/protocol/types.go:426` and every file that reads it. That is
  precisely the set `jade_search` missed while confidently returning ten
  unrelated declarations, so both halves of this task are now verified live:
  the defect before, and the fix after.

- [x] **13.2 Telemetry**
  Picked up: 2026-09-12 — `internal/telemetry`, recording at the MCP
  dispatch chokepoint, plus a `jade.telemetry` tool to read it back.

  Records one JSONL line per tool call to `.jade/telemetry.jsonl`: tool
  name, wall time, response bytes, and a failure **class**. `Summarize()`
  aggregates per tool (calls, errors, bytes, average and max latency) and
  rolls failure classes up into a single `Fallbacks` list — the headline,
  since each entry is a moment the jade path failed and the shell was one
  keystroke away. Classes: `ambiguous`, `stale_revision`, `timeout`,
  `not_found`, `unavailable`, `invalid_input`, `other`.

  **Recorded at `handleToolCall`, not per handler.** It is the single point
  every call passes through; a per-handler approach would drift the moment a
  tool was added, and the newest tool is always the one most worth
  measuring. Split the old function into an instrumented wrapper plus
  `dispatchToolCall`.

  **What is deliberately not recorded: arguments, response bodies, error
  message text.** Arguments carry source, paths and queries; error messages
  routinely quote the source line they failed on. A log containing those is
  a copy of the repository by another name — it could not be attached to a
  bug report, shared, or committed, which defeats the entire reason for
  collecting it. Content-free is what makes it shareable, and shareable is
  the point. There is a test that writes a fake API key through the failing
  path and asserts it does not reach the file.

  Other decisions: `JADE_TELEMETRY=0` disables it (an off switch is not
  optional for something that writes into the user's repo on every call);
  a failed write is swallowed, because failing a working edit to save a
  measurement would make jade less reliable than the bash it competes with;
  `Summarize` by contrast *does* report a read error, since answering "no
  calls recorded" for a log that exists but could not be read is a confident
  lie about the one thing the caller asked. Log truncates at 5MB rather
  than rotating. `.jade/telemetry.jsonl` added to `.gitignore` — note that
  `.jade/commands.json` is the opposite and *is* meant to be committed.

  **Unit tests: pass** — `go build`, `go vet`, `go test ./...`, `gofmt -l`
  all clean. 12 in `internal/telemetry` plus **6 end-to-end in
  `cmd/jade-mcp`** — the first tests that package has had. The end-to-end
  ones matter more: a recorder that works perfectly but is never called
  measures nothing, so they drive real calls through `handleToolCall` and
  read the log back through `jade.telemetry`.

  Two bugs the tests caught, both real: `Classify` matched
  `"executable file not found"` as `not_found` because `NotFound` was tested
  before `Unavailable` — which would have pointed a reader at the wrong fix
  (correct a name, rather than install something). And an unknown tool name
  is now recorded by name, which is the most direct evidence of a gap this
  log can carry: the agent asked for a capability jade does not have.

  **Verified live over MCP: yes** (confirmed after the 2026-09-12 reconnect).
  `jade_telemetry` reported real calls accumulating across the session —
  per-tool counts, response bytes and average latency — written by the live
  server to `.jade/telemetry.jsonl`, not by a test harness.

  First real finding from the instrument, which no hand-written log would
  have produced: `jade.changes` cost **4.6KB and 704ms**, an order of
  magnitude more than any other call (`grep` 990B/19ms, `read_range`
  144B/16ms). On a repo with 88 changed files it renders every one plus
  nested symbol changes. Worth a cap or a summary mode — filed as 13.8.

- [x] **13.7 Failures returned as successful responses are invisible**
  Picked up: 2026-09-12 — `telemetry.ClassifyResponse`, read at `jsonResult`.

  Taken before 13.3 because it appears next in the file and because it is
  what makes 13.2's numbers trustworthy; every later reading of the log
  depends on it.

  **The audit result: in-band failure is deliberate, not a bug.**
  `read_symbol` resolves a name to `exact` / `ambiguous` / `not_found` and
  returns the near-miss candidates alongside — which is more useful than an
  error that throws them away. So the fix is not "make it error", it is
  teaching the recorder to read the outcome the response already states.
  `SymbolResolution` on `InspectResponse` is the only such field in the
  protocol; every other `not found` in `internalapi` and `code` is a real
  returned error.

  `ClassifyResponse(value)` is read in `jsonResult` — the last point at which
  the concrete response type still exists before it becomes wire text — and
  travels to `handleToolCall` on an unexported field of `mcpToolResult`, so
  it never reaches the wire. A returned error always wins over an in-band
  outcome: it is the stronger signal, and a handler that errored never
  produced a typed response to read.

  **What is deliberately excluded, and why it matters more than what is
  included:**
  - A `check`, `run_tests` or `run_command` reporting `Passed=false` is a
    **verdict, not a fallback**. A red build is jade doing its job. Counting
    it would swamp the signal with ordinary broken code and make the number
    meaningless. Has its own end-to-end test that deliberately builds a
    broken package and asserts zero fallbacks recorded.
  - An empty `grep` or `find` result. "Nothing matches" is a correct and
    often expected answer; counting it would punish the tools for being
    asked honest questions.

  **Known limitation, left alone on purpose:** a waited `check` that times
  out returns `Status: "running"`, which is indistinguishable in the
  response from a deliberate `wait=false` call. Classifying it would mean
  guessing, and a wrong guess pollutes the exact metric this task exists to
  clean up. Recorded here rather than fixed by inference.

  **Unit tests: pass** — `go build`, `go vet`, `go test ./...`, `gofmt -l`
  all clean. 6 new in `internal/telemetry`, 2 new end-to-end in
  `cmd/jade-mcp` (now 8 there). The regression test drives a real
  `read_symbol` miss through the dispatch path and asserts it is counted,
  because the bug was in the wiring rather than in any classifier.

  **Verified live over MCP: yes** (confirmed after the 2026-09-12 reconnect).
  Called `jade_read_symbol` for a declaration that does not exist — it
  answered `not found: NotADeclarationHere` as a normal response, as
  designed — then `jade_telemetry` reported
  `jade.read_symbol 1 errors (not_found 1)` and `fallback risk: not_found 1`.
  Before this fix that call recorded as a clean success. Both the defect and
  the fix are now verified against the live server.

  Note the shape of this one: 13.2 shipped telemetry, and telemetry's own
  first tests immediately found that telemetry was undercounting. That is
  the instrument working on its first use.
  Found while testing 13.2. `jade_read_symbol` with a bogus symbol ID
  answers `not found: nowhere.go::Missing@1` — rendered as a normal
  response, with no error returned from the handler. Telemetry therefore
  counts it as a success, which understates precisely the number the log
  exists to measure.

  Not yet established how many tools do this; the `not found` errors in
  `internalapi` and `code` are all returned as real errors, so this may be
  narrow. Needs an audit of which tools report failure in-band rather than
  as an error, then either making them error or teaching the recorder to
  read the outcome from the response. Worth settling before 11.5 freezes
  the surface, since "does this tool error or answer" is part of the
  contract.
  `feedback.md` is hand-written; every number in it is recollection, which
  is the unreliable instrument the tooling was meant to replace. The stated
  reason for building a wide tool surface was so telemetry and guardrails
  could be added to it, and that half has not started.

  Minimum useful version: per tool, count calls, response bytes, wall time
  and errors; and specifically log errors that plausibly send the caller
  back to bash — ambiguous anchor, not found, stale revision, timeout.
  That last category is the direct measurement of feedback.md's subject.

  **Why this outranks everything else left:** every other item is a guess
  without it, including their ranking. It is also the only item whose
  absence gets worse with time — the pre-telemetry period is unrecoverable.

- [x] **13.3 Whole-file read**
  Picked up: 2026-09-12 — `read_range` with no bounds reads the whole file.

  Generalized rather than special-cased: an unset start means "from the
  beginning", an unset end means "to the end", so a call with neither reads
  everything and `startLine` alone reads to EOF (`sed -n '10,$p'`). Only
  `path` is required now.

  **Zero means unset, negative is an error.** An omitted integer arrives
  over JSON-RPC as zero, so zero has to mean "unset" for omission to be
  expressible at all — but a negative is a real caller mistake and is still
  reported. This flipped one existing test that asserted `start=0` was
  invalid; the assertion encoded the old contract, so it was rewritten to
  cover the negative case instead.

  An explicitly requested end past the last line stays an error rather than
  clamping: the caller stated a belief about the file's length, and being
  told it is wrong beats quietly getting less than was asked. Omitting the
  end states no such belief, so there is nothing to correct.

  Output clamps at 20,000 bytes via `textutil.Clamp`, with the hint
  "request a narrower line range". Large enough for any configuration file
  — the reason this exists — and small enough that hitting it is a signal to
  ask a narrower question.

  **Unit tests: pass** — `go build`, `go vet`, `go test ./...`, `gofmt -l`
  all clean. 6 new: whole file, start-only, end-only, clamp on a huge file,
  no clamping of small files, and an empty file reading empty rather than
  erroring.

  **Verified live over MCP: yes.** `jade_read_range("go.mod")` with no line
  numbers returned the whole file — the exact call that was a `cat` in the
  three previous tasks' feedback entries.
  `read_range` needs a range, `read_symbol` and `find` need a symbol.
  `go.mod`, `Makefile`, `.mcp.json` and every JSON/YAML/TOML config have
  neither, so each is a `cat` — and these are exactly the files an agent
  lands on when orienting in an unfamiliar repo. Likely one line:
  `read_range` with no range means the whole file, clamped.

- [x] **13.8 `changes()` is the most expensive call jade makes**
  Picked up: 2026-09-12 — symbol delta skipped above 25 changed files,
  file list capped at 40 in the renderer.

  Two separate costs, fixed separately:

  **Latency (704ms).** `changedSymbols` parses both the committed and
  working copy of every changed file. Now skipped entirely above
  `maxSymbolDeltaFiles = 25`. The cutoff is not only about cost — past that
  many files the renderer's existing 40-symbol cap reduces the result to an
  arbitrary sample (40 of 683, chosen by file order), and an arbitrary
  sample presented as a summary is worse than no summary because it reads
  like the whole answer.

  **Bytes (4.6KB).** Symbols were capped from the start; the file list never
  was, so 88 changed files printed 88 lines. Capped at 40 with
  `... N more files · use diff for the full patch`.

  **Degrades automatically, not behind a flag.** The pathological case is
  exactly the one where a caller is least likely to know to ask for the
  cheap variant. But it says so: `SymbolsOmitted` carries the file count and
  the renderer prints `symbol changes omitted (88 files changed) · use
  outline on one file`. A zero-valued `Symbols` with no omission still means
  "nothing changed", which is a different claim and must never be collapsed
  into the other — both directions have tests.

  **Unit tests: pass** — build, vet, tests and gofmt all clean. 6 new: four
  renderer tests (cap applied, cap not applied to a small list, omission
  stated, silence when nothing was actually omitted) and two server tests
  for the cutoff in both directions. The server tests seed jade's own edit
  ledger rather than git, since the cutoff is about file count and should
  not need a repository to exercise.

  **Verified live over MCP: no — needs reconnect.** Baseline recorded from
  the live telemetry log for comparison afterwards:
  `jade.changes 1 calls 4.6KB avg 704ms` against 88 changed files. Both
  numbers should fall sharply on the next reconnect; if they do not, the
  cutoff is not being reached and that is a real bug rather than a stale
  process.

  **First task with zero bash calls.** All three validations ran through
  `jade_run_command(vet)`, `jade_run_command(fmt-check)` and
  `jade_check(tests)`.
  Found by telemetry on its first live session, which is exactly the kind
  of finding no hand-written log produces. `jade.changes` cost **4.6KB and
  704ms** against this repo's 88 changed files — versus `grep` at 990B/19ms
  and `read_range` at 144B/16ms. It renders every changed file plus nested
  symbol changes, and the symbol cap of 40 does not bound the file list at
  all.

  Options: cap the file list the way symbols are capped, add a summary mode
  returning only counts, or make the symbol detail opt-in. Worth settling
  before 11.5 freezes the surface. Note this is a response-shape question,
  not a correctness one — the output is right, just expensive.

- [x] **13.4 `check` success summary should count, not tail**
  Picked up: 2026-09-12 — `verdictSummary` splits the passing and failing
  cases; `successSummary` counts.

  The decisive-line extractor falls back to the **last three lines** when
  nothing looks like a failure, which is right for a failing run and wrong
  for a passing one: an all-green `go test ./...` across 42 packages
  summarized to whichever three `ok` lines sorted last. Correct, and it
  reads as a partial run — confirming it was not cost a `cat Makefile` and a
  redundant `go test` during 12.11.

  On failure nothing changes: the compiler diagnostic or FAIL line is
  exactly what the caller needs, and counting it would destroy the only
  useful part. On success:
  - recognised per-package result lines are counted — `42 packages ok`,
    `1 packages ok, 2 with no test files`;
  - output of three lines or fewer is shown verbatim, because `go build`
    and `gofmt -l` print nothing when clean and a one-line success message
    is worth more than a count of it;
  - anything else reports its size (`20 lines of output`) rather than a
    slice of itself — a sample of a log nobody asked for is the thing being
    removed, so replacing one arbitrary sample with another would miss the
    point entirely.

  Applied at all three call sites that share `checkPassed` — `Check`,
  `RunTests` and `RunCommand` — so they cannot drift.

  **Unit tests: pass** — build, vet, tests, gofmt all clean via
  `run_command` and `check`. 6 new, including the failure case asserting the
  decisive line survives untouched.

  **Verified live over MCP: no — needs reconnect.** The stale server
  captured a clean before-picture, which is the bug itself:
  `jade_check(tests)` answered `pass tests` followed by three `ok` lines out
  of the whole module. After reconnect the same call should read
  `pass tests` / `N packages ok, M with no test files`.

  Second consecutive task with zero bash calls.
  `check(tests)` on a green suite prints the last three `ok` lines, because
  the decisive-summary extractor keeps the tail. Correct, and it looks
  broken — it cost a `cat Makefile` plus a redundant `go test` to confirm it
  was not. Success should summarize as a count ("42 packages ok"); failure
  should keep the current behaviour, which is good.

- [x] **13.5 Whole-file overwrite**
  Picked up: 2026-09-12 — `jade.replace_file`, symmetric with `create_file`.

  **No force flag, by design.** `create_file` refuses when the file exists;
  `replace_file` refuses when it does not. Neither can do the other's job by
  accident, so choosing the tool *is* the explicit act — which is a better
  guardrail than a boolean nobody reads, and keeps both error messages able
  to name the right tool instead of a flag.

  Why it was needed at all: every anchored edit needs something to anchor
  on. `create_file` cannot overwrite, `replace_text` and `apply` need text
  already in the file, and none of them can express "this document now says
  something else" — a rewritten README, a regenerated fixture, a
  consolidated notes file. The gap was total rather than awkward, with no
  partial workaround, so it fell to a raw shell write every time. It is what
  rewrote `feedback.md` during the 13.x consolidation.

  Goes through the normal edit path, so it formats, bumps the revision,
  computes diagnostics and starts a validation job like any other edit. It
  reports `RemovedLines` as well as `AddedLines` — unlike `create_file` —
  because the caller is destroying content and its size is the one fact the
  response could not otherwise carry.

  **Unit tests: pass** — build, vet, tests, gofmt clean via `run_command`
  and `check`. 6 new: overwrite, destroyed-line accounting, the refusal to
  create (asserting no file is left behind and that the error names
  `create_file`), revision bump, formatting of the rewrite, and the trailing
  newline matching `create_file` so the two cannot disagree.

  **Verified live over MCP: no — needs reconnect** (new tool). Observable
  once live: `jade_replace_file` on a non-existent path should refuse and
  point at `create_file`.

  Third consecutive task with zero bash edits; one bash call, to read
  verbose output of a single test by name — `run_tests` has a `test` scope
  that would cover it.
  `create_file` refuses to overwrite by design, and `replace_text`, `apply`
  and `insert` all need an anchor. Rewriting a document wholesale — a
  README, `feedback.md`, a generated fixture — has no jade path at all.
  Narrow but total: there is no partial workaround, so it falls to `Write`
  every time.

- [x] **13.6 Residual git gaps** — arbitrary-revision diff shipped, blame
  deliberately not built
  Picked up: 2026-09-12.

  **`diff(since)` — built.** `DiffRequest.Since` takes any git revision
  (`HEAD~3`, a branch, a SHA). The question it answers is "what has this
  branch done", which the working-tree diff structurally cannot express: a
  task that commits midway disappears from `git diff HEAD` while being
  exactly what the caller wanted to see. `git diff <rev>` spans the revision
  through to the working tree, so half-committed work reads as one change.

  Two deliberate choices: untracked files are **not** folded in on the
  revision path the way they are for the working tree — the caller asked
  what changed between committed states, and quietly adding files in neither
  answers a different question. And an unknown revision is reported with the
  spelling the caller used rather than as raw git output, since the usual
  cause is a typo. `Diff(target)` is now a thin wrapper over `DiffSince`, so
  the old path cannot drift; a test asserts the two are identical when
  `since` is empty, untracked files included.

  **`blame` — not built, and I do not think it should be.** Stating this
  rather than quietly dropping it, since the task listed both:
  - `history` (9.3) already answers the question blame is actually used for
     — "why does this code exist" — via `git log -L` scoped to a symbol, and
    answers it better, with the commit messages attached.
  - What blame adds beyond that is per-line authorship, which is rarely
    decisive for an agent: knowing *who* last touched a line does not change
    what the code should do.
  - Its output is verbose per line, and 13.8 just established that response
    cost is jade's least-noticed failure mode. Adding a verbose tool of
    marginal value immediately before the 11.5 freeze is the wrong trade.

  The backlog entry itself recorded that this came up twice in sixteen
  tasks, both times exploratory. The diff half had a concrete recurring use;
  the blame half did not. **If you disagree, say so and it is a small task
  — the decision is yours, not mine.**

  **Unit tests: pass** — build, vet, tests, gofmt clean via `run_command`
  and `check`. 5 new covering committed-only work, committed plus pending,
  the path filter, the bad-revision message, and the no-drift equivalence.

  **Verified live over MCP: no — needs reconnect.** `since` is a new
  parameter on an existing tool, so the running server's schema and dispatch
  both predate it. Observable once live:
  `jade_diff(since="HEAD~1")` returning committed work that
  `jade_diff()` alone does not show.

  Used `jade_run_tests(scope=file)` for the first time this task — the
  fallback flagged as habit in each of the last three feedback entries. Zero
  bash calls.
  No `blame`, no arbitrary-revision diff (`diff HEAD~3`). Came up twice in
  sixteen tasks, both exploratory. Lowest priority; may not be worth
  shipping before the freeze.

---

## Phase 11 — Roadmap to a 0.0.1 release

Goal: a version of jade that can be installed and pointed at a repo other
than this one, so it can be used in other projects, integrated into
droneship-next, and — above all — generate real outside feedback. Everything
here is about *shipping what exists*, not adding capability. Phases 7-9 are
explicitly deferred behind this: an unreleased tool gathers no feedback, and
feedback is what should decide what gets built next.

**Gate: 8.3 (the benchmark harness) still ranks above every feature item in
Phases 7 and 9, but it does not block 0.0.1.** Release first, measure and
extend after — real usage will produce better benchmark scenarios than
anything invented here.

- [x] **11.1 Ship a real binary instead of `go run`**
  Picked up: 2026-09-12 — `make binary`, `make install`, `--version`, and a
  documented binary-based MCP stanza.

  `make binary` produces `bin/jade-mcp` with the version stamped via
  `-ldflags -X main.version=$(git describe --tags --always --dirty)`,
  falling back to `dev` outside a git checkout rather than failing the
  build. `make install` does the same through `go install`. `--version` is
  answered before anything else is constructed, so it works even when the
  workspace root is wrong or missing — which is exactly when someone is
  trying to find out what they are running.

  **`make build` deliberately still means `go build ./...`.** It is what
  jade's own `check(build)` discovers and runs, so it has to compile
  everything fast rather than produce an artifact. Renaming it would have
  silently broken jade's validation of itself.

  **Startup latency, measured:** binary **14/14/15ms** to first handshake
  versus **584ms then 284ms** for `go run`, with a warm build cache. Roughly
  20–40x, and a cold cache is seconds.

  **This repo's own `.mcp.json` deliberately still uses `go run`.** The task
  asked to *document* the binary stanza, and switching the local config
  would make dogfooding worse, not better: `go run` picks up source changes
  on reconnect, while a binary would need `make binary` first and would
  otherwise serve stale code silently — the exact failure mode that took the
  server down earlier in this project. Documented both, with the reason for
  each. **Say if you want the local config switched; it is a one-line
  change.**

  README gains an Install section: build, install, the JSON stanza with
  absolute paths, and an explicit note that `JADE_WORKSPACE_ROOT` need not
  be the jade checkout — which is the point of 11.2.

  **Unit tests: pass** — build, vet, tests, gofmt clean via `run_command`
  and `check`. One new test asserting `version` is never empty ("dev" is a
  correct and informative answer; empty would make `--version` look broken).

  **Verified live: yes, outside MCP — which is the right place for this
  one.** The deliverable is the binary, so it was exercised directly:
  `./bin/jade-mcp --version` prints `jade-mcp 9e60033-dirty`, and a real
  `tools/list` handshake against the binary returns **all 34 tools**,
  including everything added in Phase 13 (`grep`, `telemetry`,
  `replace_file`, `run_command`, `declare_command`). The running MCP server
  is still the `go run` one and is unaffected.

  Also declared `binary` in the repo command registry
  (`make binary && ./bin/jade-mcp --version`) and ran it through
  `jade_run_command` — so the release build is now part of the same
  telemetered surface as vet and fmt-check. Zero bash edits; two bash calls,
  both measurement (latency timing loop, tool-catalog probe) rather than
  work jade should be doing.
  `.mcp.json` currently launches jade with `go run ./cmd/jade-mcp`, which
  recompiles at every process start and is why every source change in this
  whole session needed a reconnect. Add `make build` producing a `jade-mcp`
  binary, document the binary-based `.mcp.json` stanza, and confirm startup
  latency is acceptable for an MCP handshake.

- [x] **11.2 Make the workspace root configurable and not assumed**
  Picked up: 2026-09-12 — `--root` flag, reported source, validated root.

  New `cmd/jade-mcp/root.go`. Precedence is `--root`, then
  `JADE_WORKSPACE_ROOT`, then cwd. The flag wins because it is the more
  specific statement: an env var is usually inherited from a shell or a
  client config and may not have been written with this invocation in mind.
  Accepts `--root DIR`, `--root=DIR` and the single-dash spellings.

  **The cwd fallback stays, but is no longer silent.** Every startup prints
  the resolved root *and where it came from* to stderr — never stdout, which
  carries the JSON-RPC framing. Silently operating on whatever directory a
  client happened to launch from is how an agent ends up confidently editing
  the wrong repository, and that failure is invisible until something is
  already wrong.

  Validation: a missing root or a path that is a file is fatal and the error
  names **both the path and which of the three sources supplied it** —
  otherwise the operator has to guess which of three places to fix. An empty
  `--root=` is an error rather than a fall-through, since someone who passed
  the flag meant to choose a directory.

  **Non-git root warns, does not fail.** The task said "a clear error"; I
  made it a clear *warning* instead, and the reasoning is: outline, find,
  grep, read and every edit operation work on any directory, and jade's
  revision tracking is its own counter rather than git's. Refusing to start
  would make jade unusable somewhere it serves perfectly well. But it is
  said plainly at startup and names exactly what degrades — `changes`,
  `diff`, `history`, `checkpoint` — because those fail in ways that look
  like "nothing changed" rather than like an error. **Say if you want it
  fatal instead.**

  Roots are made absolute and symlink-resolved. That last part is not
  cosmetic: on macOS `/tmp` is a symlink to `/private/tmp`, and without
  resolution the root disagrees with the paths git reports, making every
  relative path jade computes wrong.

  **Unit tests: pass** — build, vet, tests, gofmt clean via `run_command`
  and `check`. 12 new covering precedence, all four flag spellings, the
  empty-flag rejection, missing and non-directory roots naming their source,
  the non-git warning naming what degrades, a **worktree `.git` file**
  counting as a repository (it is a file, not a directory, in a worktree or
  submodule — treating only directories as real would warn spuriously in
  every worktree), absoluteness, and an unreadable cwd.

  **Verified live: yes — against a foreign repository, which is the actual
  gate this task names.** Built the binary, created a throwaway **Python**
  git repo elsewhere on disk, and ran a real `tools/call` handshake:

  ```
  jade-mcp · workspace /private/var/…/otherproj (from --root flag)
  {"result":{"content":[{"text":"r1 · drifted: 0 files\n4-6 class Greeter"}]}}
  ```

  Also confirmed live: a non-existent root exits with
  `workspace root does not exist: /no/such/place (from --root flag)`, and a
  non-git root starts with the degradation warning. jade has now run
  somewhere other than its own checkout for the first time.

  **Finding for 11.3, from that same run.** The Python outline returned
  `class Greeter` and silently omitted `def greet` at line 1. jade has
  tree-sitter grammars for Go, TypeScript and Rust only; Python falls to a
  heuristic path that caught one declaration kind and not the other. The
  problem is not the gap — it is that the response looks like a complete
  outline. A partial answer presented as a whole one is worse than saying
  "this language is not parsed", and 11.3 should fix the *reporting* before
  worrying about the grammar.
  Verify jade works when launched against an arbitrary repo: root from CLI
  flag or env var, no silent dependence on cwd, and a clear error when the
  root isn't a git repository. This is the actual gate on "use it in other
  projects" — it has never been run anywhere but `/Projects/jade`.

- [x] **11.3 Degrade cleanly on a non-Go repository**
  Picked up: 2026-09-12 — parser provenance surfaced, plus a worse bug this
  uncovered live.

  **Fix 1 — say which parser ran.** New `protocol.ParserInfo`
  (language, parser, complete, note) returned from `OutlineStructured` and
  carried on `InspectResponse`. A grammar parse says nothing at all; the
  heuristic emits `! no python grammar — declarations were found by a text
  scan and some may be missing`.

  Three deliberate details:
  - **The caveat renders *above* the outline.** The failure mode is trusting
    an absence, so a reader who stops at the declaration they were looking
    for must already have seen that the list may be incomplete. A footnote
    after the data is read too late to prevent the error.
  - **Silence on a grammar parse.** A caveat printed on every Go file would
    be ignored by the time it mattered.
  - **A `.go` file that fell back is reported differently** — "go grammar
    did not parse this file (often a syntax error)" rather than "no go
    grammar", which would send the reader looking for the wrong thing.
    Provenance comes from what actually happened, not from the extension.

  **Fix 2 — raw localized git stderr in every response.** Found by running
  the 11.3 verification against a freshly `git init`ed repo. `Freshness`
  puts `HeadCommit`'s error into every inspect and outline response, and on
  a repository with no commits yet `git rev-parse HEAD` prints a four-line
  "ambiguous argument 'HEAD'" diagnostic — **which arrived in German**, since
  git localizes. Every single response began with four lines of that.

  `HeadCommit` now uses `--verify --quiet`, which exits non-zero with empty
  output instead of printing a diagnostic, and returns `ErrNoCommits` or
  `ErrNotARepository`. The distinction is drawn from **exit status, not from
  parsing git's text** — no locale-dependent string matching would have
  survived contact with a German git. A brand-new repository is about the
  most common state there is, so this was a genuine release blocker.

  **Unit tests: pass** — build, vet, tests, gofmt clean via `run_command`
  and `check`. 11 new: 5 for parser provenance (grammars complete, language
  named, failed-grammar vs absent-grammar, unknown extension, end-to-end
  through `OutlineStructured`), 2 for renderer ordering and silence, 4 for
  HEAD resolution including one asserting the freshness string stays under
  80 characters and single-line — its *length* is what actually matters,
  since it prefixes every response.

  **Verified live: yes, against a foreign fresh repo.** Before:

  ```
  r1 · freshness unknown: Schwerwiegend: mehrdeutiges Argument 'HEAD'…
  (4 lines) … 4-6 class Greeter
  ```

  After:

  ```
  python: r1 · freshness unknown: repository has no commits yet
          ! no python grammar — … some may be missing
          4-5 class Greeter
  go:     r1 · freshness unknown: repository has no commits yet
          3-3 function Works
  ```

  Python warns, Go stays silent, and the git noise is one short line.

  **Not done, deliberately: no new grammars.** Adding Python/Java/C++
  tree-sitter grammars is a separate decision with real weight — binary
  size, build time, per-language symbol-kind vocabularies — and the point of
  this task was that the *silence* was the bug. An honest heuristic is
  usable; a dishonest one is not. Grammars can follow feedback from real
  use, which is what 0.0.1 exists to gather.

  Original finding from 11.2, for the record: against a Python repo,
  `jade_outline` returned `4-6 class Greeter` and silently omitted
  `def greet` on line 1. Tree-sitter grammars exist for Go, TypeScript and
  Rust; everything else falls to a heuristic that caught one declaration
  kind and not the other.

  The bug is the *silence*, not the gap. A partial outline presented as a
  complete one is worse than an honest "this language is not parsed" — an
  agent trusts the empty space and concludes the function does not exist.
  Fix the reporting first: say which parser handled a file and whether it is
  a real grammar or the fallback. Adding grammars is a separate, later,
  optional decision.
  Discovery already handles npm and cargo (4.x), but validation defaults and
  `run_tests` still assume Go in places, and gopls paths must no-op rather
  than error. Test jade end-to-end against a TypeScript repo — droneship-next
  itself is the obvious candidate and the integration target.

- [x] **11.4 Write the install and integration README**
  Picked up: 2026-09-12 — README reopened as a user-facing document, plus a
  `docs-check` command that keeps the tool list honest.

  The README opened as a scaffold-status page ("Status: Initial concept /
  team briefing", a list of placeholder packages) — accurate a while ago,
  actively misleading for a release. It now leads with what jade is, the
  two-command install, the client stanza, the tool list by category, and
  the limitations. The old internal material is kept, demoted under
  "Internals and design notes" rather than deleted.

  **The "what Jade does not do yet" section is the point of the task**, and
  it is specific rather than modest: grammars only for Go/TS/TSX/Rust with
  an honest fallback, `references`/`rename` precise only for Go via gopls,
  no LSP client, no blame, formatting limited to gofmt and rustfmt and why,
  revision tracking being jade's own counter rather than a VCS, telemetry
  being local-only and how to switch it off, and not hardened for untrusted
  input. It closes by asking for "this made me reach for bash instead"
  reports specifically — that is the feedback loop this whole project runs
  on.

  The intro leads with the benchmark number (0.85x shell tokens, from
  5.63x) because it is the one claim that is measured rather than asserted.

  **Cross-checked the tool list against the running binary, and it was
  wrong.** A `tools/list` handshake versus the README found `insert`
  documented as a tool when it is only an `apply` op, and `search_nudge`
  missing entirely. Both fixed. A README listing a tool that does not exist
  is worse than no list — it sends a reader to look for something that was
  never there.

  So the check is now a declared command, `docs-check`, which diffs the
  binary's real catalog against the README section and exits non-zero on
  any undocumented tool. It runs through `jade_run_command` like vet and
  fmt-check, so the docs cannot drift silently as the surface changes —
  which matters immediately, since 11.5 is about freezing that surface.

  **Unit tests: pass** — tests, vet, fmt-check, and `docs-check` all green
  through jade's own tools. No new Go code, so no new Go tests; the
  executable check for this task is `docs-check` itself.

  **Verified live: yes** — `jade_run_command(docs-check)` returns
  `pass docs-check / all tools documented`, having built the binary and
  compared its 34-tool catalog against the README.
  What jade is, the one-command install, the `.mcp.json` stanza, the tool
  list with one line each, and an explicit "what jade does not do yet"
  section. The honest limitations list matters more than the feature list
  for feedback quality — it tells reporters what is worth reporting.

- [x] **11.5 Pin a tool-surface contract for 0.0.1**
  Picked up: 2026-09-12 — `docs/tool-contract.md`, a golden test that
  enforces it, and one schema bug fixed before it could be frozen in.

  **The freeze found a lying schema, which is the best possible argument
  for doing it.** `replace_text` and `replace_range` declared
  `expectedRevision` **required**, while the server treats an empty value
  as "no precondition" — exactly as `apply` and `replace_symbol` do. So the
  schema stated a contract the implementation does not enforce, which is
  worse than either choice alone: a client that trusts the schema sends a
  value it never needed, and one that does not is told it is wrong when it
  is not. Fixed by making the schema match the behaviour, because the
  behaviour was right — a one-shot edit should not have to fetch a revision
  first. `expectedRevision` is now optional and recommended everywhere.

  Freezing that as-is would have published the inconsistency and made
  fixing it a breaking change.

  **Enforcement: `cmd/jade-mcp/contract_test.go`.** A golden map of all 34
  tool names to their required arguments, deliberately hand-written rather
  than generated — a test that regenerates its own expectation cannot make
  a change deliberate, which is the entire job here. Failure messages say
  which direction the drift goes and whether it is compatible:

  ```
  tool "jade.grep" is served but not in the frozen contract
  tool "jade.grep_v2" is in the frozen contract but no longer served
  ```

  **Verified the guard actually fires** by renaming a frozen tool and
  watching it fail on both sides, then restoring. A guard nobody has seen
  fail is not yet a guard.

  Two more contract tests: every tool has a non-empty description (a tool
  the model cannot tell apart from the others loses to bash), and every
  required argument is also declared as a property (otherwise a strict
  client rejects the call before sending it and the schema documents
  nothing).

  **`docs/tool-contract.md`** covers why the surface is frozen at all (the
  MCP catalog is fixed at connection time, so renaming a tool *is* a
  deletion from an old client's view), a compatible/breaking table, the
  full surface, and — as importantly — what is **not** frozen: response
  wording, `.jade/*` file formats, symbol-ID spelling, anything under
  `internal/`. Responses are text for a model to read, not a format to
  parse; `JADE_JSON=1` exists for callers that need parsing.

  Also records the deliberate asymmetries so they are not "fixed" later by
  someone reading them as bugs: `read_symbol`/`delete_symbol` taking either
  `symbolName` or `symbolId` (JSON Schema cannot express "one of"),
  `create_file`/`replace_file` refusing each other's case without a force
  flag, and `search`/`grep`/`find` overlapping on purpose.

  **Unit tests: pass** — tests, vet, fmt-check and docs-check all green
  through jade's own tools. 3 new contract tests.
  Freeze tool names and the plain-text response format *after* Phase 10, and
  note that the MCP catalog is fixed at connection time so tool additions
  need a client reconnect. Changing the wire format after external
  integration is far more expensive than doing Phase 10 first — which is the
  whole reason Phase 10 is ordered ahead of the release.

- [x] **11.6 Integrate into droneship-next and run one real task**
  Picked up: 2026-09-12 — ran against `~/Projects/droneship-next` (real Go
  repo, 30 Go files) via `bin/jade-mcp --root`, read-only. **Nothing in
  droneship-next was modified** — editing another project was not in scope
  for a verification task.

  **Task:** "Is the CAS store's write atomic?" A genuine comprehension
  question: locate the store type, read its write path, check for
  temp-file-then-rename.

  **Answer, obtained:** yes — `Store.Put` uses `os.CreateTemp`, `Sync()`,
  then `os.Rename`. Both arms reached it.

  **Result: jade lost.**

  | | calls | bytes | est. tokens |
  |---|---|---|---|
  | jade | 3 | 2585 | ~646 |
  | shell (`grep -rn` + `grep -A 60`) | 2 | 1904 | ~476 |

  **1.36x the tokens and one extra round trip.** Recording this plainly
  because the 8.3 benchmark's 0.85x is the number that gets quoted, and it
  was measured on seven scenarios in jade's *own* repo. One scenario in a
  foreign repo went the other way. Both numbers are real; neither is the
  whole picture.

  **Why jade lost, and why the reason is not entirely bad.**
  `read_symbol(symbolName: "Put")` returned
  `ambiguous: Put matches …::Put@48, …::Put@72` and cost a third call to
  disambiguate. The ambiguity is real: `S3CompatibleStore.Put` at line 48
  and `Store.Put` at line 72. The shell arm only avoided it by my having
  typed the receiver into the pattern — `grep -A 60 "func (s \*Store) Put"`.
  An agent grepping the likelier `func.*Put` would have got both and could
  have read the wrong one without ever knowing there was a choice.

  So jade paid a round trip for a correctness property the shell arm did
  not have. That is a defensible trade, but it is *also* an avoidable cost,
  which is the actionable finding — filed as **11.9**.

  **Second finding: my first jade attempt was worse than my second.** I
  reached for `grep` with `context:18` across five matches before switching
  to `find` + `read_symbol`. The tool that fits is not the one that comes to
  hand first, even for a caller who wrote the tools. That is an argument
  about tool descriptions, not capabilities.

  **Unit tests: pass** — unchanged this task; no jade source was modified.
  Verified via `check(tests)`, `run_command(vet)`, `run_command(fmt-check)`.

  **Verified live: yes — this is the first time jade has done real work on
  a repository that is not itself.** `--root` from 11.2 and the binary from
  11.1 both held up; the Go grammar, `find`, `grep` and `read_symbol` all
  behaved correctly against unfamiliar code.

- [x] **11.9 Ambiguous symbol resolution costs a round trip it should not**
  Picked up: 2026-09-12 — candidates now carry their declaration line.

  `SymbolResolution` gains `Candidates []SymbolCandidate` (ID, kind, path,
  line, **signature**) alongside the existing `CandidateIDs`, which is kept
  because it is the machine-usable form and a caller already parsing it
  should not have to change. The signature is read off disk rather than
  reconstructed from the symbol record, because the record has no receiver
  and the receiver is usually the entire distinction. A failed read degrades
  to no signature rather than failing the response.

  Before and after, against the real droneship-next file that caused this:

  ```
  ambiguous: Put matches internal/cas/store.go::Put@48, internal/cas/store.go::Put@72
  ```
  ```
  ambiguous: Put matches 2 declarations
    internal/cas/store.go::Put@48  func (s *S3CompatibleStore) Put(ctx …) (Object, error) {
    internal/cas/store.go::Put@72  func (s *Store) Put(ctx …) (Object, error) {
  ```

  **Re-measured, and only half the claim holds.** The round trip is gone —
  3 calls down to 2, which was the actual defect. Tokens moved from **1.36x
  to 1.31x**, which is nothing.

  Recording that plainly because it corrects my own diagnosis in 11.6: I
  attributed the token loss to the ambiguity round trip, and it was not.
  The cost is dominated by `read_symbol` returning a 60-line function body
  — which the shell arm's `grep -A 60` returned too — plus `find Store`
  answering with two Store types when only one was wanted. The ambiguity
  fix was worth making on turns alone, but it was never going to move the
  token number, and saying otherwise would have been convenient rather than
  true.

  **Unit tests: pass** — `release-gate` green (build, vet, test, gofmt).
  4 new: renderer showing both receivers while keeping the IDs, fallback to
  bare IDs for transports that only populate them, no dangling separator
  when a signature is missing, and **one end-to-end through the real index**
  with two same-named methods, since the signature has to be read from a
  real file for the feature to mean anything.

  **Verified live: yes, via the binary against droneship-next** — the
  output above is the real response, not a constructed one. The session's
  MCP server still predates it.

  Left alone: `internal/transport/mcp`'s parallel copy, which only
  `cmd/jade` (the demo CLI) uses. It still emits bare IDs and the renderer
  falls back cleanly for it.
  Found in 11.6, and the direct cause of jade losing a head-to-head against
  grep on a real repo.

  `read_symbol(symbolName: X)` with two matches returns only the IDs:
  `ambiguous: Put matches internal/cas/store.go::Put@48,
  internal/cas/store.go::Put@72`. The caller cannot tell which they want
  — both are in the same file, and the line numbers mean nothing without
  reading them — so a third call is forced.

  Cheapest fix: give each candidate a one-line disambiguator, which jade
  already knows from the parse — the receiver and signature:

  ```
  ambiguous: Put
    internal/cas/store.go:48  func (s *S3CompatibleStore) Put(…)
    internal/cas/store.go:72  func (s *Store) Put(…)
  ```

  That usually ends the interaction at call two, since the receiver is
  exactly what the caller was distinguishing on. Stronger option: when the
  candidate count is small and bodies are short, return them all — but
  measure first, because that trades tokens for turns and 13.8 showed jade's
  worst instinct is spending bytes nobody asked for.

  Applies to `find` and `delete_symbol` too, which share the resolver.
  The first genuine outside use. Success criterion is not "it worked" but a
  written comparison against the shell-and-file baseline for the same task:
  tokens, turns, and whether anything had to be redone.

- [x] **11.7 Stand up a feedback loop**
  Picked up: 2026-09-12 — three issue templates, `docs/reporting.md`, and a
  `templates-check` command.

  **The task said `FEEDBACK.md`; writing it would have destroyed
  `feedback.md`.** This repo's filesystem is case-insensitive (the macOS
  default), so the two are the same path — confirmed by `ls FEEDBACK.md`
  resolving to the existing internal log. Named it `docs/reporting.md`
  instead, with a note at the bottom explaining why so nobody "fixes" the
  name later.

  **Templates: bug, friction, feature.**

  *friction* is the one that matters and is framed as such: "I reached for
  the shell instead". It explicitly tells reporters that **"it was just
  habit" is a valid and wanted answer** and asks them not to self-filter —
  because habit means the jade path was not the obvious one at the moment of
  choosing, which is a design problem. That is the single clearest finding
  from this project's own log and it would be lost if reporters only filed
  things they could justify.

  *bug* opens with a **required reconnect checkbox**. The MCP catalog is
  fixed at connection time, and during jade's own development this accounted
  for every single "the tool is missing" report without exception — so it is
  the cheapest possible filter. It also asks for the file's language, since
  the text-scan fallback for non-Go/TS/Rust is a known limitation rather
  than a bug, and for `gopls` presence on anything touching
  `references`/`rename`.

  *feature* asks for the **task, not the feature** — "I wanted to find every
  caller of a struct field before deleting it" tells us whether an existing
  tool should have covered it, where "add a field-usage tool" does not. It
  also redirects known limitations toward a friction report, since "this bit
  me in real work" is priority information and a feature request is not.

  **Telemetry is what makes these reports cheap to act on**, and both bug
  and reporting docs say why it is safe to paste: it records no arguments,
  no response bodies and no error text, only tool names, timings, sizes and
  failure classes. That content-free design (13.2) was chosen precisely so
  the log could be attached to an issue from a private repo. The
  `fallback risk:` line is the direct measurement of this project's whole
  subject.

  `docs/reporting.md` also states what *not* to bother with: exact response
  wording (not frozen), known limitations unless they bit you, and polishing
  — "a one-line 'this made me use grep and I don't know why' is worth more
  than a well-written issue that never gets filed."

  **Unit tests: pass** — tests, vet, docs-check all green. No Go changes.
  New `templates-check` declared command validates every issue template
  parses as YAML; a template that does not parse is silently ignored by
  GitHub, which would mean the whole feedback loop quietly not existing.

  **Verified live: yes** — `jade_run_command(templates-check)` returns
  `pass templates-check / all issue templates parse` across all four files.
  Issue templates for bug / UX friction / feature request, a short
  `FEEDBACK.md` on what makes a useful jade report (which tool, what was
  called, what came back, what was expected), and a place to collect them.
  Without this, (c) and (d) do not happen.

- [x] **11.8 Tag 0.0.1 and write release notes**
  **Done: 2026-09-12 — tagged `v0.0.1`.** Owner authorized the commit after
  the live re-verification below. All 91 files went in as one commit,
  `6316c30`, since the tree is a single coherent state and splitting it
  after the fact would have invented a history that never happened.
  `git tag -a v0.0.1`; `make binary && ./bin/jade-mcp --version` reports
  `jade-mcp v0.0.1`, so ldflags stamping works off a real tag, not just the
  throwaway one used earlier. Tag is local — not pushed.

  Unit tests: `go build && go vet && go test ./...` all green.

  Verified live over MCP: **yes** — this was the first wake after a
  reconnect, so 11.10's fix was finally testable through the running server
  rather than the binary. Declared a trap command through
  `jade_declare_command` (`echo "everything looks fine"; exit 3`), ran it
  through `jade_run_command`, got `FAIL exit-status-probe … exit status 3`.
  Before 11.10 that exact command returned `pass`. Removed the probe, then
  ran `release-gate` live: `pass · 11 packages ok, 12 with no test files`.

  The note below is the pre-tag history and is kept as written.

  ---

  Picked up: 2026-09-12 — **release notes written; tag Blocked.**

  **Done:** `CHANGELOG.md` with the 0.0.1 entry — what jade is, the 34
  tools, what is actually verified live, the benchmark numbers *with their
  disagreement* (0.85x across seven scenarios at home, 1.36x on the one
  scenario run away from home), the known limitations stated plainly, the
  reconnect gotcha, install, and where to send feedback.

  Verified version stamping end to end by creating a throwaway tag and
  rebuilding: `jade-mcp v0.0.1-probe-dirty`. Tag deleted afterwards. On a
  clean tree a real tag yields `jade-mcp v0.0.1`.

  Added a `release-gate` declared command (build, vet, test, gofmt) so the
  pre-tag checks are one call rather than four remembered ones.

  **Update 2026-09-12 (later): the code blocker is cleared.** 11.10 fixed
  the exit-status defect and 11.9 removed the ambiguity round trip;
  `release-gate` reports `release gate: all green`. Nothing in jade now
  stands between here and a tag.

  **Still not done, and not mine to do.** Tagging requires committing 91
  files — this entire project — and how that history should be shaped is the
  repository owner's decision, not a detail to be settled by whoever happens
  to be holding the keyboard. One release commit, a handful of phase
  commits, or something else are all defensible; picking silently is not.
  Asked across two wakes; the loop fires on a timer and does not answer, so
  this waits for a person.

  When that decision comes, the remaining steps are exactly:

  ```bash
  # reconnect the MCP client first, so 11.9 and 11.10 are verified live
  # rather than only through the binary
  git add -A && git commit -m "<your message>"
  git tag -a v0.0.1 -m "jade 0.0.1"
  make binary && ./bin/jade-mcp --version   # expect: jade-mcp v0.0.1
  ```

  The original blocker, kept for the record:

  **Blocked: `run_command` reports a failing command as `pass`.** Found by
  the release gate itself — it printed `pass release-gate` and
  `exit status 1` in the same response. Confirmed with a deliberate probe:

  ```
  declare probe-fail = echo "everything looks fine"; exit 3
  run_command probe-fail  →  pass probe-fail / everything looks fine / exit status 3
  ```

  `checkPassed` decides the verdict by scanning output text for markers
  (`fail`, `error`, `panic:`, …) and never consults the process exit
  status. For Go's build/vet/test that mostly works, because they print
  those words when they fail. For the **arbitrary project commands 12.11
  deliberately opened up** — lint, codegen, migrate, a deploy script — it is
  simply wrong: a command that exits non-zero while printing something
  cheerful is reported as green.

  **Not tagging 0.0.1 with this in.** A validation tool that reports false
  green is the worst possible defect to ship, and it undermines the release
  gate meant to guard the tag. Filed as 11.10; tag after it is fixed.

  Everything else for the tag is ready: notes written, gate defined, version
  stamping verified, working tree still uncommitted (90 files) pending a
  decision on commit granularity — **that part is yours to call, not mine.**

- [x] **11.10 `run_command` and `check` ignore process exit status** — release blocker
  Picked up: 2026-09-12 — exit status is now authoritative.

  Taken ahead of 11.9 despite coming later in the file: 11.9 is an
  efficiency improvement, this is a validation tool reporting false green,
  and it blocks 11.8.

  **The fix.** `Runner` now records whether a job's process exited non-zero
  (`failed map[string]bool`, surfaced as `JobOutput.Failed`). The executor
  already had the answer — `cmd.Wait()`'s error — and was *appending its
  text to the output* (`exit status 3`) and then never looking for it, so
  the status was captured and immediately discarded. New
  `CompleteWithResult(id, output, failed)`; `CompleteWithOutput` delegates
  with `false` for the callers that never ran a process.

  `checkPassed` became `jobPassed(output)`:

  - **Non-zero exit fails, full stop.** No wording can override it.
  - The text scan survives as a **secondary** signal, because some tools
    genuinely exit 0 while reporting a failure. It can fail a job, never
    pass one.

  Also fixed the start-failure and timeout paths, which reported a command
  that could not launch, or was SIGKILLed, as a pass if its output happened
  to look innocuous.

  **Then the fix exposed that the release gate itself was broken.** With
  exit status authoritative, `release-gate` started failing — and it turned
  out its `gofmt -l … | (! grep .)` clause aborts the whole script under
  `set -e`, so the gate had been exiting 1 all along while reporting `pass`.
  The false green had been hiding a genuinely broken gate. Rewritten as
  `test -z "$(gofmt -l …)"`, now exits 0 and prints `release gate: all
  green`.

  That is the clearest possible demonstration of why this mattered: the one
  command whose entire job is to gate the release was itself broken, and the
  bug made it look fine.

  **Unit tests: pass.** 5 new. Two at the verdict level (non-zero exit beats
  cheerful output; failure wording still counts on a clean exit) and
  **three end-to-end through the real runner** — `echo 'everything looks
  fine'; exit 3` must fail, `echo 'done'; exit 0` must pass, and
  `echo 'FAIL: …'; exit 0` must fail. The end-to-end ones matter more: the
  bug was that the exit status never reached the verdict, so a test handing
  the verdict an exit status could not have caught it.

  **Verified live over MCP: no — needs reconnect.** The running server
  predates the fix and still answers `pass release-gate` alongside
  `exit status 1`, which is the bug itself, captured one last time.
  Observable after reconnect: that same call reports the gate's real
  verdict.

  **11.8 is unblocked once this is live.**
  A command exiting non-zero is reported as `pass` whenever its output
  happens not to contain "fail", "error", "panic:" or the other markers
  `checkPassed` scans for. Demonstrated with `echo ok; exit 3`.

  The runner already knows the truth: `RunCommandWithTimeout` receives the
  error from `cmd.Wait()` and currently *appends its text to the output*
  (`exit status 3`) rather than recording it. So the fix is to carry the
  exit status through `JobOutput` and let it decide the verdict, with the
  text scan kept only as a secondary signal for tools that exit 0 while
  failing (some linters and test runners genuinely do).

  Order matters: **non-zero exit means fail, full stop.** A zero exit with
  failure markers in the output should still read as fail. Anything else
  keeps the current false-green.

  Applies to `check` and `run_tests` as well — they share `checkPassed` —
  though the Go toolchain masks it there. Needs a test per path using a
  command that exits non-zero while printing something innocuous, since that
  is precisely the case the current implementation gets wrong.
  Version the binary, tag the repo, and write notes that state the scope
  honestly: primitives are real and verified live, the call graph is
  approximate where gopls is unavailable, and no benchmark has yet
  established jade beats the baseline. Then build the post-release roadmap
  from what actually comes back, not from this file.

---

## Definition of "borderline useful"

Jade crosses from "convincing demo" to "an agent can actually use this
without losing work or being lied to" when Phase 1 and Phase 2 are both
done and verified end-to-end:

1. An edit made through `replace_symbol`/`replace_range` is actually on disk.
2. A stale edit is rejected, not silently applied.
3. A background validation job actually runs `go build`/`go test` and
   reaches "completed" with a real pass/fail summary.
4. `changes()` matches `git status` reality regardless of which tool made
   the edit.

Everything in Phase 3-5 is real value on top of that baseline, not a
prerequisite for it.

## Definition of "actually validated" (Phases 6-9)

Phases 1-5 made jade's primitives real and proved them live one at a time.
None of that proves jade is *worth using* in scope.md's own terms (§26,
§42-43: tokens/turns/success rate versus shell-and-file baseline) — nothing
has measured that yet, on this repo or any other. **Phase 8.3 (the
benchmark harness) is the single highest-priority item across Phases 6-9**,
ahead of every North Star feature in Phase 7 and 9: it answers whether the
rest of this backlog is worth continuing at all, which is the question
scope.md says should have gated this work from the start.
