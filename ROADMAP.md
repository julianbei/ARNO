# Roadmap

What is planned, and why each item is on the list. Items come from real use —
agents running Jade over MCP in other repositories — and from the conformance
suite. Each one names the observed problem first, because the fix is only
right if it removes that problem.

Nothing here is a promise of a date. Order within a section is priority order.

---

## Release plan

### 0.0.3 — good tenant, cheaper calls, honest checks

Theme: nothing Jade writes or says should be something the caller did not ask
for or cannot act on. Worked in this order; each item links to its section.

1. Accept both tool-name spellings (§1)
2. Keep `.jade/` state out of the workspace (§1)
3. Drop header lines a caller cannot act on (§2)
4. Name the core tools in the server instructions, make `apply` discoverable (§2)
5. Name the checker in every edit response (§3)
6. Language-server diagnostics after edits for every configured server (§3)
7. JSON and YAML syntax checks after edit (§3)
8. Multi-query `find`, multi-range `read_range`, and `read_range` clamping an
   end line past the end of the file (§2)
9. `check` says what will run before running it (§4)
10. Document and anchor revisions to git (§5)

Release gate: every item above checked, `make conformance` green, README and
CHANGELOG updated, release notes in
[docs/release-notes/v0.0.3.md](docs/release-notes/v0.0.3.md) finished (draft
status section removed), tag `v0.0.3`. Each item adds itself to the release
notes when it lands.

### 0.0.4 through 0.1.0

Phased in [docs/release-plan-0.1.0.md](docs/release-plan-0.1.0.md): evidence
(0.0.4), tool surface (0.0.5), environment predictability (0.0.6), the change
transaction (0.0.7), then 0.1.0. Each phase has an exit gate.

---

## Strategy and evidence

From an external review (2026-09-13) of Jade and the surrounding ecosystem.
Its central point: symbol-aware code access over MCP is becoming table stakes
— several projects already offer it — so Jade's defensible ground is the
**whole change transaction**: read → edit against a known revision →
validate with the repository's own commands → report consequences → revert.
The question every item below serves is the one the review ends on:

> Does giving an agent Jade make it measurably better than giving it a shell?

Competitor details below are the reviewer's claims, not verified here.

### Evidence before features

