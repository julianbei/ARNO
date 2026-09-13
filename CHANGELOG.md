# Changelog

## Unreleased

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
