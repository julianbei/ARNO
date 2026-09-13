# Jade — Just Agentic Development Environment

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="logo-dark.png">
  <img src="logo.png" alt="Jade — Just Agentic Development Environment" width="520">
</picture>

**A change transaction for coding agents, served over MCP.** Read by symbol,
edit against a known revision, get the compiler's diagnostics back with the
edit, validate with the repository's own commands, see what changed, and
revert to a checkpoint — each step one tool call, none of it through the shell.

Symbol-aware reading is how Jade finds its way around; the transaction is what
it is for. Where the shell is still the better tool, use it — the question
Jade has to answer is whether an agent gets more done, at acceptable cost,
with it than without ([docs/benchmark.md](docs/benchmark.md)).

**Status:** 0.0.3 — early, usable, and looking for feedback.

```bash
go install github.com/julianbei/jade/cmd/jade-mcp@latest
```

---

## Table of contents

- [Why Jade exists](#why-jade-exists)
- [Install](#install)
- [Configure your MCP client](#configure-your-mcp-client)
- [Use it in a container](#use-it-in-a-container)
- [The tools](#the-tools)
- [Repository commands](#repository-commands)
- [Language support](#language-support)
- [Design principles](#design-principles)
- [What Jade does not do yet](#what-jade-does-not-do-yet)
- [Stability and versioning](#stability-and-versioning)
- [Telemetry](#telemetry)
- [Reporting problems](#reporting-problems)
- [Development](#development)

---

## Why Jade exists

An agent that falls back to `grep`, `sed` and `cat` is operating outside any
tooling you control. No revision tracking, no guardrails, no telemetry, no way
to know what it did or why it chose to do it that way. Every shell fallback is
a hole in your visibility.

You cannot fix that by telling the model not to use the shell. The model uses
the shell because the shell is *cheaper* — fewer tokens, fewer round trips,
more flexible. So the only durable fix is to make the structural tool the
cheaper option, and then measure whether you succeeded.

That is the entire bet, and it is testable. On this repository's own benchmark,
Jade answers seven realistic engineering questions in **0.85x** the tokens of
the equivalent shell commands. It was **5.63x** before responses became plain
text instead of JSON — see [docs/response-style.md](docs/response-style.md) for
what changed and why.

The counter-measurement matters as much. One question asked against an
unrelated repository came out at **1.36x** — worse than the shell — because an
ambiguous symbol name forced an extra disambiguation call. Seven scenarios at
home and one away disagree, both are honest, and the second is the one that
predicts outside use. Jade is not finished.

Jade's own development log ([docs/feedback.md](docs/feedback.md)) records every
time its author reached for bash instead, and why. The pattern it found was
blunt: the fallbacks that survived longest each closed within two tasks of
being *named in the log* — not when the tool shipped.

---

## Install

### From Go

```bash
go install github.com/julianbei/jade/cmd/jade-mcp@latest    # newest
go install github.com/julianbei/jade/cmd/jade-mcp@v0.0.3    # pinned
```

Lands in `$GOBIN`, or `$(go env GOPATH)/bin` if that is unset — which is
usually `~/go/bin`, and is **not** on `PATH` by default. Add it if it is not
there, then confirm:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
jade-mcp --version
```

If you would rather not touch `PATH`, use the absolute path in your MCP client
config instead of the bare `jade-mcp` shown below.

### From a release binary

Prebuilt binaries for linux and darwin on amd64 and arm64 are attached to each
[GitHub release](https://github.com/julianbei/jade/releases), with a
`checksums.txt` alongside them. Each is built natively on its own platform —
Jade links tree-sitter through cgo, so the linux builds need a reasonably
current glibc. On an older distro, build from source or use the container
image, which is statically linked against musl.

```bash
VERSION=v0.0.3
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -fsSL "https://github.com/julianbei/jade/releases/download/${VERSION}/jade-mcp_${VERSION}_${OS}_${ARCH}.tar.gz" \
  | tar xz
sudo mv "jade-mcp_${VERSION}_${OS}_${ARCH}" /usr/local/bin/jade-mcp
```

### From source

```bash
git clone https://github.com/julianbei/jade.git
cd jade
make binary          # bin/jade-mcp, version stamped from git describe
make install         # or straight onto your PATH
```

### Optional: gopls

`references` and `rename` use `gopls` for their exact, compiler-resolved form.
Without it they still work — `references` degrades to a textual approximation
that says so in the response, and `rename` refuses rather than guessing.

```bash
go install golang.org/x/tools/gopls@latest
```

---

## Configure your MCP client

Jade is a stdio MCP server. Point your client at the binary:

```json
{
  "mcpServers": {
    "jade": {
      "type": "stdio",
      "command": "jade-mcp",
      "env": {
        "JADE_WORKSPACE_ROOT": "/absolute/path/to/the/repo/jade/should/work/on"
      }
    }
  }
}
```

`JADE_WORKSPACE_ROOT` is the repository Jade inspects and edits. It does **not**
have to be the Jade checkout — pointing it somewhere else is the entire point.
A `--root /path/to/repo` flag takes precedence over the environment variable,
and Jade prints which of the three sources it used (flag, env, working
directory) at startup, so an agent can never quietly operate on the wrong
repository.

Jade works on a non-git directory and on a repository with no commits yet. In
both cases it says what is degraded — `changes`, `diff`, `history` and
`checkpoint` need git — and everything else keeps working.

### Two things that will confuse you once

**The MCP tool catalog is fixed at connection time.** A newly added tool does
not appear until the client reconnects. If you upgrade Jade mid-session and a
tool seems missing, reconnect before investigating.

**Use the binary, not `go run ./cmd/jade-mcp`.** A `go run` stanza recompiles at
every process start: measured here at 284–584ms to first handshake against 14ms
for the binary, with a *warm* build cache. A cold one is seconds. It also means
the server silently changes whenever the source does — useful while hacking on
Jade itself, confusing everywhere else. This repository's own `.mcp.json`
deliberately still uses `go run` for that reason.

### Environment variables

| Variable | Effect |
|---|---|
| `JADE_WORKSPACE_ROOT` | Repository to operate on. Overridden by `--root`. |
| `JADE_JSON=1` | Emit machine-readable JSON instead of plain text. |
| `JADE_TELEMETRY=0` | Disable local usage recording entirely. |
| `JADE_STATE_DIR` | Keep the telemetry log outside the workspace, one subdirectory per workspace. |
| `JADE_METALS_IMPORT=1` | Let metals import an sbt build so Scala edits get diagnostics. Runs sbt; creates `.bloop/` and `.metals/`. |

---

## Use it in a container

Jade is a child process, not a service, so the useful shape is to copy the
binary into your own image rather than run Jade's:

```dockerfile
FROM ghcr.io/julianbei/jade-mcp:v0.0.3 AS jade

FROM your-project-base
COPY --from=jade /jade-mcp /usr/local/bin/jade-mcp
ENV JADE_WORKSPACE_ROOT=/workspace
```

Or build it yourself from the included [Dockerfile](Dockerfile).

The published image is `distroless/static`, so it carries no git, no gopls and
no language toolchains. Jade detects each of those at runtime and degrades with
an explicit message rather than failing, so this still works — you get the
textual `references` fallback, and `changes`/`diff`/`history`/`checkpoint` are
off. If you want the full surface, install `git` and `gopls` in *your* image;
Jade will find them.

---

## The tools

35 tools, in four groups. Every response is plain text, shaped to lead with the
decisive line — the answer first, the supporting detail after, raw output only
when you ask for it.

**Over MCP each one is registered as `jade.<name>`** — `jade.outline`,
`jade.replace_symbol`, and so on. The tables below use the bare name for
readability. Most MCP clients show you the prefixed name already, often with
the dot rewritten (Claude Code displays `mcp__jade__jade_find`). Over the wire
Jade accepts both `jade.find` and `jade_find`.

### Inspect

| Tool | What it does |
|---|---|
| `outline` | File structure — declarations grouped by kind, without reading bodies. |
| `read_symbol` | One declaration, by name or symbol ID. |
| `read_range` | Verbatim lines, or a whole file. `ranges` reads several files or ranges in one call; an end line past the file reads to the end. |
| `find` | Locate a declaration **and** get its body in one call. `queries` finds several names at once. |
| `grep` | Literal or regex text search with path globs. The replacement for `grep -rn`. |
| `search` | Rank declarations by name similarity. Fuzzy and name-only — use `grep` for anything else. |
| `references` | Find usages. Exact from the language server when one is installed; a name-matched approximation otherwise, and it says which answered. |
| `repository_map` | Rank files against a task description, within a token budget. |
| `retrieve` | Pull a working set for a query. |
| `context` | Assemble the surrounding context for one symbol. |
| `workspace_tree` | Directory structure. |
| `search_nudge` | For harness integrators: given a shell search command the harness already ran, return index hits worth appending below it. |

Symbols are addressed as `path::Name`, or `path::Name@line` when a name is
ambiguous. An ambiguous read returns the candidates with their signatures
rather than guessing.

### Modify

| Tool | What it does |
|---|---|
| `replace_symbol` | Replace a whole declaration. Takes the full `path::Name@line` ID, or just `path::Name` when that name is unique in the file. |
| `replace_text` | Replace exact, unique text. Anchored on content, not line numbers. |
| `replace_range` | Replace a line range. |
| `replace_file` | Replace an entire file's contents. |
| `create_file` | Create a new file. |
| `delete_file` | Delete a file. |
| `delete_symbol` | Delete one declaration. |
| `rename` | Cross-file rename from the language server; refuses rather than guessing when it cannot be exact. |
| `insert` | Add text without replacing anything — a new function, a new section, an extra case. Appends with no anchor; places before or after a unique anchor with one. |
| `apply` | Several edits as one atomic unit — anchors validated up front, all applied or none, one revision bump and one validation at the end. |

Every edit returns consequences, not "success": the revision transition, which
symbols moved, immediate diagnostics, and the IDs of any background validation
it started. Edits accept an `expectedRevision` precondition; supplying it makes
a stale edit fail loudly instead of silently clobbering a concurrent change.

### Validate

| Tool | What it does |
|---|---|
| `check` | Build, typecheck or tests — discovering the repository's own command rather than assuming one: Makefile target, then npm script, cargo, Maven, Gradle, sbt, pytest/mypy or bundler, by manifest. A project it cannot identify is reported as such rather than run with the wrong toolchain. Every result names the command that ran; `dryRun` names it without running. |
| `run_tests` | Tests scoped to a file, a test name, or the changed files. |
| `run_command` | Run one of the repository's declared commands by name. |
| `declare_command` | Add or remove a declared command. |
| `job_status` | Poll a background job. |
| `job_output` | Raw output for a job, on demand. |

Exit status is authoritative. A command that prints a success-looking line and
exits non-zero fails.

### State

| Tool | What it does |
|---|---|
| `changes` | What moved — by file and by symbol, not just by path. |
| `diff` | The patch, including untracked files. `since` takes any git revision. |
| `history` | Which commits touched one symbol, via `git log -L`. |
| `checkpoint` | Mark a revertible point: snapshots the files Jade edited and records git's `HEAD`. Not a commit. |
| `revert` | Restore those files to a checkpoint. Never moves git, and refuses if a commit landed since the checkpoint. |
| `events` | The workspace event stream. |
| `telemetry` | How Jade's own tools have been used in this workspace. |

---

## Repository commands

Beyond build and test, every repository has its own verbs — lint, codegen,
migrate, release-gate — and an agent that does not know them reaches for the
shell. So Jade lets it record them instead:

```text
declare_command(name: "lint", run: "golangci-lint run ./...")
run_command(name: "lint")
```

They live in `.jade/commands.json`, which is meant to be committed. It becomes
the repository's declared command vocabulary — written once by whoever (or
whatever) worked out the incantation, replayed by name forever after. Calling
`run_command` with no name lists what the repository declares; calling it with
an unknown name answers with the commands that *do* exist, so a wrong guess
teaches rather than fails.

---

## Language support

Structure comes from tree-sitter grammars compiled into the binary, so it
works with nothing installed. Semantics come from a real language server,
which you provide — jade starts it on first use, reuses it for the session,
and shuts it down on exit.

| Language | Structure | Semantics, with this installed |
|---|---|---|
| Go | ✅ built in | `gopls` |
| TypeScript / TSX | ✅ built in | `typescript-language-server` |
| JavaScript | ✅ built in | `typescript-language-server` |
| Rust | ✅ built in | `rust-analyzer` |
| Python | ✅ built in | `pyright-langserver`, or `pylsp` / `jedi-language-server` |
| Ruby | ✅ built in | `ruby-lsp`, or `solargraph` |
| Java | ✅ built in | `jdtls` |
| Scala | ✅ built in | `metals` |
| Everything else | text scan, announced | — |

Every row is verified end-to-end by `make conformance`, which builds an image
containing all eight servers and runs jade against a real repository per
language.

Semantic requests wait for the server to finish indexing (its `$/progress`
tokens), because an indexing server answers wrongly rather than slowly. When
the primary server declines a rename, jade asks the language's installed
alternative: **ruby-lsp renames classes but not methods, so Ruby method rename
needs `solargraph` installed alongside it.** With ruby-lsp alone, method
rename refuses and repeats the server's reason.

Every edit response names what checked the file (`checked: pyright-langserver`)
or why nothing did (`not checked: app.py: pyright-langserver is not installed`).

**Scala needs one opt-in.** metals reports errors only after importing the sbt
build, and it asks permission first, because importing runs sbt and creates
`.bloop/` and `.metals/` in the repository. Jade declines unless
`JADE_METALS_IMPORT=1` is set, and says so in the edit response. A repository
an editor has already imported needs no setting.

"Structure" is outline, symbol read, edit-by-symbol, grep and search.
"Semantics" is exact `references`, cross-file `rename`, and type-level
diagnostics on edit.

Jade looks for servers on `PATH` and in the places toolchains actually install
them — `~/go/bin`, `~/.cargo/bin`, `~/.local/bin`, `~/.coursier/bin` — because
`go install` puts `gopls` somewhere that is not on `PATH` by default, and a
client that only checked `PATH` would report Go as unsupported on a machine
that has a working `gopls`.

A missing server is never an error. Jade degrades to the behaviour above and
says which answer you got.

## Design principles

1. **Structure before source.** Return the minimum sufficient representation
   first — outline before full source, summary before raw logs.
2. **Deterministic tools before model reasoning.** Jade orchestrates
   tree-sitter, git, gopls and the project's own build tooling. It does not
   reimplement them, and does not guess where they could answer.
3. **Every edit returns consequences.** Not "success" — the revision
   transition, the symbols that moved, diagnostics, and jobs started.
4. **Conclusions before logs.** The verdict leads. Raw output expands on
   request.
5. **Semantic operations before textual ones.** But textual escape hatches stay
   available, because the semantic path does not always exist.
6. **State is explicit.** Revisions, checkpoints and change sets are objects,
   not implications.
7. **Long-running work is asynchronous.** Builds and test suites return job IDs
   and stream events.
8. **An approximation must announce itself.** When Jade falls back to a text
   scan or a name-matched graph, the caveat travels *with the data*, in the
   response — not in documentation the agent will never read.
9. **Jade is model- and harness-independent.** MCP is an adapter, not the
   architecture.
10. **Measure agent outcomes, not infrastructure sophistication.** Tokens and
    turns per completed task — and token reduction is worthless if the success
    rate drops with it.

The longer design document is [docs/scope.md](docs/scope.md).

---

## What Jade does not do yet

This list is more useful than the feature list — it tells you what is worth
reporting and what is already known. What is planned is in
[ROADMAP.md](ROADMAP.md).

- **Nine languages get a real grammar; the rest fall back to a text scan.**
  Go, TypeScript, TSX, JavaScript, Python, Ruby, Java, Scala and Rust are
  parsed properly. Anything else (Kotlin, Swift, C/C++, C#, PHP, …) is served
  by a heuristic that finds some declarations and misses others — and the
  amount it misses varies enormously by language, so treat those outlines as
  a hint rather than an inventory. Jade always says which you got
  (`! no kotlin grammar — …`).
- **Semantic features need a language server installed for that language.**
  Jade speaks LSP to whatever is on the machine (see
  [Language support](#language-support)). With a server, `references` and
  `rename` are compiler-exact and cross-file. Without one, `references`
  degrades to a textual approximation that says so, and `rename` refuses
  rather than guessing — an approximate reference list is still useful to a
  reader, but an approximate edit is corruption.
- **No completion, hover or code actions.** Jade's LSP client implements what
  the tools need — references, rename, diagnostics — not the whole protocol.
- **No blame, no cross-repo work, no remote execution.**
- **Formatting runs only where it is safe.** gofmt and rustfmt always run.
  prettier (TypeScript/JavaScript), black or ruff (Python) and scalafmt run
  only when the repository declares them — its config file, and for Node and
  Python the project's own binary — because a formatter the project did not
  choose turns a one-line edit into a whole-file diff. Ruby, Java, JSON and
  Markdown are left as edited.
- **Revision tracking is Jade's own counter, not git's.** It detects concurrent
  edits within a session. It is not a VCS. A checkpoint snapshots the files Jade
  has edited and records git's `HEAD`; `revert` restores those files and
  nothing else, never moves git, and refuses once a commit has landed since the
  checkpoint — undoing committed work is git's job. Checkpoints do not survive
  a restart of the server.
- **Not hardened for untrusted input.** It runs shell commands you declare and
  edits files you point it at. Treat it as a development tool, and do not point
  it at a repository you would not run `make` in.

---

## Stability and versioning

Tool names and required arguments are frozen and enforced by a test. 0.0.3
added no tools and made two arguments optional (`query` on `find`, `path` on
`read_range`), both backward compatible. Schemas and server instructions are
still read once at connection time, so **reconnect after upgrading**.
[docs/tool-contract.md](docs/tool-contract.md) has the full surface and the
policy on what counts as a breaking change.

What is **not** frozen: response *wording*, the `.jade/*` file formats, the
exact spelling of symbol IDs, and everything under `internal/`. Treat responses
as text for a model to read, not as a format to parse. `JADE_JSON=1` gives
machine-readable output if you need to parse something.

---

## Telemetry

Jade records how its own tools are used — call counts, response sizes, timing,
and the failure classes that most often precede a caller giving up and using
the shell.

It is written to `.jade/telemetry.jsonl` in your workspace and **never
transmitted anywhere**. It records no arguments, no response bodies and no
error text — only a 10-character hash of each call's target (path, symbol or
query), so the confusion report can tell a second tool asked about the same
thing. `JADE_TELEMETRY=0` turns it off; `telemetry(reset: true)` clears it.

Jade tries not to leave files in a repository it was only asked to work in:

- In a git repository, before creating the log, Jade adds it to
  `.git/info/exclude` — the clone-local ignore file, never committed — unless
  git already ignores it. `.gitignore` is never touched. The log does not show
  up as untracked, so a harness that commits every untracked file does not
  commit it.
- `JADE_STATE_DIR=/some/dir` moves the log out of the workspace entirely, into
  a subdirectory per workspace. Use it when Jade is rooted at a checkout that
  something else commits or reviews wholesale.
- A call Jade rejects outright (an unknown tool name) never creates the log.

`.jade/commands.json` is different: it is the repository's declared command
vocabulary, meant to be committed, and is only created when you declare a
command.

It exists because response *cost* is invisible to whoever is reading the
response. Its first live reading found a tool returning 4.6KB in 704ms on a
routine call — something sixteen tasks of hand-written notes had never noticed.

---

## Reporting problems

[docs/reporting.md](docs/reporting.md) says what makes a useful report. There
are three issue templates:

- **bug** — it did the wrong thing.
- **friction** — *"I used the shell instead."* This is the valuable one.
- **feature** — it should be able to do X.

If you are unsure which, pick friction. It is the cheapest to write and the
easiest to act on, and **"it was just habit" is a real answer** — we want it.
Every shell fallback is a place Jade was not worth reaching for, and that is
the only signal that reliably improves it.

Before filing a bug, check whether your client has reconnected since the
version changed. A stale tool catalog explains a surprising share of "this tool
does not exist" and "my fix did not take effect".

---

## Development

```bash
make build      # go build ./...
make test       # go test ./...
make fmt        # gofmt -w ./cmd ./internal
make binary     # bin/jade-mcp, version-stamped
make install    # onto your PATH
```

The repository declares its own commands in `.jade/commands.json`, including
`release-gate` — build, vet, tests and a gofmt check, which is the gate a tag
has to pass. Run it the way an agent would: `run_command(name: "release-gate")`.

Layout:

| Path | What lives there |
|---|---|
| [cmd/jade-mcp](cmd/jade-mcp) | The MCP stdio server — the entry point that matters. |
| [cmd/jade](cmd/jade) | A small CLI for driving the internal API directly. |
| [cmd/jade-bench](cmd/jade-bench) | The token benchmark: Jade against equivalent shell commands. |
| [internal/workspace](internal/workspace) | Revisions, change sets, checkpoints, git. |
| [internal/code](internal/code) | Symbol index, outlines, search, grep, references. |
| [internal/edit](internal/edit) | Mutation, atomic apply, formatting. |
| [internal/diagnostics](internal/diagnostics) | Immediate feedback on edits; gopls. |
| [internal/jobs](internal/jobs) | Async job runner, command discovery. |
| [internal/languages](internal/languages) | Per-language adapters (Go, TypeScript, Rust). |
| [internal/commands](internal/commands) | The declared-command registry. |
| [internal/telemetry](internal/telemetry) | Local usage recording. |
| [internal/transport](internal/transport) | MCP adapter, and the transport-independent internal API. |
| [internal/protocol](internal/protocol) | Shared request and response types. |

Contributions are welcome. The one hard rule is principle 8: if you add a code
path that approximates, the response has to say so.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE). Copyright 2026 Julian Amelung.