- [ ] **External benchmark, with bash as the baseline.** The current benchmark
  runs against Jade's own repository, which is necessary but not sufficient.
  Measure task success, tokens, tool calls, wall time, invalid edits, shell
  fallbacks and regressions across real repositories in several languages,
  comparing *agent + shell*, *agent + Jade*, and *agent + Jade + shell*. A
  bash-only agent (mini-SWE-agent's thesis) is the adversary that matters
  most. SWE-agent's configurable tool bundles are a candidate harness.
- [ ] **Measure tool confusion.** 35 tools is a large routing surface: `find`,
  `search`, `grep`, `retrieve`, `context`, `repository_map`, `read_symbol`,
  `read_range` and `outline` all answer overlapping questions. Use telemetry
  to find tools agents pick wrongly, retry, or never use — then merge or
  delete them.

### Surface and cost

- [ ] **Tool profiles.** A small core profile (on the order of inspect, edit,
  validate, state) alongside the full catalog, and a way to leave out what a
  host already does well. Avoid collapsing into one polymorphic tool with a
  huge `action` schema — that moves the routing problem into arguments.
- [ ] **Response budgets with continuation.** One convention across tools —
  `budget` in tokens, and a continuation handle when the answer does not fit
  (`24 of 87 references shown · continue=…`) — instead of per-tool limit
  knobs. Makes response cost an explicit part of the API.

### Trustworthy answers

- [ ] **Provenance on every answer.** Jade already distinguishes exact from
  approximate in `references`, and now names the checker on edits. Make it
  uniform and compact: `exact · gopls`, `approximate · text index`,
  `structural · tree-sitter`, `no grammar · text fallback`. The rule stays:
  exact when exact, approximate when useful, refuse when approximation is
  unsafe.
- [ ] **Impact-aware validation.** After an edit, use references to name the
  affected callers, packages and likely tests, and validate those first
  rather than the whole repository. Directly strengthens the transaction, and
  replaces the unread whole-repository typecheck every edit starts today.

### Investigate

- [ ] **SCIP** as a third semantic backend next to tree-sitter and LSP:
  indexed, exact, cheap for large repositories and historical revisions.
  Operations should name capabilities, not backends, so this stays invisible
  to agents except in provenance.
- [ ] **ast-grep** for structural search and rewrite (codemods) as an
  agent-safe operation, complementing symbol replacement.
- [ ] **Explainable git signals** (co-change, churn) in `repository_map` and
  `retrieve` ranking — only if every ranked result can say why it ranked.
- [ ] **Integrations** worth testing the core hypothesis in: OpenCode, goose,
  Cline, Codex CLI, Gemini CLI.

### Non-goals

Recorded so they stay decisions rather than drift:

- Jade is not an agent: no planning, memory, conversation state, subagents or
  prompt workflows. The agent decides *what*; Jade guarantees *how*.
- Jade is not a general code-knowledge platform. Retrieval exists to serve the
  next correct edit.
- No model or embedding in the deterministic path. Similarity may nominate;
  parsers, compilers, LSP and git decide.
- Validation stays inside Jade through declared repository commands rather
  than being handed back to the shell — that is what makes the whole
  transaction observable.
- Not a sandbox runtime. Run Jade inside one instead.

---

## 1. Be a good tenant in someone else's repository

Jade is increasingly run against checkouts it does not own: a worker's git
worktree, a container mount, a CI clone. In those, every file Jade writes
that the user did not ask for becomes part of the change.

- [x] **Keep `.jade/` state out of the workspace.**
  `telemetry.jsonl` is written to `<root>/.jade/` on the first tool call —
  including a call Jade rejects as unknown. A harness that commits every
  untracked file then commits Jade's bookkeeping as part of the agent's work,
  and one that fails review on a changed worktree fails.
  - Add `JADE_STATE_DIR` to put telemetry outside the workspace.
  - When state must live in the workspace, add `.jade/telemetry.jsonl` to
    `.git/info/exclude` (local, never committed) before first write.
  - Write nothing for a call that fails validation. *(unknown tool names: done
    with item 1; argument-validation failures still to check)*
  - `.jade/commands.json` stays in the repo — it is meant to be committed —
    but is only created when a command is declared.
  - Done 2026-09-13: `JADE_STATE_DIR` moves the log to a per-workspace
    subdirectory (base name + path hash). In a git repo the log is added to
    `.git/info/exclude` before its first write, prefixed correctly for a
    workspace below the repo root, skipped when git already ignores it;
    `.gitignore` is never touched. Verified live with raw sessions against
    fresh repos in both modes: `git status` shows only the user's file.
    Arguments rejected by the dispatch check write nothing; handler-level
    errors are still recorded, into a log git now ignores.
- [x] **Accept both tool-name spellings.** The wire name is `jade.find`; hosts
  display `jade_find` (Claude Code shows `mcp__jade__jade_find`), and a raw
  `tools/call` for `jade_find` currently fails as unknown. Accept the
  underscore form as an alias, and document the wire names.
  - Done 2026-09-13: `jade_<name>` resolves to `jade.<name>` at the dispatch
    chokepoint, telemetry records the canonical name, unknown names get near
    matches. Also: a rejected name no longer creates `.jade/` — it is recorded
    only into a log that already exists. Verified with a raw session against a
    fresh git repo: `git status` clean after a rejected call.

## 2. Spend fewer tokens and turns per call

- [x] **Drop header lines a caller cannot act on.**
  - `drifted: N files` on every read. It is noise for a read, and its meaning
    (files changed outside Jade — by a build, git, another process) is never
    explained. Show it on edits, where it is a real precondition, with a
    one-word hint of what drift means; omit it from reads.
  - `jobs: job-N` on every edit. The background validation result is only
    ever seen when it fails, and a failure is already reported inline. Fold
    a finished result into the response, omit the line otherwise, and name
    `job_status` when a job is still running.
  - Done 2026-09-13: reads no longer print a drift count (only a broken
    "freshness unknown" state is shown); edit responses no longer print job
    IDs in text output (still present with `JADE_JSON=1`). Verified live: a
    read in a repo with two files changed outside Jade renders as `r1` plus
    content; an edit renders as `r1 → r2 · a.go · +1 -1`.
  - Follow-up for 0.0.4: every single edit still starts a whole-repository
    typecheck job whose result nobody reads. Either scope it to the edited
    package and fold the verdict in, or stop starting it outside `apply`.
- [x] **Name the core tools in the server instructions.** Hosts load MCP tool
  schemas lazily, so an agent's first call to any Jade tool first costs a
  schema-search turn. A worker with a four-turn budget lost one of them this
  way. The instructions should name the handful to load first: `find`,
  `read_range`, `replace_text`, `insert`, `apply`, `outline`, `check`.
  - Done 2026-09-13 (with the `apply` item below): instructions name seven
    tools to load first, say when to batch, and state both name spellings,
    in 495 bytes. A test fails if a named tool leaves the catalog or the text
    passes 600 bytes. Also fixed: `serverInfo.version` was hardcoded `0.1.0`;
    it now reports the binary's real version. Verified live with a raw
    `initialize` against a stamped build.
- [x] **Make `apply` discoverable.** Atomic multi-file batches of
  `replace_text` and `insert` already exist in `apply`, with one validation
  pass at the end. An agent that made twelve separate doc edits in one session
  did not know. Mention it in the `replace_text` and `insert` descriptions and
  in the server instructions.
  - Done 2026-09-13: `replace_text` and `insert` descriptions point at `apply`
    for multi-site work; the server instructions name it; a test keeps both.
- [ ] **Multi-query `find`** — `find(names: ["Project", "AttentionItem"])` in
  one call instead of three. Seen again in a later session: two or three
  types were needed at once almost every time, sent as parallel calls.
- [ ] **Multi-range `read_range`** — several files and ranges in one read.
- [ ] **Clamp `read_range` to the end of the file.** An end line past EOF is
  rejected — `range 190-360 is invalid for refs.go (312 lines)` — and cost a
  whole extra turn four times in one session. "From here to the end" is the
  usual intent: return lines 190-312 and say `lines 190-312 of 312`.

## 3. Diagnostics everywhere, and say which checker ran

Diagnostics in the edit response are the feature agents cite as the reason to
use Jade over the host's own edit tool: `undefined: strings` returned with the
edit, fixed on the next call, no build turn. They should hold in every
language.

- [x] **Name the checker in every edit response** — `checked: gopls`,
  `checked: tsc`, or `not checked: no checker for .tsx`. Thirteen new
  TypeScript files produced no diagnostics and no way to tell "nothing wrong"
  from "nothing checked".
  - Done 2026-09-13 (with the item below): every edit and `apply` response
    names its checker, or gives each unchecked file a `not checked: <reason>`
    line. Files with no language stay silent. Checks are memoised per file on
    mtime and size, so diagnostics and the report cost one check.
- [x] **Language-server diagnostics after edits for every configured
  server**, not only Go, with the same wait-for-publish discipline.
  - Done 2026-09-13: verified in the conformance container — a broken edit
    returns its real error in all eight languages. Four server behaviours had
    to be handled, each first seen as a broken file reported clean: stale
    publishes (wait for a newer one, then for quiet); ruby-lsp's pull model and
    its parse-before-apply race (flush with `documentSymbol` before pulling);
    ruby-lsp's incremental sync needing a ranged change; metals publishing
    empty before compiling (only post-compile diagnostics count). metals also
    asks to import the build; jade declines unless `JADE_METALS_IMPORT=1`,
    because importing runs sbt and writes `.bloop/` and `.metals/`.
    Closes the reported gap of ~15 TypeScript/TSX edits returning nothing,
    leaving all checking to `tsc` in the build.
- [x] **JSON and YAML syntax checks** after edit — cheap, built in, no server.
  - Done 2026-09-13: edits to `.json`, `.yaml` and `.yml` report `checked:
    json` / `checked: yaml` and the first syntax error with its location.
    Every JSON value and every YAML document in the file is parsed, so an error
    after a valid first document is still found. JSON-with-comments files
    (`tsconfig.json`, `jsconfig.json`, devcontainer and VS Code settings) are
    not reported as broken. Verified live: a stray comma in `package.json`
    returned `error package.json:2:19 invalid JSON: …`.
- [ ] **Stop reporting transient errors from half-done multi-step edits.**
  Imports added before the code that uses them reported
  `"context" imported and not used`; the next call fixed it. `apply` avoids
  this; the response could also point at it when an error names a symbol the
  same edit just introduced or removed. Seen again: three edits calling a
  helper added by a later edit in the same message each reported
  `undefined: nextAttemptNumber`.

## 4. Make `check` and `run_tests` usable for real builds

Agents bypassed `check` for four reasons, each fixable:

- [ ] **Say what will run before running it.** A Go module with three Node
  packages and no Makefile gave no way to predict what `check kind=build`
  would execute, and a green result on the wrong target is worse than none.
  Add a dry-run that lists the discovered command per target, and report the
  command and directory in every result.
- [ ] **Multi-project repositories.** Discover each package (Go module, every
  `package.json`, …) and let `check` take a target, instead of picking one.
- [ ] **Long-running work without polling.** `check` caps its wait at 300
  seconds, and a Docker build takes longer. Jade's job IDs then cost a polling
  turn each. Send MCP progress notifications while a job runs so hosts that
  support them can wake the agent on completion, and raise or remove the cap
  for jobs that are already backgrounded.

## 5. Explain how revisions relate to git

- [ ] **Document and anchor revisions to git.** Jade's `r1…r53` ran alongside
  three commits made from the shell, and the agent avoided `revert` and
  `checkpoint` entirely because it could not tell what a revert does to
  already-committed files or whether it crosses a commit. State it in the
  tool descriptions, record the git `HEAD` in each checkpoint, and make
  `revert` refuse — with the reason — when a commit has landed since the
  checkpoint rather than silently rewriting committed work.

## 6. Workspace scope

- [ ] **An optional scratch root.** Jade is fixed to one root, so an agent's
  temporary scripts were edited with `sed` instead, and a broken `pkill`
  pattern slipped through unchecked and corrupted a verification run.
  Support a second, explicitly configured root (for example
  `JADE_SCRATCH_ROOT`) for edits only — no indexing, no telemetry.

## 7. Language support

- [ ] **Formatting conformance against real formatters.** prettier, black,
  ruff and scalafmt selection is unit-tested with stand-in binaries; add them
  to the conformance image and prove a real project config is honoured.
- [ ] **Java formatting.** google-java-format has no project config file to
  detect, so there is no safe opt-in signal yet. Candidates: a Spotless or
  google-java-format entry in `pom.xml` / `build.gradle`.
- [ ] **Ruby formatting.** Deliberately absent: `rubocop -a` applies lint fixes
  that change behaviour. `rubocop --fix-layout` with a `.rubocop.yml` present
  is the candidate, if layout-only can be proven.
- [ ] **ruby-lsp method rename.** ruby-lsp 0.26 renames classes and modules
  but returns null for methods. Jade falls back to `solargraph` when it is
  installed; without it, method rename refuses. Revisit when ruby-lsp adds it.

---

## 8. First-hand use

Found while building 0.0.3 with Jade itself rather than the host's own tools.

- [ ] **`replace_text` line counts misdescribe additions.** Appending 14 lines
  after a 4-line anchor reported `+18 -4`: the anchor is counted as removed and
  re-added. A reader sees a rewrite where there was a pure addition. Report the
  diff of the change, not the size of the two texts.
- [ ] **`run_command` with no name prints every command's full script.** One
  declared command is a ~700-byte shell pipeline; the listing's job is to show
  what exists. Show name and description, and the script only for the command
  actually run.

Done items move to [CHANGELOG.md](CHANGELOG.md).
