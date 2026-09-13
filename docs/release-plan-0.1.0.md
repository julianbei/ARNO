# Road to 0.1.0

What has to be true before Jade calls itself 0.1.0, and the phases that get it
there. Tasks link to [ROADMAP.md](../ROADMAP.md) where the item already lives;
this file orders them and says when each phase is done. Done items move to
[CHANGELOG.md](../CHANGELOG.md), as everywhere else.

## Where this comes from

An external review of 0.0.2 (2026-09-13) described Jade back to us. Its
reading of the thesis matches ours: symbols, revisions, diagnostics and
repository commands instead of `grep`/`sed`/`cat`, and a product that only
wins if it is **cheaper for the model than bash**. It named three strengths —
symbol-oriented edits, revisions with edit consequences, and
`.jade/commands.json` as a repository vocabulary — and three cautions:

1. **"Treat it as an experiment rather than infrastructure."** Fair at 0.0.x.
   0.1.0 is the version where that stops being the right advice.
2. **Structured API versus arbitrary shell.** The shell composes without
   limit; matching enough of it without exploding the MCP surface is the hard
   product problem. 35 tools already overlap.
3. **Capabilities vary by environment.** The strongest features depend on git,
   language servers and toolchains, and Jade degrades when they are missing.
   Sensible — but an agent cannot tell in advance what it will get.

The review also restates the number that matters: **0.85x at home, 1.36x
away.** One outside scenario is not evidence either way.

A second review scored 0.0.2 at 7.5/10 overall: idea 9, design 8.5,
documentation 9, agent ergonomics 8 — and **reliability 5, adoption readiness
5**. The two lowest scores are what 0.1.0 has to move. It sharpened the
cautions above:

- **"Architecturally correct but economically unnecessary"** is the largest
  risk. Two shell calls beat six structured ones however elegant they are.
- **Scale of evidence:** 20–50 outside repositories across Go, Python,
  TypeScript, Java, Rust and mixed monorepos — and agent + Jade + shell
  fallback "may actually be the practical winner".
- **Consolidate by data, delete aggressively.** The right surface may be
  10–15 operations, not 35.
- **Abstraction leakage.** When `jdtls` takes minutes to start or pyright
  disagrees with the project's config, the user's verdict is "Jade doesn't
  work". Managing dependencies may cost more than the MCP layer.
- **Real users** are the next signal, not more features: people installing
  Jade in unrelated projects and keeping it enabled.

A third review surveyed the category. Competitor details and star counts are
the reviewer's claims, not verified here. Its conclusion changes the
positioning, not the architecture:

- **Symbol-aware code access over MCP is table stakes.** Serena (LSP-backed
  retrieval, editing and refactoring; by far the largest) overlaps almost
  entirely with Jade's inspect layer. SymForge is technically closest: symbol
  edits, impact analysis, checkpoints, trust labels on answers, and a
  three-tool compact mode that claims to cut schema size from ~85 KB to
  ~4.8 KB. code-atlas already does affected-test detection and token-capped
  responses. treesitter-mcp makes Jade's own "fewer tokens than grep" argument.
  **"Cheaper than cat" is not a unique claim.**
- **Jade's ground is the transaction:** edit against revision N, apply
  atomically, report what structurally changed, validate with the repository's
  own commands, diff, revert, record how the agent got there. Describe Jade as
  a transactional development environment for agents, not a code-intelligence
  server.
- **Hosts, in priority order:** OpenCode (large, MCP-native, and its docs warn
  that big MCP surfaces eat context — so it needs a compact profile), goose
  (MCP is its extension model, and its ACP passthrough reaches Claude Code and
  Codex), Cline (built-in search/read/patch/bash make a clean before-and-after),
  SWE-agent (tool bundles make it the evidence harness), then Codex CLI and
  Gemini CLI.
- **Benchmark against competitors as well as bash:** Serena, SymForge,
  code-atlas, codescout, treesitter-mcp — and mini-SWE-agent, bash only by
  design, as the adversary that matters most.
- **Build on, do not rebuild:** SCIP and ast-grep later; Semgrep through
  declared commands; SWE-ReX as the sandbox Jade runs inside; Joern never in
  core.

A fourth review said what to take from competitors, under one rule: **copy
mechanisms, not product identity.**

- **Take now:** answer provenance with completeness (SymForge), compact and
  host-specific surfaces (SymForge, Serena), token budgets with continuation
  (code-atlas). **Soon:** impact-driven validation (code-atlas), capability
  profiles (Serena). **When scale needs it:** a shared warm backend
  (codescout). **Carefully:** awareness of docs, specs and ADRs.
  **Experiment:** git ranking signals. **Low:** embeddings. **Avoid:**
  memories and orchestration.
