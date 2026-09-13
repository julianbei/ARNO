# Changelog

## Unreleased

### `grep`, `find`, `references` and `read_range` page by budget

The first tools on the 0.0.5 budget convention
([tool-contract.md](docs/tool-contract.md#budgets-and-provenance--005-design)).

- **`budget`** sizes the answer in tokens (four bytes each). It is cut at whole
  matches, never mid-line, and says so:
  `87 matches in 12 files, shown 1-24 · continue=c3 · exact · text search · cut`.
- **`continue=<handle>`** returns the next page, under the same budget or a
  new one. The last page reads `shown 73-87, the last · … · complete`.
- **A handle is never answered from a different state.** After an edit it is
  refused with both revisions named: `continue=c3 was cut at r12; the
  workspace is at r14 — repeat the call`.
- `limit` still works and is marked as the older form. A budgeted grep pages
  through its first 2,000 matches; the total still counts past them.
- **`find`** cuts at whole declarations, bodies included, and pages through
  its first 500; **`references`** cuts at whole references. A handle answers
  only the tool that cut it.
- **`read_range` no longer cuts the middle out of a large file.** A read past
  20,000 bytes dropped everything between its head and tail with
  `… bytes omitted …`. It now reads whole lines up to its budget (default
  5,000 tokens, the same size) and says `lines 1-612 of 3000 · continue=c7`
  for the rest. A range in `ranges` pages the same way, with its own
  `read_range` handle.
- **`diff` and `job_output` page instead of cutting the middle.** A patch or a
  job log past 8,000 bytes lost its middle — in a failing test run, often the
  failure. Both now return whole lines up to a budget (default 2,000 tokens,
  the same size) with `continue=<handle>`. A job keeps up to 1 MB of output
  for paging; `check` and `run_tests` verdicts still read the clamped form. A
  job-output handle survives edits, since a finished log does not change.
- **`read_symbol`** takes `budget`: a long body comes in whole-line pages
  (`body lines 1-212 of 900 · continue=c5`) instead of stopping at 150 lines
  with `// ... truncated`. **`workspace_tree`** pages at whole entries.
- **`history` with `includePatch`** pages its patch at whole lines (default
  1,500 tokens, the 6,000 bytes it used to cut from the middle).

### Answers say how sure they are

The first provenance lines from the 0.0.5 design
([tool-contract.md](docs/tool-contract.md#budgets-and-provenance--005-design)):
certainty, source and completeness, from closed sets, on the answer's first
line.

- **`references`** leads with its summary and
  `exact · <language server> · complete`, or
  `approximate · text index · may be incomplete` for name matching. The
  summary used to come last, after every reference.
- **`grep`** ends its summary with `exact · text search · complete`, or `cut`
  when the limit stopped it.
- **An outline** carries `structural · tree-sitter · complete` for a grammar
  parse, and `text fallback · text scan · may be incomplete` or
  `parse errors` otherwise, next to the revision.
- **`find`** says `structural · tree-sitter` when every code file it read had a
  grammar, and `text fallback · tree-sitter, text scan · may be incomplete`
  when one was read by a text scan — a declaration there may have been
  missed. `cut` when the limit stopped it.
- **`search`** and **`retrieve`** say `approximate · text index`: ranked name
  and term matches, never resolved. So does **`repository_map`**, `cut` when
  files were omitted.
- **`context`** says how its callers and tests were found:
  `callers exact · gopls · complete` or `callers approximate · text index ·
  may be incomplete`.

### A crashing language server is restarted once, then reported

A server that died was started again on every call, so one that crashes on a
workspace cost a crash and a startup per request. It is now restarted once;
if it dies again, the language is marked failed for the session with the
reason — `the python server exited again after a restart` — which
`capabilities` and an approximate `references` report.

An exact `references` answer from a server that was still indexing when the
wait gave up now says so — `· the java server is still indexing (Importing
projects); ask again when it ends for a complete list` — and its provenance
reads `may be incomplete`. Such a server answers wrongly rather than slowly,
and the list was presented as complete.

### `capabilities`: what Jade can do here, in one call

The first 0.0.6 item. An agent in a repository without language servers
learned that from failed calls, one tool at a time. `capabilities` answers
up front:

```text
go (412 files) · structural · tree-sitter · server gopls · formats with gofmt
typescript (88 files) · structural · tree-sitter · no server (typescript-language-server not installed) · references approximate, rename refused
kotlin (3 files) · text fallback · text scan · may be incomplete · no server known
git: yes
build: go build ./...
tests: make test
declared commands: release-gate
```

It uses the provenance words, so what the report promises and what each
answer reports can be compared directly.

It tells the kinds of missing apart, and reads them without starting a
server: no server known for the language, server not installed, server
installed but failed (`server pyright-langserver failed: exit status 1`), and
the working states `not started`, `running` and `indexing (Importing
projects)`.

The opening server instructions carry the short form, so an agent knows
before its first call: `This workspace: go (grammar, server gopls); kotlin
(text scan only, no server known). Call capabilities for build and test
commands and details.`

`references` says the same when it falls back to name matching —
`rust-analyzer is not installed`, `the python server failed: exit status 1`,
`no language server is known for this file type` — where it said
`gopls unavailable` for every language.

### Five overlapping tools deprecated

`search`, `search_nudge`, `repository_map`, `read_symbol` and `replace_range`
still work and are removed before 0.1.0. Their descriptions now start with
`Deprecated, removed before 0.1.0:` and name the replacement — `find`, `grep`,
`retrieve`, `replace_text` or `apply`. The evidence per tool is in
[tool-contract.md](docs/tool-contract.md#deprecated-in-005): none was called in
36 benchmark runs, and `read_symbol`'s eight calls while building Jade all
answered `not_found`.

### Where the shell is still the right tool

The README says which work belongs in the shell — git operations, one-off
probes, debugging a script, installing dependencies, network access — and
which stays in Jade even though a shell could do it: builds, tests, lint and
codegen through `check`, `run_tests` and declared commands, so validation is
part of the change record.

### `grep`'s glob reaches below the directory it names

Reported from a Go and TypeScript repository: `glob: "internal/*"` answered
"no matches" for three declarations under `internal/storage/` and
`internal/core/`, because `*` never crossed a `/`. The same happened building
Jade (`internal/*.go`).

- **A glob with a directory selects files below it.** `internal/*`,
  `internal/*.go` and `internal/**/state.go` match
  `internal/core/state.go`; `internal/code/*` still selects only that
  directory's tree, and `*.go` still matches at any depth.
- **An empty answer says what the filter selected:**
  `no matches for "NewState" in the 0 files glob/exclude selected`, so a
  filter that emptied the search is not read as the code being absent.

### Found building 0.0.4 with Jade

- **An edit in `apply` accepts `text`.** `insert` on its own takes `text`,
  an edit in `apply` takes `newText`; a batch of 21 edits was refused for
  carrying the one spelling into the other.

## 0.0.4

### Project configuration in `.jade/project.json`

A committed file tells Jade how the repository builds and tests, instead of
leaving it to discovery's guesses:

- **Areas** with a path, a language and build, typecheck, test, `testFile` and
  `testName` commands (`{file}` and `{name}` placeholders). `check` and
  `run_tests` use them first, inside the area's path; for a file, the deepest
  area containing it decides. Empty fields fall back to discovery.
- **`env`**: an interpreter put first on `PATH`, and variables for every
  command. **`generated`**: paths `grep`, `find` and the tree skip.
  **`notes`**: sent with the list of areas when a session starts.
- An invalid config fails checks and tests with the reason rather than falling
  back to guessing, and `check` with `dryRun` names the config as the source.
- `jade-mcp init` drafts the file from discovery without overwriting one.

The pilot benchmark hit each case this settles: the system interpreter under
a Makefile's pytest, a whole npm test script for one file, cargo testing only
the root package, and a Go module with Node packages beside it.

### Fewer failed calls

Every failed Jade call in the pilot suite, classified: 14 in 24 runs.

- **A missing anchor says where the file differs.** Eight failures were edit
  anchors written from memory; one agent retried six rewrites because
  "anchor text not found" did not say which line was wrong. `replace_text`,
  `insert` and `apply` now add the first line where anchor and file part
  (`line 1482 reads "return c.Name()" where the anchor has
  "return c.displayName()"`), or that the text is there with different
  indentation.
- **`read_range` no longer advertises `startLine` and `endLine`.** Agents
  still wrote `"startLine": 1204, 1240`, invalid JSON the host rejects, three
  times in the suite. The schema offers only `lines`; the old fields are still
  accepted.
- **A grep-style alternation with a literal parenthesis is searched as meant.**
  `test('lowercase method\|toUpperCase` failed as an invalid regex: the retry
  that escapes parentheses ran before the `\|` translation, which removed the
  escape again.
- **A read refused outside the workspace names the dependency form.** A path in
  the Cargo registry, Go module cache, `node_modules` or `site-packages` is
  answered with `dep:<name>/<path>` and grep's `dependency`.
- **A JavaScript test failure keeps its error message.** ava prints the
  inspected error object before the `HTTPError: Request failed with status code
  400` line; with a request's options in that object the summary's 400-byte
  bound cut the message, and a rerun agent spent twenty turns adding logging to
  find it. `Name: message` lines now come first in the summary.

A rerun of Jade alone and the lean shell after these changes (12 tasks each):
both solved 11 of 12; Jade used 18% fewer tokens, 14% fewer turns and 24% less
time, and 0.42 failed calls per run against 0.67 in the suite.

### Acting on the pilot benchmark

The full pilot suite (36 runs, see `docs/benchmark.md`) found that Jade alone
solved 12 of 12 tasks with 51% fewer tokens than Claude Code's built-in tools,
and that most of the saving is the tool list itself: the built-in tools are
38k tokens of prompt on every turn, Jade's core profile 14k. Given both, the
agent used Bash for four calls in five and spent 9% more tokens. A shell
trimmed to Bash, Read, Edit and Write, measured afterwards, used 34% fewer
tokens than the full list; Jade alone used 25% fewer than that, took 24% less
time and solved the two tasks both shell setups failed.

- **The README recommends Jade in place of the built-in tools**, with the
  Claude Code invocation (`--tools ""`), the core-profile server config, the
  pilot's numbers and the trade-off: no arbitrary shell commands.
- **`jade-bench agent` has a `shell-lean` arm**: Bash, Read, Edit and Write
  only. Measured against it, a result says how much of Jade's saving is its
  tools and how much a shorter tool list.
- **Dependency source is readable.** `grep` and `find` take `dependency`, and
  `read_range` reads `dep:<name>/<path>`: the crate in the Cargo registry, the
  module in the Go module cache, the package in `node_modules` or the
  repository's virtual environment, at the version the lock or manifest pins.
  On ripgrep an agent searched for a type in `regex-syntax` and could not reach
  it. Reads only: a dependency gets its own index, edits use the workspace's,
  and a name that is a path is refused.
- **Edits in languages without a running language server get a syntax
  check.** No server ran for TypeScript, Python or Rust in the pilot, so every
  edit there said "not checked" and a broken edit showed up only at the next
  test run. When the server is not installed or not configured, TypeScript,
  JavaScript, Python, Rust, Ruby, Java and Scala files are parsed with Jade's
  tree-sitter grammars and syntax errors are reported with their position; the
  checker reads `tree-sitter (syntax only; <why the server did not run>)`.

### Fewer wasted turns for a Jade-only agent

Reading the Jade-only agent's calls, turn by turn, in further cobra runs found
turns lost to Jade itself, not to the task. The fixes are general; none is
specific to cobra or Go.

- **Edits return the edited region.** `replace_text`, `replace_range`,
  `insert` and `apply` answered only `+22 -20`, so the agent read the region
  back to see the result: nine re-reads, a turn each, in one task. They now
  return the edited lines as the file reads after formatting, with two lines
  of context, at most 16 lines per region and four regions per `apply`.
- **`read_range` takes `lines: "280-400"`** (also `"280-"` and `"280"`), on
  its own and in `ranges`. Agents repeatedly sent
  `{"path": "command.go", "startLine": 1195, 1230}`, dropping the second key.
  That is not JSON, so the host rejected each call before Jade saw it. One
  string leaves no second key to drop; `startLine`/`endLine` still work.
- **`grep` tries the other reading of a query that finds nothing.** A literal
  query written as a pattern (`func.*SetArgs`) is run again as a regex, and a
  regex that matches nothing (`func (c \*Command) ParseFlags`, whose
  parentheses form a group) is run again as literal text with its escapes
  removed. The answer says which reading matched. Before, the agent read a
  hint and repeated the call.
- **A passing test suite is no longer reported as failing.** Any `error` or
  `fail` in the output counted as a failure, and cobra's passing tests print
  `Error: if any flags in the group ...`. `apply` said `FAIL tests` and the
  agent re-ran every test to find out whether its edit was fine. Exit status
  decides; output only fails a run through lines that only failures print
  (`--- FAIL`, `FAIL`, `FAILED`, `panic:`, `undefined:`).
- **`jade-mcp --tools core` lists twelve tools instead of 35.** The host sends
  the whole tool list again on every turn: 23 KB for the full catalog, 10 KB
  for the tools agents actually called in the benchmark runs (`find`, `grep`,
  `read_range`, `outline`, `replace_text`, `insert`, `apply`, `check`,
  `run_tests`, `create_file`, `delete_file`, `workspace_tree`). Unlisted tools
  stay callable. The default is still the full list; `jade-bench agent
  -jade-tools core` passes the profile to the Jade arms.
- **`insert` no longer duplicates its anchor.** Agents write an insert like a
  replacement: the new code, then the anchor line it goes above. The anchor
  stays in the file, so it appeared twice and the file stopped parsing; each
  time cost two turns of repair. Text that ends (`before`) or starts (`after`)
  with the anchor now has that copy dropped.
- **`find` accepts a declaration as written.** `func (c *Command) Name`,
  `pub(crate) fn walk_dir` and `def parse_header(value)` find `Name`,
  `walk_dir` and `parse_header`. An agent that had just read the declaration
  searched for it verbatim, got "no declarations matching", and repeated the
  call with the bare name.
- **A passing `apply` check shows only its verdict.** The check summary is
  whatever the suite logged; under `pass tests` cobra's expected
  `Error: if any flags ...` lines still sent the agent to re-run every test.
- **`run_tests` scopes work outside Go.** `scope: file` and `scope: test` ran
  `go test` in every repository; in ky (TypeScript) they answered
  "go mod init ... FAIL" seven times in three tasks, and the agent fell back to
  the whole `npm run test` — lint, build and browser suites — whose unrelated
  failures cost it dozens of turns. Scoped runs now use the project's own
  runner: the JavaScript runner the package depends on
  (`node_modules/.bin/vitest|jest|ava|mocha`, else `node --test`), pytest, or
  `cargo test` (`--test <name>` for `tests/<name>.rs`). A dependency that is not
  installed reads as unavailable, with `npm install` named.
- **Test names match the way agents write them.** ava's `--match` wants the
  whole title, so a title's first words answered "Couldn't find any matching
  tests" six times in one ky round; a name without `*` is now matched as a part
  of the title, as jest, vitest, mocha and pytest already do.
- **A failing JavaScript, Python or Rust test's summary says why it failed.** ava
  and jest explain a failure in a detail block below the summary line, and the
  summary kept only `✘ [fail]: <title> Rejected promise returned by test`. An
  agent re-ran one test seven times and wrote DEBUG tests to see the value.
  pytest cuts its summary line to the terminal width
  (`FAILED tests/test_utils.py::test_parse - Ass...`), and cargo's summary kept
  `thread '...' panicked at tests/regression.rs:3:5:` without the message
  after it. The summary now carries the difference or error
  (`Difference (- actual, + expected): - 'a' + 'b'`,
  `TypeError: Illegal invocation`, pytest's `E` lines,
  ``assertion `left == right` failed left: "a" right: "b"``), without the
  code frame and stack, capped at 400 bytes.
- **Scoped Rust test runs test the right package.** Plain `cargo test` tests
  only the root package: in ripgrep, `run_tests` for a file in `crates/regex`
  ran the root package's tests, and a unit test's name found nothing. A file
  now runs `cargo test -p <package>` for the nearest `Cargo.toml` above it, and
  a test name alone in a workspace runs across `--workspace`.
- **A narrowed test run that ran nothing is no longer a pass.** cargo and
  `go test` exit 0 when a name matches no test (`running 0 tests`,
  `[no tests to run]`), and `run_tests` reported `pass` for a test that was
  never run. It now answers `no test matched "<name>" — nothing ran`.
- **`grep` with an unbalanced parenthesis searches for it.** `rgtest!(r\d+` was
  an invalid regex and cost a turn; when a pattern does not compile, it is
  retried with its parentheses escaped, and the answer says it was read that
  way. Patterns invalid for other reasons are still rejected.
- **Python checks use the repository's virtual environment.** `.venv/bin/python`
  or `venv/bin/python` is preferred over the system `python3`, which usually
  has neither the project nor pytest installed.
- **Validation commands run in the project's environment.** A Makefile target,
  npm script or tox run that calls bare `python` got the system interpreter:
  requests' `make test` (`python -m pytest tests`) would fail without the
  project or pytest installed. Commands now run with the repository's
  `.venv`/`venv` activated (`VIRTUAL_ENV`, its `bin` first on `PATH`) and
  `node_modules/.bin` on `PATH`, as a developer's shell would.
- **Validation commands are no longer killed after 60 seconds.** `run_tests`
  waits up to 300, but the command underneath was killed at 60: a cold
  `cargo test` compiles for minutes, so a first Rust test run always timed out.
  The kill bound is 10 minutes; the caller's wait still decides when to answer,
  and a slower job stays pollable.
- **`check typecheck` finds TypeScript's compiler.** A project with a
  `tsconfig.json` and no typecheck script runs `node_modules/.bin/tsc --noEmit`
  instead of answering unavailable.
- **Searches skip what git ignores.** `grep`, `find` and `workspace_tree`
  skipped a fixed list of directory names and nothing else, so ky's gitignored
  `distribution/` build output answered every search twice: one grep returned
  123 matches and 19.7 KB, the tree 4.7 KB. Untracked ignored paths are now
  skipped, as ripgrep does by default. Jade's own `.jade/` is never listed.
- **`lines` accepts `55, 125`, `55:125` and `55..125`.**
- **`jade-bench agent-report` counts output tokens correctly.** It read them
  from streamed events, which carry the count at the start of each message,
  and undercounted output about 40 times. Totals were unaffected; output now
  comes from the run's result.

### Jade gets used next to the shell, and searches several patterns at once

The first nine benchmark runs with full transcripts, on cobra:

- **With the shell available, the agent never called Jade.** Claude Code hides
  MCP tools behind tool search by default, and an agent that already has
  `Bash`, `Read` and `Edit` does not go looking: the jade+shell arm made 0 Jade
  calls in 3 runs and simply measured the shell again. With `"alwaysLoad":
  true` in the server config, the same agent called Jade directly. The README's
  configuration includes it now and explains why, and the benchmark's Jade
  arms set it.
- **`grep` takes `queries`** — several patterns with the same filters in one
  call, each answered and labelled in order. The Jade-only agent made about ten
  more search calls per task than the shell agent, which searched
  alternatives in one `grep` command. `query` is now optional, which the tool
  contract allows; the server requires one of the two.

### Two turn-wasters found in benchmark transcripts

The first pilot runs with full transcripts showed a Jade-only agent taking
more turns than the shell agent on one cobra task. Reading its calls found two
Jade defects, not agent choices:

- **`grep` with `regex: true` now accepts grep's `\|` alternation.** The agent
  wrote `version for %s\|%s version %s` as it would for `grep -n`. Go's regexp
  reads `\|` as a literal pipe, so Jade answered `no matches` three times and
  the agent split every search into single patterns. A pattern that uses `\|`
  and no bare `|` now has `\|`, `\(`, `\)`, `\+` and `\?` read as grep reads
  them; patterns already in RE2 form are untouched.
- **A failing test's summary keeps what it expected and got.** The summary kept
  `command_test.go:76: Expected to contain:` and dropped the lines after it, so
  the agent had to call `job_output` to see the values. A decisive line that
  ends in `:` now carries the next few lines, capped at 400 bytes.
- **Test summaries name the failing test first.** On another cobra task the
  Jade agent took 51 turns to shell's 23. `run_tests` reported a failure as
  eight `Error: if any flags in the group…` lines — expected errors printed by
  passing tests — and dropped `--- FAIL: TestCompleteWithDisableFlagParsing`,
  which came first. The agent chased the wrong test for eighteen calls,
  toggling its fix in and out, until `job_output` showed the real failure. When
  a runner names failing tests (Go `--- FAIL:`, pytest `FAILED`, cargo
  `... FAILED` and panics, ava `✘`), the summary is those names with their
  assertion lines and the package result.
- **A literal `grep` that reads like a pattern no longer answers a bare "no
  matches".** The same run searched `helpFlagName|helpCommand\b` and
  `func (c \*Command) ParseFlags` literally and got "no matches" four times.
  Such a query is now retried as a regex (see above).

### `jade-bench agent`: the external benchmark

Release plan Phase 2 needs evidence from repositories Jade was not built in.
`jade-bench agent -tasks <file> -repo <checkout>` runs every task with Claude
Code headless on three arms — shell tools only, Jade only, Jade plus shell —
and reports success, tokens, turns, cost, time and diff size per arm, with the
scorecard fixed before any run (success never below shell; up to +10% tokens
per +5 points of success).

- **Success is the task's own verify command**, with hidden tests written
  after the agent stops.
- **Each run starts from a history-free baseline**, so the fix a task came
  from cannot be found in `git log`.
- **The budget cannot be overrun**: a run is charged its cap when it prints
  no result.
- **Adding a repository is a task file.** The first four are in
  `bench/tasks/`: cobra (Go), ky (TypeScript), requests (Python) and ripgrep
  (Rust), three real fixes each, every one checked to fail before and pass
  after.

Agents run with permissions bypassed, so the command refuses to run outside a
sandbox unless `-allow-host` is given. See [docs/benchmark.md](docs/benchmark.md).

### `telemetry` reports tool confusion

Release plan Phase 3 merges and cuts tools by data, so the data has to say
where agents pick wrong. `telemetry` now adds three lines:

- `switched tools on the same target:` an inspect tool followed by a
  different one on the same file or symbol — `jade.read_range→jade.outline 4`.
  A read followed by an edit is ordinary work and not counted.
- `retried after a failed answer:` a tool called again straight after it
  answered `ambiguous` or `not_found`.
- `never called:` every tool in the catalog with no calls.

To find "the same target" each record now carries a 10-character hash of the
call's `path`, `symbolId`, `symbolName` or `query`. The log still stores no
paths, names, arguments or responses in the clear.

### Smaller, truer edit and search responses

Found while building 0.0.3 with Jade itself:

- **Single edits start no background job.** Each edit started a
  whole-repository typecheck (`replace_range` started the whole test suite)
  whose result no response showed. Diagnostics already come back inline;
  `check` and `apply`'s `check` validate when asked.
- **`replace_text` counts what changed.** Appending two lines after a kept
  anchor reads `+2 -0`, not a rewrite of the anchor. Same for `apply`.
- **`grep` skips binary files**, detected by a NUL byte in the first block as
  git and ripgrep do. A built binary without an extension was searched before.
- **Go structs are `struct`, not `class`.** `find` still accepts `type`,
  `struct` and `class` for them.
- **No "freshness unknown" before the first commit.** A repository with no
  commits has nothing to be stale against.
- **`run_command` with no name lists names and descriptions**, not every
  script; an undescribed command's script is clipped to 60 characters.

## 0.0.3

### Validation says passed, failed, unavailable, running or timed out

`check`, `run_tests`, `run_command` and `apply`'s check reported a free-form
status beside a `Passed` bool. A waited check that ran out of time read
`running`, the same as a call that never waited; a declared command whose tool
was not installed read `FAIL`, sending the caller to fix code nothing had
checked.

Each now carries one outcome from a closed set — `passed`, `failed`,
`unavailable`, `running`, `timed out` — and leads its response with it:
`timed out slow`, `unavailable lint`, `unavailable build` when no command was
discovered. Only `passed` renders or counts as a pass. A command killed for its
timeout is `timed out`, exit status 127 or a binary that cannot start is
`unavailable`, and exit status still decides `failed`. Telemetry uses the same
set, so timed-out waits and missing tools are now counted — the hole
[feedback.md](docs/feedback.md) recorded. `Status` and `Passed` remain in JSON
output.

### Nothing reads or writes outside the workspace

A path was used as given: an absolute path went wherever it pointed, `..` was
joined without a check, and a symlink inside the repository was followed out
of it. Nothing stopped an edit from writing outside the workspace, so "safe
to leave running" could not be claimed.

Every path a caller passes — to reads, edits, `create_file`, `delete_file`,
`apply`, symbol IDs, `rename` — now resolves through one function that refuses
absolute paths outside the root, `..` escapes, and symlinks whose target leaves
the root, with an error naming the root. A language server proposing a rename
edit outside the workspace is refused before any file is written. `grep`,
`find` and retrieval skip symlinked files that point outside rather than
reading through them. Absolute paths inside the workspace still work,
including ones spelled through a symlinked root such as `/private/var` for
`/var`.

### `revert` will not overwrite committed work

Jade's revisions ran alongside commits made from the shell, and nothing said
what `revert` would do to files git already had committed. It restored the
snapshot regardless — silently writing pre-commit content back over committed
files — so agents avoided `checkpoint` and `revert` entirely.

Each checkpoint now records git's `HEAD` and shows it. `revert` checks `HEAD`
before writing anything and refuses when a commit has landed since the
checkpoint, naming both commits. Within the work since the last commit it
behaves as before; without git, nothing changes. The tool descriptions and
README state the model: a checkpoint snapshots only files Jade edited, revert
restores only those, git is never moved, and checkpoints last for the session.

### `check` says what it runs

In a Go module with several Node packages and no Makefile, a caller could not
tell what `check kind=build` would execute — and a green result on the wrong
target is worse than none, so it was not used. Every `check` result now names
its command in the first line (`pass build · ran: go build ./...`), `dryRun`
names it without running anything, and a workspace with no recognisable
manifest is told so before any job starts.

### Fewer calls to read what you need

- **`find` takes `queries`** — several names in one call, answered in the
  order asked. Callers writing types against another language's structs sent
  two or three parallel calls for what is one question.
- **`read_range` takes `ranges`** — several files or ranges in one call. A
  range that fails reports its own error; the others still come back.
- **An end line past the end of the file reads to the end** instead of being
  rejected, and the header says so: `lines 190-312 of 312`. The rejection cost
  a whole extra turn every time. A start past the end is still an error.

`query` on `find` and `path` on `read_range` are now optional, since each has
an alternative; the server requires one of the pair. Making a required
argument optional is compatible under the tool contract.

### JSON and YAML are checked after an edit

A broken `package.json` or CI workflow used to surface only when something read
it next — a build, a deploy, a CI run. Edits to `.json`, `.yaml` and `.yml` now
parse the file with the standard parsers and return the first syntax error with
its location, named `checked: json` or `checked: yaml`. Multi-document YAML is
checked document by document. `tsconfig.json` and other JSON-with-comments
files are not reported as broken. A diagnostic with no column (YAML reports
only a line) is rendered as `path:line`, not `path:line:0`.

### Edits report errors in every language with a server

Only Go edits used to come back with diagnostics. Now every language with a
running server does, and every edit response names what checked it —
`checked: jdtls` — or why nothing did, so an empty result reads as "nothing
wrong" rather than "nothing looked".

Getting a real answer took four server-specific fixes, each found by the
conformance suite reporting a broken file as clean:

- **Fresh publishes only.** Diagnostics read right after a change are the old
  content's. Jade counts publishes per file and waits for one newer than the
  edit, then for the list to stop changing.
- **Pull diagnostics.** ruby-lsp never publishes; it answers
  `textDocument/diagnostic`. It also parses a request before applying the
  preceding change, so a pull sent straight after an edit is answered — and
  cached — from the old source. Jade round-trips a `documentSymbol` request
  first.
- **Ranged changes.** ruby-lsp declares incremental sync and cannot apply a
  whole-document change without a range.
- **Compile-on-save servers.** metals publishes an empty list on save and the
  real errors after compiling. For metals, only diagnostics that follow a
  completed compile count; until then the edit says `not checked`.

metals asks permission to import an sbt build before it can compile. Importing
runs sbt and writes `.bloop/` and `.metals/`, so Jade declines unless
`JADE_METALS_IMPORT=1` is set, and says which setting to use.

### A good tenant in someone else's repository

Jade's telemetry log appeared as an untracked file in every repository that
did not ignore `.jade/`, so a harness that commits a worker's worktree
wholesale committed Jade's bookkeeping as part of the agent's change. Now:

- In a git repository the log is added to `.git/info/exclude` before it is
  first written — clone-local, never committed — unless git already ignores
  it. `.gitignore` is never edited.
- `JADE_STATE_DIR` moves the log out of the workspace, one subdirectory per
  workspace.
- A call with an unknown tool name never creates the log.

### Shorter responses

Two lines that appeared on every call and that no caller could act on are
gone from text output:

- `drifted: N files` on reads. Content returned by a read is current either
  way, and `changes` reports what moved. A broken freshness check is still
  reported.
- `jobs: job-N` on edits. The background typecheck's result was never seen
  unless polled, and the edited file's own errors are already in the
  response. Job IDs remain in `JADE_JSON=1` output.

### A first call that works

Hosts load tool schemas lazily, so an agent's first call to any Jade tool used
to start with a schema-search turn. The server instructions now name the seven
tools to load first, say when to batch with `apply`, and give both name
spellings — in under 500 bytes, since every session pays for them.
`replace_text` and `insert` now point at `apply` for multi-site edits; an agent
that made twelve single edits in one session did not know it existed.

`initialize` reported `serverInfo.version` as a hardcoded `0.1.0`. It now
reports the binary's actual version.

### Tool names

`jade_find` is accepted as well as `jade.find`. Hosts rewrite the dot away
(Claude Code shows `mcp__jade__jade_find`), and an agent that learned the name
from its host was told the tool does not exist.

### Semantic answers wait for the server's index

Jade sent `references` and `rename` as soon as a server finished `initialize`.
An indexing server does not answer those slowly, it answers them wrongly:
ruby-lsp returned `null` for a class rename it got right two seconds later,
and a `null` rename reads as "cannot rename", not "ask again". rust-analyzer,
jdtls and metals all index asynchronously too, so the first semantic call
after startup was a race in four languages.

Jade now declares `window.workDoneProgress`, tracks the server's `$/progress`
tokens, and waits for them to end before a semantic request — bounded at 60
seconds, free when the server is idle, a moment on the first call for a server
that reports nothing.

### Rename falls back to an alternative server

A language's alternative server was only used when the primary was not
installed. Now it is also asked when the primary is running and declines.
ruby-lsp 0.26 renames classes but returns `null` for methods even when fully
indexed; `solargraph` renames methods correctly across files. With both
installed, Ruby method rename works. With only ruby-lsp, it still refuses and
repeats the server's reason.

### Formatting beyond Go and Rust

Formatters now run for TypeScript, JavaScript, Python and Scala — **only when
the repository declares them**:

| Language | Runs when |
|---|---|
| TypeScript / JavaScript | a prettier config or `"prettier"` key in `package.json`, and the project's own `node_modules/.bin/prettier` |
| Python | `[tool.black]` or `[tool.ruff.format]` in `pyproject.toml`; the project venv's binary first |
| Scala | `.scalafmt.conf` and `scalafmt` installed |

gofmt and rustfmt still run unconditionally: they are canonical and take no
config. Every other formatter does, and running one a project did not choose
turns a one-line edit into a whole-file diff. Config is searched from the file
upward to the workspace root and never above it, so nested packages work and
a repository never inherits a formatter from the directory it sits in. Ruby
and Java stay unformatted; see [ROADMAP.md](ROADMAP.md).

### Roadmap

[ROADMAP.md](ROADMAP.md) collects the planned work, each item tied to the
problem observed in real use.

## 0.0.2

Semantics. 0.0.1 could read and edit nine languages structurally but only
understood one of them — `references` was compiler-exact for Go and a
name-matched guess everywhere else, and `rename` refused outright outside Go.

### Language servers

Jade now speaks LSP. `internal/lsp` is a real client: process lifecycle,
JSON-RPC over stdio, initialize handshake, document sync, capability gating.
Servers start on first use, are reused for the session, and shut down on exit
— a server's first answer is expensive because it indexes, every one after is
cheap, which is exactly what the previous per-call CLI invocation threw away.

With a server installed, `references` and `rename` are compiler-exact and
cross-file in **Go, TypeScript, TSX, JavaScript, Python, Ruby, Rust, Java and
Scala**. Without one, nothing is lost: the gopls CLI remains a second path for
Go, `references` falls back to the name-matched graph and says so, and
`rename` still refuses rather than guessing — an approximate reference list is
useful to a reader, an approximate edit is corruption.

Servers are found on `PATH` and in the places toolchains actually install
them. `command -v gopls` finds nothing on a stock machine while gopls sits
working in `~/go/bin`; a PATH-only client would report Go as unsupported on
the system that supports it best.

### Grammars

Real tree-sitter grammars for **Python, Ruby, Java, Scala and JavaScript**.
Measured against the text-scan fallback they replace, on a trivial file:

    python      1 of 3 declarations found      now all
    ruby        1 of 3                         now all
    scala       1 of 4                         now all
    java        0 of 2 — nothing at all        now all
    javascript  3 of 4                         now all

Java is the one that mattered: an empty outline carrying a note that some
declarations "may be missing" reads as a small caveat, so the fallback was
worse than an honest refusal.

### Validation

`check` discovers Maven, Gradle, sbt, pyproject/setup.py and Gemfile by
manifest. Previously anything unrecognised fell through to the Go default, so
validating a Python repository ran `go build ./...` — not a degraded answer
but a wrong one, failing for a reason unrelated to the code. A project jade
cannot identify now says so.

### New tool

`insert` adds text without replacing anything. It existed only as an `apply`
op, so additive work had no tool to reach for and plain file editing won.
Adding a tool is backward compatible, but the MCP catalog is fixed at
connection time — **reconnect before `jade.insert` appears**. Surface is 35
tools.

### Fixes

- **`run_command` and `check` ignored exit status.** A command printing a
  success-looking line and exiting non-zero reported `pass`.
- **An anchor passed to `insert` with no position was ignored entirely**, and
  the text appended to the end of the file — the silent misplacement the
  anchor exists to prevent.
- **A guessed symbol ID dead-ended.** `path::Name` without the `@line` suffix
  now resolves when unambiguous, removing a lookup call that preceded nearly
  every edit. Ambiguous names are still refused, with candidates.
- **A misspelled argument failed in the wrong place.** `old`/`new` instead of
  `oldText`/`newText` was silently unread; now answered with the real name.
- **jade blamed a missing language server for a running one's refusal**, which
  sends someone to install what they already have.
- `go install`-ed binaries reported their version as `dev`.

### Verification

`make conformance` builds an image containing all eight language servers and
runs jade against a real repository per language: **8/8 structure, 8/8
semantics**. It found five real problems while being written, four of which
would otherwise have shipped.

One server limitation is recorded rather than hidden: ruby-lsp advertises
rename and then produces no edits for a method. Jade reports its reason.

## 0.0.1 — unreleased

First tagged version. The point of it is to be usable somewhere other than its
own repository, so that real feedback can start.

### What this is

An MCP server that gives a coding agent structural access to a codebase: read
and edit by symbol rather than by line number, validate the result, and track
what changed — without shelling out.

34 tools across four areas: **inspect** (outline, find, grep, read_symbol,
read_range, references, repository_map, search, retrieve, context,
workspace_tree, search_nudge), **modify** (replace_text, replace_symbol,
replace_range, delete_symbol, create_file, replace_file, delete_file, rename,
apply), **validate** (check, run_tests, run_command, declare_command,
job_status, job_output), and **state** (changes, diff, history, checkpoint,
revert, events, telemetry).

The full surface and its stability policy: [docs/tool-contract.md](docs/tool-contract.md).

### What is actually verified

Every tool has been exercised live over MCP against this repository, and the
whole surface has been run against an unrelated Go repository via
`--root`. Specifically checked:

- `apply` rolls back every edit when one fails, leaving the tree as it found it
- `replace_text` refuses an ambiguous anchor rather than editing the first match
- `create_file` refuses to overwrite; `replace_file` refuses to create
- `rename` refuses rather than guessing when `gopls` is unavailable
- Edits are formatted, diagnosed and revision-tracked on every path
- Jade starts and works on a non-git directory and on a repo with no commits

### Numbers, with their caveats

Jade's benchmark (`cmd/jade-bench`, seven scenarios, this repository) measures
**0.85x** the tokens of equivalent shell commands — down from **5.63x** before
responses became plain text rather than JSON.

**The counter-measurement matters as much.** One real question on an unrelated
repository ("is the CAS store's write atomic?") came out at
**1.36x** and one extra round trip, because an ambiguous symbol name forced a
disambiguation call. Seven scenarios at home and one away disagree; both are
honest, and the second is the one that predicts outside use.

### Known limitations

Stated plainly, because they tell you what is worth reporting:

- **Real grammars for Go, TypeScript, TSX and Rust only.** Everything else uses
  a text scan that finds some declarations and misses others. Jade says so in
  the response (`! no python grammar — …`) rather than presenting a partial
  outline as complete.
- **`references` and `rename` are precise only for Go**, via `gopls`
  subcommands. There is no LSP client. Without gopls, `references` degrades to
  a textual approximation and `rename` refuses.
- **Formatting covers gofmt and rustfmt only.** TypeScript, JSON and Markdown
  are deliberately untouched, since their formatters take project config and
  could reformat far more than the agent did.
- **Revision tracking is jade's own counter, not git's.** It catches concurrent
  edits within a session; it is not a VCS.
- **No blame, no arbitrary-revision blame, no cross-repo or remote execution.**
- **Telemetry is local only** — `.jade/telemetry.jsonl`, never transmitted,
  records no arguments, response bodies or error text. `JADE_TELEMETRY=0`
  disables it.
- **Not hardened against untrusted input.** It runs commands you declare and
  edits files you point it at. It is a development tool.

### The one thing to know before filing a bug

**The MCP tool catalog is fixed at connection time.** A newly added tool does
not appear, and a newly built binary is not used, until the client reconnects.
During development this accounted for every single "the tool is missing"
report, without exception.

### Install

```bash
make binary
./bin/jade-mcp --version
```

Point your MCP client at `bin/jade-mcp` and set `JADE_WORKSPACE_ROOT` (or pass
`--root`) to the repository you want jade to work on — it does not have to be
the jade checkout. Full stanza in the [README](README.md#install).

### Feedback

[docs/reporting.md](docs/reporting.md) and three issue templates: **bug**,
**friction**, **feature**.

The friction one is the one we want most: the moments you reached for `grep`,
`sed` or `cat` even though jade was there. **"It was just habit" is a real
answer** — it means the jade path was not the obvious one at the moment of
choosing, which is a design problem rather than a user error.

[docs/feedback.md](docs/feedback.md) is the same log kept from the inside across
development. Its blunt finding: the fallbacks that lasted longest closed within
two tasks of being *written down*, not when the tool shipped. `check` existed,
worked, and kept losing to `go test` for three tasks. Naming it fixed it.