- **Where Jade should disagree with them.** Not an agent (Serena is heading
  there). Not a code-knowledge platform ("Sourcegraph-lite over MCP"). And
  **validation stays inside Jade**: SymForge hands builds and tests back to
  the shell, which leaves Jade controlling half the transaction.
  `.jade/commands.json` should get stronger, not weaker.
- **Operations name capabilities, not backends.** `references` may answer from
  LSP, SCIP or a text index; only provenance says which.
- **Its build order:** provenance, profiles, budgets, impact-aware validation,
  then a stronger benchmark. This plan keeps a benchmark pilot first — profile
  shape and budget defaults are the decisions it exists to settle — but moves
  provenance forward to sit with budgets in Phase 3.

A fifth review proposed a plugin architecture under one rule: **core owns
state and effects; plugins provide capabilities and evidence.** Plugins parse,
analyse, rank, propose edits, return diagnostics and describe validation
commands; only core writes to the workspace or runs a process. Its shape:
subprocess plugins over stdio JSON-RPC (not Go's `plugin` package), one
versioned interface per capability (`outline`, `references`, `rename`,
`diagnostics`, `format`, `validation`, `rank`), per-capability provider
selection by strength, authority classes fixed by the protocol rather than
claimed by plugins, explicit user-controlled installation, and **no
plugin-defined MCP tools** — plugins make existing tools smarter.

Checked against the code, 2026-09-13:

- **Right: core does not own all writes.** LSP rename writes files itself,
  one at a time, so a failure midway leaves a partial rename
  ([lsp_bridge.go:130](../internal/code/lsp_bridge.go#L130)). `os.WriteFile`
  is called from seven places in `internal/code`.
- **Right: ecosystem knowledge is hardcoded** — command discovery in
  [internal/jobs/discovery.go](../internal/jobs/discovery.go), and a fixed
  vocabulary map (auth, session, user, validate…) in retrieval at
  [index.go:936](../internal/code/index.go#L936).
- **Wrong: size.** The binary is 17 MB, not 180 MB. Distribution size is not
  a reason to split before 0.1.0.

This plan takes the boundary now and the ecosystem later: providers behind a
registry inside the one binary, and every write through one path, before
0.1.0; the subprocess protocol, installation and separate packages after.

A sixth review read this plan and rated its strategy about 9/10. What it
said was missing is integrity, not features. Checked against the code,
2026-09-13, every claim held:

- **No workspace containment.** `resolvePath` returns absolute paths as given
  and joins relative ones without checking for `..`
  ([index.go:1359](../internal/code/index.go#L1359)); no layer checks that a
  path stays inside the root. "Safe to leave running" cannot be claimed
  while an edit can write outside the workspace.
- **Revisions only see Jade's own edits.** `expectedRevision` is compared with
  an in-process counter ([apply.go:34](../internal/edit/apply.go#L34)). A file
  changed by the user, the host or another tool leaves the revision unchanged,
  so a stale edit passes its precondition.
- **Revert moves the revision backward** — it sets the counter to the
  checkpoint's ([manager.go:286](../internal/workspace/manager.go#L286)) — so
  two different states can both be called `r18`.
- **Revert restores contents only.** Files missing at checkpoint time are
  skipped, files created since are not deleted, and write errors are
  discarded ([manager.go:232](../internal/workspace/manager.go#L232),
  [295](../internal/workspace/manager.go#L295)).
- **Validation outcomes are untyped.** `check` already says `no command` rather
  than passing, which is right, but outcomes are free strings beside a
  `Passed` bool, and a waited check that times out still reads `running`.

It also asked for: capability registrations rather than eight Go interfaces;
Phase 5 ordered so the transaction is correct before impact-aware validation
is built on it; a benchmark scorecard in place of an all-metrics-at-once gate;
diff size as a measured outcome; and "no provider adds a tool" held as an
architectural invariant, not a roadmap choice.

## What 0.1.0 means

0.1.0 is a claim someone else can check:

- **Measured.** On at least 20 repositories Jade was not built in — Go,
  Python, TypeScript, Java, Rust and at least one mixed monorepo — agent +
  Jade (or agent + Jade + shell, whichever wins) passes the Phase 2
  scorecard against agent + shell: never less successful, never more invalid
  or unintended edits, and any extra tokens, calls or time within the
  trade-offs fixed before the run. The same
  tasks run against at least Serena, as a comparison rather than a gate.
  Published with the method, including the losses.
- **Predictable.** An agent can ask, in one call, what this workspace
  supports — which languages are exact, which are approximate, which checkers
  and commands exist — before it relies on any of it. When a dependency is
  slow, crashed or misconfigured, the response names that dependency and what
  it cost, not a generic failure.
- **Small enough to route.** A core profile an agent can learn in one read, and
  no two tools that answer the same question without saying which to prefer.
- **Safe to leave running.** No operation reads or writes outside the
  workspace. An edit is refused if its target changed since the read that
  informed it, whoever changed it. Revisions never move backward, and a revert
  restores which files exist as well as their contents. Revisions, checkpoints
  and `revert` have
  documented behaviour against git, and nothing Jade does rewrites committed
  work or dirties a worktree it was not asked to touch.
- **Stable where promised.** The tool contract says what 0.1.x will not break.
- **Used outside.** Setup is verified live in OpenCode, goose, Cline, Codex CLI
  and Gemini CLI, and people other than the author run it in unrelated
  projects, keep it enabled, and report what broke.

Anything that does not serve one of those six waits for 0.2.

## How a task earns its place

Every task below should pass the six principles from the fourth review. Most
are already in the README's design principles; the rest are added in Phase 2.

1. **Minimum sufficient context.** Do not hand the agent code it does not need.
2. **Deterministic evidence.** Parsers, compilers, LSP, git and repository
   tooling decide; similarity may only nominate.
3. **Transactional mutation.** Every write has preconditions, is atomic where
   possible, and reports its consequences.
4. **Repository-native execution.** Agents run declared repository commands
   instead of rediscovering shell incantations.
5. **Truthful degradation.** Every answer says how authoritative it is: exact
   when exact, approximate when useful, refuse when approximation is unsafe.
6. **Cheaper than the escape hatch.** If bash is easier, faster and cheaper
   for a workflow, Jade has failed that workflow.

A feature that makes Jade more like an agent, a knowledge base or an IDE, and
not more like a safer, cheaper transaction, fails this test however good it is
in a competitor.

And one architectural rule, from the fifth review: **core owns state and
effects; everything else provides evidence.** Parsers, language servers,
indexes, rankers and ecosystem discovery return symbols, references,
diagnostics, proposed edits and command plans. Core applies edits, runs
processes, advances revisions and decides what the agent is told. If a
capability is useful but not needed for transaction integrity, it does not
belong in core.

Two invariants follow, and hold for every phase and every later plugin:

- **The tool catalog is Jade's alone.** No language, provider or plugin adds
  a tool; they make existing tools answer better. Enforced by the existing
  contract test on the catalog.
- **Every path goes through core.** Providers receive paths core has already
  resolved and contained; none resolves a filesystem path itself.

---

## Phase 1 — 0.0.3: finish the release in flight

Theme: a good tenant, cheaper calls, honest checks. Already planned in
[ROADMAP.md § Release plan](../ROADMAP.md#003--good-tenant-cheaper-calls-honest-checks);
listed here so the sequence is complete.

- [x] JSON and YAML syntax checks after edit (ROADMAP §3)
- [x] Multi-query `find`, multi-range `read_range`, `read_range` clamping past
  EOF (ROADMAP §2)
- [x] `check` says what will run before running it (ROADMAP §4)
- [x] Document and anchor revisions to git (ROADMAP §5) — checkpoints record
  `HEAD`, and revert refuses across a commit (f76ee88)
- [x] **Workspace boundary enforced.** Done 2026-09-13: `internal/pathguard`,
  tested per escape on every path tool. Foundational correctness, so it ships in
  0.0.3 rather than waiting for the transaction work. Every file operation —
  read, edit, create, delete, rename, checkpoint restore, `read_range`,
  formatter and LSP edits — resolves through one core path function that
  rejects absolute paths outside the root, `..` escapes, and symlinks whose
  target leaves the root, with an error naming the root. Tests for each
  escape, on every tool that takes a path.
- [x] **Typed validation outcomes.** Done 2026-09-13: `protocol.ValidationOutcome`,
  classified once in `jobs.Outcome`. `check`, `run_tests` and `run_command`
  report one of a closed set — `passed`, `failed`, `unavailable` (no command
  discovered, tool not installed), `running` — plus `timed out` as its own
  state instead of `running`. `unavailable` can never render or count as
  passed. Telemetry uses the same set, which also closes the timeout hole
  recorded in [feedback.md](feedback.md).
- [ ] README status line says 0.0.3; the review was still reading "0.0.2"

**Exit gate:** ROADMAP release gate for 0.0.3 — `make conformance` green,
release notes draft section removed, tag `v0.0.3`.

## Phase 2 — 0.0.4: evidence before features

Theme: find out whether Jade beats the shell before building more of it.
Caution 2 cannot be answered by opinion; this phase produces the data every
later phase uses to decide what to merge, cut or add.

- [x] **External benchmark with bash as the baseline** (ROADMAP § Evidence).
  Three arms — agent + shell, agent + Jade, agent + Jade + shell — starting
  with a pilot of at least five outside repositories covering Go, TypeScript,
  Python, Java and Rust. Record task success, tokens, tool calls, wall time,
  invalid edits, shell fallbacks, regressions and unintended side effects
  (files touched that the task did not need), failed validations, and recovery
  steps after a failed edit or check, and diff quality — lines and files
  changed against the smallest correct change, and formatting churn in lines
  the task did not need. *Done when* `jade-bench` can run
  an arm against a repository path and task list it did not ship with, and
  the pilot results table is committed — whatever it says. The repository
  list grows to 20+ by Phase 6; adding one must be a config entry, not code.
  Harness candidates, to pick before building one: Jade as a SWE-agent tool
  bundle, mini-SWE-agent as the bash arm, a SWE-bench Verified subset as one
  task source, SWE-ReX sandboxes as the runtime.
  *Progress 2026-09-13:* harness built — `jade-bench agent` runs the three
  arms with Claude Code headless against any checkout and task file, verifies
  success with the task's own command, records cost, tokens, turns, time and
  diff size, enforces the budget and applies the fixed scorecard
  ([benchmark.md](benchmark.md)). Still to do: pick the five pilot
  repositories and write their tasks, a sandbox the agent can authenticate
  in, and the pilot run itself. *Decided:* the pilot runs on the author's
  machine with `-allow-host`, and without Java (no JDK there) — Go,
  TypeScript, Python and Rust first; Java remains required before this item
  is done. Task files for cobra, ky and ripgrep are in `bench/tasks/`, each
  task checked through the harness to fail with a no-op agent and pass with
  the real fix.
  *Done 2026-09-13:* four repositories (cobra, ky, requests, ripgrep), 12
  tasks, four arms including a trimmed shell; results in
  [benchmark-results.md](benchmark-results.md). Java and a fifth repository
  move to 0.0.5.
- [ ] **Competitor arm.** The same tasks with Serena in place of Jade; SymForge
  and code-atlas if their setup allows. A comparison to learn from, not a gate —
  but if Serena wins on the inspect tasks, Phase 3 cuts Jade's inspect surface
  harder rather than competing on it. *Moved to 0.0.5 (2026-09-13):* not
  started, and not a gate.
- [x] **Reposition the README lead.** Done 2026-09-13: the lead describes the
  change transaction; principles 3, 7, 11 and 12 now state preconditions,
  repository-native execution and cheaper-than-the-shell. From "structural access to a codebase" to
  the change transaction, with the loop spelled out and the inspect tools as
  the means. Only claims Jade already backs; Phase 6 re-checks it. Fold the
  principles Jade's design principles do not yet state into the README:
  transactional mutation (preconditions, not just consequences),
  repository-native execution, and cheaper than the escape hatch.
- [x] **Decide which arm Jade optimises for.** *Decided 2026-09-13:* agent +
  Jade alone, built-in tools off, core profile. Jade + shell used 9% more
  tokens than the shell with Bash taking half its calls; Jade alone used 51%
  fewer than the full shell and 18–25% fewer than a trimmed one. The README
  recommends it. If agent + Jade + shell wins,
  that is the supported configuration and the README says so; Jade's job
  becomes owning the transaction, not replacing every shell call. The
  criterion is that agents use Jade where Jade is better — not that the shell
  disappears.
- [x] **Fix the scorecard before the first run.** Written into the benchmark
  docs and not changed after results are seen. *Decided 2026-09-13:*
  agents run as Claude Code headless (`claude -p`), with and without Jade's
  MCP server, on Sonnet 5, pilot capped at $50; tradeable threshold is up to
  +10% tokens for each +5 points of task success. *Non-negotiable* against the
  shell arm: task success at least equal; invalid edits and unintended edits
  no more. *Tradeable:* tokens, tool calls and wall time, against success and
  diff quality, within stated thresholds — for example up to +10% tokens is
  acceptable for a success gain of at least 5 points. The thresholds are a
  decision to make and record, not to infer from the results. A strict
  every-metric gate would reject a system that succeeds more often while
  spending slightly more.
- [x] **Measure tool confusion** (ROADMAP § Evidence). Built 2026-09-13:
  `telemetry` reports same-target switches, retries and never-called tools;
  the pilot suite's report is recorded in
  [benchmark-results.md](benchmark-results.md). Extend telemetry, still
  content-free, with the sequences that signal a wrong pick: a tool followed
  by a different tool on the same target, retries after `ambiguous` or
  `not_found`, tools never called. *Done when* `telemetry` reports those
  per tool, and one benchmark run's report is recorded.
- [x] **Fix the 1.36x case for real.** 11.9 fixed the turn; the tokens were
  `read_symbol` returning a whole body and `find Store` returning two types.
  *Done when* that scenario re-measures at or below 1.0x, or the reason it
  cannot is written down. *Closed 2026-09-13 with the reason:* the scenario
  ran once, in a private repository not available to re-measure, and one
  question of three calls is superseded as evidence by the 36-run pilot. Its
  two token causes remain and belong to 0.0.5's response budgets: whole
  `read_symbol` bodies, and `find` answering every type of a name.
- [x] **Scope or stop the per-edit whole-repository typecheck** (ROADMAP §2
  follow-up). Done 2026-09-13: stopped; single edits start no job. Cost nobody reads distorts every benchmark number.
- [ ] **Test the retrieval vocabulary map away from home.** `retrieve` expands
  a query with a fixed word list (auth → session, login, token…) at
  [index.go:936](../internal/code/index.go#L936). Run the outside benchmark
  with and without it; keep it only if it helps there, and in Phase 4 it
  becomes a ranking provider rather than a map in the index. *Moved to 0.0.5
  (2026-09-13):* `retrieve` is not in the core profile, so no pilot agent
  called it; measure with a profile that lists it.
- [x] **`replace_text` line counts** and **`run_command` listing size**
  (done 2026-09-13)
  (ROADMAP §8). Small, and both inflate what the benchmark measures.

**Exit gate:** a results table from outside repositories exists in `docs/`,
the README quotes it with its caveats instead of only the home number, and
the confusion report names the tools Phase 3 should look at first.

## Phase 3 — 0.0.5: a surface an agent can route

Theme: answer caution 2 with the Phase 2 data, not before it.

- [ ] **Merge or cut overlapping tools** the confusion report implicates.
  `find` / `search` / `grep` / `retrieve` / `context` / `repository_map` /
  `read_symbol` / `read_range` / `outline` is the known cluster. A tool stays
  only if its description can say, in one sentence, when to prefer it over
  its neighbours. Removals follow the breaking-change policy in
  [tool-contract.md](tool-contract.md) — deprecate in 0.0.5, remove before
  0.1.0. The catalog is Jade's alone: nothing — language support, providers,
  later plugins — adds a tool. A framework-aware provider makes `references`
  or `retrieve` better; it never adds `django_models`. That is what keeps
  profiles and schema size predictable.
- [ ] **Tool profiles** (ROADMAP § Surface). *Measured 2026-09-13:* the full
  catalog is 35 tools, 22.6 KB of `tools/list`, and costs about 6.9k prompt
  tokens on every turn (a one-turn Haiku run: 13.6k with Jade loaded against
  6.7k with no MCP server). At the pilot's ~20 turns per task that is ~140k of
  a ~1M-token run. The nine-tool subset the benchmark's Jade-only agent
  actually leaned on (find, grep, read_range, outline, replace_text, insert,
  apply, check, run_tests) is 9.0 KB. Moved ahead of Phase 3 by the benchmark:
  tokens are a category Jade has to win outright. A core profile selectable at
  launch, with the full catalog opt-in. Measure schema bytes first: record the
  full catalog's `tools/list` size today, and set the core profile's budget
  from it (SymForge claims ~4.8 KB compact). Two shapes are plausible — about a
  dozen narrow tools, or four grouped ones (`inspect`, `edit`, `validate`,
  `state`) — and ROADMAP warns that grouping moves routing into arguments.
  Benchmark both; ship the one that wins. Add **host profiles** that leave out
  what the host already does better — a host with good file reads and search
  does not need Jade's versions of them in context — the way Serena disables
  its own overlapping tools inside agent harnesses. The bar for every tool in a
  profile: it keeps Jade preferable to the host's own tool for that job.
- [ ] **Response budgets with continuation** (ROADMAP § Surface). *Design
  written 2026-09-13* in [tool-contract.md](tool-contract.md#budgets-and-provenance--005-design).
  *Progress 2026-09-13:* continuation store in `internalapi`; `grep` takes
  `budget` and `continue`, refuses a handle after an edit. `find`,
  `references`, `read_range` and the rest still to do. One `budget`
  convention and one continuation handle across every tool that can return a
  list or a body — `24 of 87 references shown · continue=…` — replacing
  per-tool limit arguments, which are deprecated.
- [ ] **Provenance on every answer** (ROADMAP § Trustworthy; *design written
  2026-09-13* in [tool-contract.md](tool-contract.md#budgets-and-provenance--005-design)), moved forward
  from Phase 4: it and budgets are the same kind of change — a convention on
  every response — and belong in one release. Three parts on one compact line:
  certainty (`exact` / `structural` / `approximate` / `text fallback`), source
  (`gopls`, `tree-sitter`, `text index`), and completeness (`complete`,
  `may be incomplete`, `parse errors`, `stale`). For example
  `exact · gopls · complete` or `approximate · text index · may be incomplete`.
  The source names a backend; tool names and arguments never do, so SCIP or
  another backend can arrive later without changing the API. The certainty
  classes are a closed set defined by core; a provider reports which class its
  answer falls in and what limits it, and core renders the line. No provider
  chooses its own confidence wording.
  *Progress 2026-09-13:* `Provenance` type and closed sets in core;
  `references`, `grep`, outlines, `find`, `search` and `retrieve` carry it.
  `context`, `repository_map` and the edit responses' `checked:` wording
  still to align.
- [ ] **Say where the shell is still the right tool.** The review is right that
  Jade will not match shell composability, and should not try. Document the
  boundary — `run_command` and declared commands are the sanctioned escape
  hatch; one-off probes and debugging a script are shell work (see
  feedback.md, 11.10) — so "use the shell" is a decision, not a leak. Unlike
  SymForge, builds, tests, lint and codegen stay on the Jade side of that
  line: validation run from the shell is validation the transaction cannot
  see.
- [ ] **Shrink the full catalog, not only the core profile.** The second
  review's 10–15 is a hypothesis to test against the confusion report, not a
  quota. Every tool that is rarely called or often mis-picked is merged or
  deprecated unless a benchmark task shows what it uniquely saves.
- [ ] **Host integrations, in order:** OpenCode, goose, Cline, Codex CLI,
  Gemini CLI. For each: a config snippet in the README, a live session
  verified with the core profile, and notes on what the host already does
  (Cline's checkpoints and diff review, for instance) so Jade's instructions do
  not duplicate or fight it. goose first among equals if its ACP passthrough
  really gives Claude Code and Codex Jade through one config.

- [ ] **Carried from 0.0.4.** A Java repository and a fifth pilot repository
  (needs a JDK on the benchmark machine), the Serena competitor arm, and the
  retrieval vocabulary test away from home.

**Exit gate:** core profile is at most a dozen tools; the full catalog is
smaller than 35 with each removal justified by telemetry; re-running the Phase 2
benchmark on the core profile is no worse than on the full catalog; no two
tools in the contract lack a when-to-prefer sentence; OpenCode and goose
sessions verified; every inspect and edit response carries provenance and
every list-returning tool honours `budget`.

## Phase 4 — 0.0.6: predictable across environments

Theme: answer caution 3. Degrading is fine; degrading silently is not.

- [ ] **Workspace capability report.** One call (and the server instructions,
  in brief) listing per language: grammar or text fallback, language server
  present or missing, rename available, formatter active; plus git present,
  declared commands, discovered `check` targets. *Done when* an agent in a
  container with no language servers learns that from one response instead of
  from failed calls.
- [ ] **Providers behind a registry, still in one binary.** The abstraction is
  the capability registry, not a type hierarchy: a provider has an ID and
  declares the capabilities it serves (outline, symbols, references, rename,
  diagnostics, format, validation discovery, ranking), and implements only
  the small interface for each one it declares. Start with the capabilities
  that have more than one real provider today — references, rename,
  diagnostics — and add others as a second provider appears, rather than
  designing eight interfaces up front. Initial providers: tree-sitter, each
  language server, the text index, the existing discovery code. A future
  subprocess adapter must be able to register as just another provider. Per request, core picks the strongest available provider for that
  capability (`gopls` exact, then text index approximate), and provenance
  names the one that answered. Generalise the existing `internal/code` → LSP
  bridge rather than exposing LSP as the interface. *Done when* adding a
  language touches a provider and a registration, not the tools, and the
  capability report is generated from the registry. This is the seam an
  out-of-process plugin later plugs into; no protocol yet.
- [ ] **Three kinds of missing, told apart.** Not supported (no provider for
  this language), provider present but dependency missing (`rust-analyzer`
  not installed), provider running but degraded (`jdtls still indexing`). The
  capability report and each answer say which; conformance asserts all three.
- [ ] **Same words as provenance.** The capability report uses Phase 3's
  provenance vocabulary, so what the report promises and what each answer
  reports can be compared directly.
- [ ] **Own the abstraction leak.** Language-server trouble is reported as the
  server's, with what to do: `jdtls still initializing (38s) · references
  approximate until ready`, `pyright exited: …`, `pyright ignored
  pyrightconfig.json: …`. Startup and first-answer time per server measured
  in conformance, with a budget that fails the suite when a release makes it
  worse. A crashed server restarts once, then degrades with a reason.
- [ ] **Conformance covers the degraded paths.** Each language run once with
  its server and once without, asserting the response says which it got.
  Today conformance proves the happy path.
- [ ] **Multi-project `check`** (ROADMAP §4) — part of knowing what the
  environment offers.

**Exit gate:** capability report shipped and conformance-tested both with and
without servers; a response's provenance never claims more than the report
does.

## Phase 5 — 0.0.7: the change transaction holds

Theme: the part competitors do not have — read, edit against a known
revision, validate with the repository's commands, report consequences,
revert — is dependable end to end, including in long and concurrent sessions.
Ordered so the transaction is correct before anything is built on top of it:
impact-aware validation comes after the write path it depends on, not before.

- [ ] **One write path.** Every change to the workspace — `replace_text`,
  `insert`, `delete_symbol`, symbol replacement, formatter output, LSP rename
  and checkpoint restore — goes through a single transaction step that checks
  preconditions, writes, updates the index and advances the revision.
  Providers return proposed edits; they never call `os.WriteFile`. Fixes a
  real bug on the way: a rename across files becomes all-or-nothing instead of
  stopping part-way. A test fails if `os.WriteFile` appears outside that step.
- [ ] **Write the edit contract down, and test it.** For every write tool, in
  [tool-contract.md](tool-contract.md): preconditions (expected revision,
  unique target, target unchanged since read, path inside the workspace),
  atomicity (`apply` all or nothing; what a single edit guarantees),
  postconditions (disk state, index updated, diagnostics collected, new
  revision, consequences reported). One test per guarantee, so the contract
  cannot silently weaken — this is the part competitors have not built, and
  the hardest to commoditise.
- [ ] **Preconditions see changes Jade did not make.** Today an edit made by the
  user, the host or another tool leaves Jade's revision unchanged, so a stale
  `expectedRevision` still passes. The requirement: a mutation detects any
  change to its target since the read that informed it. Likely shape — a
  workspace generation that advances on detected external change, plus a
  content fingerprint of the target returned by reads and accepted by edits
  (`read_symbol` → `r41 · digest abc123`; `replace_symbol expected=r41
  digest=abc123`) — but the API is decided in this task, not here. Refusals
  say what changed and who is known to have changed it. Tested by editing a
  file from outside Jade between a read and an edit.
- [ ] **Checkpoint and revert that restore state.** Part of the edit contract,
  with a test for each:
  - *Revisions are monotonic.* A revert creates a new revision whose content
    equals the checkpoint's: `r17` checkpoint, `r18`, `r19` edits, `r20`
    revert. Revision identity never moves backward, so no two states share a
    name.
  - *Existence as well as contents.* A file modified since the checkpoint is
    restored; one deleted since is recreated; one created since is removed.
  - *No silent partial restore.* A restore that cannot write every file fails
    as a whole and says which, instead of discarding errors.
- [x] **Revisions against git, enforced.** Landed early, in 0.0.3 (f76ee88):
  each checkpoint records `HEAD`, and `revert` refuses with both commits named
  when one has landed since. What remains is the restore itself, above.
- [ ] **Discovery returns plans, core runs them.** Ecosystem discovery (Go,
  Node, Cargo, Maven, Gradle, pytest, bundler) returns a command plan — kind,
  command, arguments, directory — and `jobs.Runner` executes it with its own
  timeout, process group, output clamping, exit-status verdict, typed outcome
  and telemetry. Same shape for declared commands, so `check` has one input
  type.
- [ ] **Record the boundary** in [scope.md](scope.md): what core owns, what a
  provider may do, the two invariants, and the conditions a future plugin
  protocol must meet (see *Deferred*). Written now so the refactors above do
  not close the door and the plugin work later does not reopen the argument.
- [ ] **Strengthen declared commands.** They are how validation stays inside
  Jade. Declared commands appear in the capability report, `check` can run
  them as validation steps by kind (`lint`, `codegen`), and their runs appear
  in `changes` and `events` alongside edits, so the whole transaction is in
  one record.
- [ ] **Validation chain through declared commands.** Document and test a
  `.jade/commands.json` that runs Semgrep (or any repository rule tool) after
  diagnostics and tests, with exit status authoritative. No native
  integration; the example is the feature.
- [ ] **Impact-aware validation** (ROADMAP § Trustworthy), on top of the
  single write path. References name the affected packages and likely tests;
  those run first. code-atlas already detects affected tests, so this is
  parity for the inspect side — what Jade adds is running them inside the
  transaction and folding the verdict into the edit response. Target shape:

  ```text
  r41 → r42 · auth/session.go::Validate
  impact: 6 callers · 2 handlers · 3 likely tests
  checked: gopls ✓ · targeted tests running (job-17)
  ```

  Symbol changed → references → affected packages → declared commands → the
  smallest validation that covers them.
- [ ] **Concurrent-agent test.** Two sessions against one workspace: stale
  edits rejected, neither loses the other's work, `changes` attributes each.
  The review singles this out as where revisions earn their keep; it is not
  tested today. Measure what each session's language servers cost; if two
  sessions booting jdtls or rust-analyzer twice is the bottleneck, sharing
  servers across sessions (codescout does) moves up — otherwise it waits. No
  daemon without profiling; but check that nothing in the LSP client assumes
  one process owns its server, so sharing stays possible later.
- [ ] **Long-running work without polling** (ROADMAP §4). Progress
  notifications; no 300-second cap on backgrounded jobs.
- [ ] **Stop reporting transient errors mid-edit** (ROADMAP §3).

**Exit gate:** a scripted session — checkpoint, create a file, delete another,
multi-file `apply`, edit a file from outside Jade, attempted stale edit,
failing check, fix, commit from the shell, attempted revert, revert to a
checkpoint before the commit — behaves as documented, and runs in CI.

## Phase 6 — 0.1.0: release

- [ ] **Re-run the Phase 2 benchmark** on the release candidate across all 20+
  repositories, plus a few added after the last tuning so no one tuned for
  them. Publish results as they are, competitor arm included.
- [ ] **Remaining host integrations verified** — Cline, Codex CLI, Gemini CLI —
  against the release candidate.
- [ ] **Outside users.** From 0.0.5 on, ask for installs in unrelated projects
  — issue template for "why I turned it off", and a one-command way to share a
  `telemetry` summary (already content-free) voluntarily. *Done when* at least
  a handful of outside users have run it for weeks and their reports are
  triaged into ROADMAP.
- [ ] **Stability promise for 0.1.x** in [tool-contract.md](tool-contract.md):
  tool names, required arguments, profile names, `budget` and continuation,
  provenance vocabulary, validation outcome set, capability-report fields and
  the edit contract's
  guarantees frozen; response wording still not.
- [ ] **Hardening statement.** README keeps "not hardened for untrusted input"
  and says what running Jade inside a sandbox covers (ROADMAP non-goals).
- [ ] **README rewrite of "Status"** from "early, usable" to what 0.1.0 does
  and does not promise, and removal of any number not re-measured on the
  release candidate. The transaction positioning from Phase 2 is checked
  against what shipped.
- [ ] Release notes in `docs/release-notes/v0.1.0.md`, CHANGELOG, tag `v0.1.0`.

**Release gate:** the six claims under *What 0.1.0 means* each point to the
test, benchmark or document that proves them. If the benchmark says agent +
Jade fails the scorecard fixed in Phase 2, 0.1.0 does not ship; the phase
that fixes it does.

---

## Deferred past 0.1.0

Deliberately not on this path because none of the six claims needs them:
SCIP backend, ast-grep structural search and rewrite, git signals in ranking,
shared language servers across sessions (unless Phase 5 measures otherwise),
native Semgrep or SWE-ReX integration, scratch root, Java and Ruby formatting,
ruby-lsp method rename.

Also after 0.1.0, with conditions attached now so they do not drift:

- **Docs, specs and ADRs in `retrieve`.** Only as a thin, deterministic
  addition (README, AGENTS.md, CONTRIBUTING, ADRs, `docs/`), and always
  labelled as declared intent, separate from code as implemented truth. An
  ADR must never shape a structural answer about what the code does.
- **Git ranking signals** (co-change, churn), only where every ranked result
  says why it ranked: `same package · implements Provider · changed with
  provider.go in 14 of 19 commits`. No opaque relevance scores.
- **Embeddings**, only as a nominator whose picks a parser, LSP or git
  confirms. Never an authority (ROADMAP non-goals).

- **Out-of-process plugins**, built on Phase 4's registry and Phase 5's single
  write path. Conditions agreed now:
  - Subprocesses over stdio JSON-RPC, a protocol far smaller than MCP:
    `initialize` with a protocol version, then one versioned method set per
    capability (`jade.capability.references/v1`…). A plugin may provide any
    subset.
  - Plugins do not write to the workspace. First-party plugins comply by
    contract from day one; third-party plugins get brokered file access
    (`workspace.read`, `workspace.list` back to core) before they are
    supported.
  - Authority classes come from the protocol; core renders them.
  - Installation is the user's: `~/.jade/plugins/` and `jade plugin
    install|list|remove|doctor`. A repository's `.jade/config.json` may *name*
    a plugin it wants; Jade never launches a binary found inside a
    repository.
  - No plugin adds MCP tools.
  - Package split only when there is a reason. Language support and its
    ecosystem ship together (`jade-lang-rust` includes Cargo discovery).
- **WASM for providers that need no OS access** — rankers, policy,
  post-processing — with a deliberately tiny host API. After subprocess
  plugins, not instead of them.
- **Ranking plugins** (git history, call graph, embeddings) contribute scores
  to candidates core generates; core aggregates and applies the budget. A
  plugin never owns `retrieve`.

Not planned at all: memories, planning, prompt workflows or other agent
features; a Joern or other code-property-graph adapter in core (a
security-focused agent can run one beside Jade); handing builds and tests
back to the shell; Go's `plugin` package; plugin-defined tools; running
binaries shipped inside a repository.
