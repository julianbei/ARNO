# Jade — Just Agentic Development Environment

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="logo-dark.png">
  <img src="logo.png" alt="Jade — Just Agentic Development Environment" width="520">
</picture>

## The IDE for agents

Jade gives coding agents what an IDE gives you, served over MCP. Read by symbol,
edit against a known revision, get the compiler's diagnostics back with the
edit, validate with the repository's own commands, see what changed, and
revert to a checkpoint — each step one tool call, none of it through the shell.

Symbol-aware reading is how Jade finds its way around; the transaction is what
it is for. Where the shell is still the better tool, use it — the question
Jade has to answer is whether an agent gets more done, at acceptable cost,
with it than without ([docs/benchmark.md](docs/benchmark.md)).

**Half the tokens, more issues fixed.** Claude Code on 12 real closed issues
from cobra (Go), ky (TypeScript), requests (Python) and ripgrep (Rust), judged
by each upstream fix's own hidden tests:

| Claude Code with… | Issues fixed | Tokens per task | Time per task |
|---|---|---|---|
| its built-in tools | 10 of 12 | 1.38M | 215s |
| **Jade in their place** | **12 of 12** | **0.68M (−51%)** | **163s (−24%)** |

Not just a shorter tool list: against a shell trimmed to Bash, Read, Edit and
Write, Jade still used 18–25% fewer tokens and fewer turns, in two separate
runs. Sonnet 5, one run per task, four languages —
[results and caveats](docs/benchmark-results.md) ·
[how to set it up](#let-jade-replace-the-built-in-tools).

**Status:** 0.0.10 — early, usable, and looking for feedback. **Testing it?**
Start with [the tester guide](#trying-jade-a-guide-for-testers).

```bash
curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | sh
```

For macOS and Linux — Windows isn't supported ([here's why, and where to upvote](https://github.com/julianbei/jade/issues/2)).
Run it again to update. [Other ways to install](#other-ways-to-install).

---

## Table of contents

- [Trying Jade: a guide for testers](#trying-jade-a-guide-for-testers)
- [Why Jade exists](#why-jade-exists)
- [Install](#install)
- [Configure your MCP client](#configure-your-mcp-client)
- [Use it in a container](#use-it-in-a-container)
- [The tools](#the-tools)
- [Repository commands](#repository-commands)
- [Language support](#language-support)
- [Design principles](#design-principles)
- [When the shell is still the right tool](#when-the-shell-is-still-the-right-tool)
- [What Jade does not do yet](#what-jade-does-not-do-yet)
- [Stability and versioning](#stability-and-versioning)
- [Telemetry](#telemetry)
- [Reporting problems](#reporting-problems)
- [Development](#development)

---

## Trying Jade: a guide for testers

Thanks for testing. Half an hour gets you set up; the useful part is a week or
two of your normal work with it switched on, and then telling us how it went —
**including if you turned it off.**

### 1. Install Jade and your language servers

**macOS and Linux:**

```bash
curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | sh
```

**Windows** isn't supported, sorry! If you'd like it to be, please 👍
[issue #2](https://github.com/julianbei/jade/issues/2) or tell us there why it
matters to you. (WSL 2 runs Linux, so the Linux build may work there, but we
don't test it or take bug reports for it.)

The script picks the build for your OS and CPU, checks it against the
release's checksums and installs it to `/usr/local/bin`, or `~/.local/bin`
when that is not writable — no sudo, no Go toolchain. If that directory is
not on `PATH`, it offers to add it to your shell profile, and it prints the
absolute path to use as `command` in your MCP client config. On a first install it then opens
`jade-mcp install`, a menu that installs the language servers you pick, or
shows how to install them by hand; Enter skips it, and you can run it again
any time. **Run the same command again to
update** — Jade tells you when a new release is out (see
[Update check](#update-check)). (Prefer Go? `go install
github.com/julianbei/jade/cmd/jade-mcp@v0.0.10` works too.) Jade reads
structure in every language with nothing else installed; exact references,
cross-file rename and type errors on edit need the language's server —
`jade-mcp install` installs these for you, or by hand:

| Language | Install | Notes |
|---|---|---|
| Go | `go install golang.org/x/tools/gopls@latest` | First answer about 1.6s. |
| Java | `brew install jdtls` (needs JDK 21+), or your distro's package | First answer about 8.5s while it indexes. `check` runs `mvn` or `gradle`; with only the Gradle wrapper, have the agent declare `./gradlew build` once (step 4). |
| Scala | `cs install metals` ([coursier](https://get-coursier.io)) | Set `JADE_METALS_IMPORT=1` so metals may import the sbt build (it creates `.bloop/` and `.metals/`). First answer about 22s. |
| Kotlin | — | No grammar or server yet: text search only. Tell us if you need it. |

A missing server is never an error: Jade says which answers are approximate.

### 2. Point your agent at a real repository

For **Claude Code**, put this in `.mcp.json` at the root of the repository you
work in (other hosts: [Codex CLI, goose, OpenCode](#other-hosts)):

```json
{
  "mcpServers": {
    "jade": {
      "type": "stdio",
      "command": "jade-mcp",
      "args": ["--root", "/absolute/path/to/the/repo", "--tools", "core"],
      "alwaysLoad": true
    }
  }
}
```

That keeps Claude Code's own tools too. For the clearest signal, run some
sessions with Jade **in place of** them: `claude --tools ""` with the same
config ([why and trade-offs](#let-jade-replace-the-built-in-tools)). Restart
or reconnect the client (`/mcp`) after installing or upgrading Jade.

### 3. Check it came up

Ask the agent: *"call jade.capabilities"*. You should see your languages, each
with `server … (not started)` or `no server (… not installed)`, plus the build
and test commands Jade found (`mvn`, `gradle`, `sbt`, `go test`). If a server
you installed shows as not installed, that is a bug report.

### 4. What to try

Work as you normally would. If you want a checklist for the first sessions:

- **Find and follow code:** "where is X declared, and who calls it?" — `find`,
  `references`.
- **Rename across files:** a method or class used in several files — `rename`
  (exact with gopls, jdtls or metals running).
- **A change in several places at once:** "change the signature and update the
  callers, then check it builds" — `apply` with `check`.
- **Run the tests that matter:** `run_tests` with a file or test name, or `apply`
  with `check: "impact"`.
- **Repeatable commands:** have the agent `declare_command` something you run
  often (`./gradlew :core:test`, `sbt "testOnly *ParserSpec"`); later sessions
  reuse it from `.jade/commands.json`.
- **Undo:** `checkpoint` before something risky, `revert` if it goes wrong.

### 5. Tell us how it went

| When | File this |
|---|---|
| After a week or two — or when you turn Jade off | [**Feedback**](https://github.com/julianbei/jade/issues/new?template=feedback.yml) |
| The agent used the shell although a Jade tool existed | [Friction](https://github.com/julianbei/jade/issues/new?template=friction.yml) |
| A tool gave a wrong answer or failed | [Bug](https://github.com/julianbei/jade/issues/new?template=bug.yml) |
| Something you wish Jade did | [Feature wish](https://github.com/julianbei/jade/issues/new?template=feature.yml) |

The templates ask for the output of `jade.capabilities` and, optionally,
`jade.telemetry`. Neither contains source code; telemetry is
[local only](#telemetry) and records no arguments or response text, so both are
safe to paste from a private repository.

**Known rough edges on the JVM:** no formatter runs for Java or Scala files;
large Gradle builds can make jdtls's first answer much slower than 8.5s; Kotlin
has no support yet.

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
predicts outside use. The 0.0.4 pilot on four outside repositories answers
it at a larger scale: used in place of Claude Code's built-in tools, Jade
solved 12 of 12 real issues with 51% fewer tokens, and 18–25% fewer than a
shell trimmed to four tools
([docs/benchmark-results.md](docs/benchmark-results.md)). Jade is not finished.

Jade's own development log ([docs/feedback.md](docs/feedback.md)) records every
time its author reached for bash instead, and why. The pattern it found was
blunt: the fallbacks that survived longest each closed within two tasks of
being *named in the log* — not when the tool shipped.

---

## Install

### With the install script

```bash
curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | sh
```

Downloads the latest [release binary](#from-a-release-binary) for your OS and
CPU, verifies it against `checksums.txt`, and installs it without sudo to
`/usr/local/bin` or `~/.local/bin`. Run it again to update: a `jade-mcp`
already on `PATH` is replaced where it is, and nothing is downloaded when it
is already current. If the directory is not on `PATH`, it offers to add it to
your shell profile (`JADE_ADD_TO_PATH=1` does it without asking) and prints
the absolute path to use in your MCP client config. `JADE_VERSION=v0.0.10` pins a release;
`JADE_INSTALL_DIR` picks the directory. Read it first if you like:
[install.sh](install.sh).

**Windows** isn't supported — see [issue #2](https://github.com/julianbei/jade/issues/2),
and give it a 👍 if you'd like that to change.

### Language servers: `jade-mcp install`

```bash
jade-mcp install                           # menu: pick what to install
jade-mcp install --list                    # what is installed, and how the rest would be
jade-mcp install --servers go,java,scala   # install these, no questions
jade-mcp install --all                     # every missing server this machine can install
```

The menu lists each language server Jade can use, whether it is installed, and
the exact command it would run — `go install` for gopls, `brew install jdtls`,
`cs install metals`, `npm install -g` for TypeScript and Pyright, `rustup
component add rust-analyzer`, `gem install ruby-lsp` — and confirms before
running anything. A server with no installer on the machine gets instructions
for installing it by hand. The install script opens the menu after a first
install; `JADE_SKIP_SETUP=1` skips it.

**For an agent, or any script** — there is no terminal to answer a menu, so the
same steps come without questions:

```bash
# install Jade and chosen servers in one go
curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | JADE_SERVERS=go,java sh

jade-mcp install --list --json                   # state of every server, as JSON
jade-mcp install --servers java,scala --dry-run  # the commands, not run
jade-mcp install --servers java,scala            # run them
```

`--list --json` gives each server's `key`, whether it is `installed` and
where, the `command` that would install it on this machine, and `manual`
steps when there is none. `jade.capabilities` ends a missing server's line
with the command that installs it. Installing is deliberately not an MCP
tool: global package installs go through the agent's shell, where you approve
them.

### Other ways to install

The install script above is the easiest way. These work too.

#### From Go

```bash
go install github.com/julianbei/jade/cmd/jade-mcp@latest    # newest
go install github.com/julianbei/jade/cmd/jade-mcp@v0.0.10   # pinned
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

#### From a release binary

Prebuilt binaries for linux and darwin on amd64 and arm64 are attached to each
[GitHub release](https://github.com/julianbei/jade/releases), with a
`checksums.txt` alongside them. Each is built natively on its own platform —
Jade links tree-sitter through cgo, so the linux builds need a reasonably
current glibc. On an older distro, build from source or use the container
image, which is statically linked against musl.

```bash
VERSION=v0.0.10
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -fsSL "https://github.com/julianbei/jade/releases/download/${VERSION}/jade-mcp_${VERSION}_${OS}_${ARCH}.tar.gz" \
  | tar xz
sudo mv "jade-mcp_${VERSION}_${OS}_${ARCH}" /usr/local/bin/jade-mcp
```

#### From source

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
jade-mcp install --servers go      # or: go install golang.org/x/tools/gopls@latest
```

The same goes for every language below: `jade-mcp install` shows which servers
are installed and installs the rest ([Language servers](#language-servers-jade-mcp-install)).

---

## Configure your MCP client

Jade is a stdio MCP server. Point your client at the binary:

```json
{
  "mcpServers": {
    "jade": {
      "type": "stdio",
      "command": "jade-mcp",
      "alwaysLoad": true,
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

### Let Jade replace the built-in tools

Jade saves tokens when it *replaces* the agent's own tools, not when it is
added next to them. Every turn resends the whole tool list, and in Claude Code
the built-in tools are about 38k tokens of it. In Jade's pilot benchmark (12 real
issues in cobra, ky, requests and ripgrep, one run each):

| Tools | Tasks solved | Tokens per run | Time per run |
|---|---|---|---|
| Claude Code's built-in tools | 10 of 12 | 1.38M | 215s |
| Built-in tools trimmed to Bash, Read, Edit, Write | 10 of 12 | 0.90M | 213s |
| Jade only, core profile | 12 of 12 | 0.68M | 163s |
| Both, all built-in tools and Jade | 11 of 12 | 1.50M | 191s |

Trimming the built-in list is most of the saving on its own. Jade on top of
that used 25% fewer tokens and 24% less time than the trimmed shell, and solved
the two tasks both shell setups failed. Given both Jade and every built-in
tool, the agent used Bash for four calls in five and paid for both lists.
To run Jade in place of the built-in tools:

```sh
claude --tools "" --mcp-config jade.json
```

with `jade.json` passing the core profile, which lists twelve tools — read,
edit, validate, and run the commands a repository declares — and keeps the
others callable:

```json
{
  "mcpServers": {
    "jade": {
      "type": "stdio",
      "command": "jade-mcp",
      "args": ["--root", "/absolute/path/to/the/repo", "--tools", "core"],
      "alwaysLoad": true
    }
  }
}
```

The trade-off is real: without Bash the agent cannot run arbitrary commands.
`run_tests`, `check` and repository commands declared with `declare_command`
cover building, testing and repeatable scripts — a declaration lives in
`.jade/commands.json`, so later sessions reuse it; a task that needs git operations, network access
or ad-hoc scripts needs the shell back. The numbers above are one run per task
— see [docs/benchmark-results.md](docs/benchmark-results.md) for the results,
a rerun after the pilot's fixes, and the caveats.

### Other hosts

Verified with a live session — find a declaration, insert beside it, run
`check` — using only Jade's tools:

**Codex CLI** (0.154), in `~/.codex/config.toml`:

```toml
[mcp_servers.jade]
command = "jade-mcp"
args = ["--root", "/absolute/path/to/the/repo", "--tools", "core"]
```

Codex asks before every MCP tool call. With `approval_policy = "never"` it
refuses them outright; run `codex exec --approve-for-me` or approve Jade's
tools interactively.

**goose** (1.50), for one run:

```sh
goose run --with-extension "jade:jade-mcp --root /absolute/path/to/the/repo --tools core" -t "…"
```

or permanently with `goose configure` → *Add Extension* → *Command-line
Extension*, command `jade-mcp --root /absolute/path/to/the/repo --tools core`.
Verified through goose's `claude-code` provider.

**OpenCode** (1.18), in `opencode.json` at the repository root or in
`~/.config/opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "jade": {
      "type": "local",
      "command": ["jade-mcp", "--root", "/absolute/path/to/the/repo", "--tools", "core"],
      "enabled": true
    }
  }
}
```

OpenCode prefixes tools with the server name, so they appear as
`jade_jade_find` and so on. Verified with the `github-copilot` provider
(Claude Sonnet 5).

Cline and Gemini CLI are not verified yet.

### Three things that will confuse you once

**Without `"alwaysLoad": true`, Claude Code may never use Jade.** Claude Code
hides MCP tools behind a tool search by default: the agent sees their names but
not their definitions, and has to search before it can call one. With its own
shell and file tools right there, it does not. In Jade's benchmark, an agent
given both Jade and the shell made no Jade call in three of three runs; the
same setup with `alwaysLoad` called Jade directly. Other hosts may have their
own equivalent — check that Jade's tools are actually being called.

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
| `JADE_UPDATE_CHECK=0` | Turn off the daily check for a newer release ([Update check](#update-check)). |

The [install script](#with-the-install-script) reads its own:

| Variable | Effect |
|---|---|
| `JADE_VERSION` | Release to install, e.g. `v0.0.10`. Default: the latest. |
| `JADE_INSTALL_DIR` | Where to put `jade-mcp`. Default: the directory of the `jade-mcp` already on `PATH`, else `/usr/local/bin` if writable, else `~/.local/bin`. |
| `JADE_SERVERS` | Language servers to install afterwards without a menu: `go,java,scala,typescript,python,rust,ruby`, or `all`. |
| `JADE_SKIP_SETUP=1` | Skip the language-server step. |
| `JADE_ADD_TO_PATH=1` | Add the install directory to the shell profile without asking. |
| `JADE_RELEASE_URL` | Base URL of the releases, for a mirror. |

---

## Use it in a container

Jade is a child process, not a service, so the useful shape is to copy the
binary into your own image rather than run Jade's:

```dockerfile
FROM ghcr.io/julianbei/jade-mcp:v0.0.10 AS jade

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

31 tools, in four groups. Every response is plain text, shaped to lead with the
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
| `capabilities` | What Jade can do in this workspace: per language, grammar or text scan, language server state, formatter; git, validation commands, declared commands. Call it first. |
| `outline` | File structure — declarations grouped by kind, without reading bodies. |
| `read_range` | Verbatim lines, or a whole file. `lines: "280-400"` picks a range; `ranges` reads several files or ranges in one call; an end line past the file reads to the end. `dep:<name>/<path>` reads a dependency's source, read-only, at the locked version — `grep` and `find` take `dependency` to search it. |
| `find` | Locate a declaration **and** get its body in one call. `queries` finds several names at once. |
| `grep` | Literal or regex text search with path globs. The replacement for `grep -rn`. |
| `references` | Find usages. Exact from the language server when one is installed; a name-matched approximation otherwise, and it says which answered. |
| `retrieve` | Pull a working set for a query. |
| `context` | Assemble the surrounding context for one symbol. |
| `workspace_tree` | Directory structure. |

Symbols are addressed as `path::Name`, or `path::Name@line` when a name is
ambiguous. An ambiguous read returns the candidates with their signatures
rather than guessing.

### Modify

| Tool | What it does |
|---|---|
| `replace_symbol` | Replace a whole declaration. Takes the full `path::Name@line` ID, or just `path::Name` when that name is unique in the file. |
| `replace_text` | Replace exact, unique text. Anchored on content, not line numbers. Like every text edit, returns the edited region as it now reads. |
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

## Project configuration

Discovery guesses how to build and test a repository from its manifests, and the
guess is sometimes wrong: a Makefile's `python -m pytest` picks the system
interpreter, `npm run test` runs lint and browser suites for a one-file check,
and a Go module with a TypeScript app beside it has two answers to "build". A
committed `.jade/project.json` states the answer once, the way an editor keeps
its settings in `.vscode/`:

```json
{
  "areas": [
    { "path": ".", "language": "go",
      "build": "go build ./...", "test": "go test ./...",
      "testName": "go test -run {name} ./..." },
    { "path": "web", "language": "typescript",
      "typecheck": "node_modules/.bin/tsc --noEmit",
      "test": "node_modules/.bin/vitest run",
      "testFile": "node_modules/.bin/vitest run {file}",
      "testName": "node_modules/.bin/vitest run {file} -t {name}" }
  ],
  "env": { "python": ".venv/bin/python", "vars": { "CI": "1" } },
  "generated": ["web/dist", "*.pb.go"],
  "notes": "Browser tests need Playwright; run unit tests by file."
}
```

- **Areas** are parts of the repository with their own language and commands,
  each run inside its path. `check` runs a kind in every area that declares
  it; `run_tests` with a file uses the deepest area containing that file, with
  `{file}` relative to the area and `{name}` the test name.
- **Empty fields fall back to discovery**, so a config can state only what
  discovery gets wrong. A config that does not parse, names an unknown field
  or a path outside the workspace fails the check instead of being ignored.
- **`env`** puts the interpreter's directory first on `PATH` and adds the
  variables to every command. **`generated`** paths are skipped by `grep`,
  `find` and the workspace tree. **`notes`**, with the list of areas, is sent
  to the agent when a session starts.
- `check` with `dryRun` says when a command comes `from .jade/project.json`.

`jade-mcp init --root /path/to/repo` drafts the file from discovery, one area,
for you to review and commit; it never overwrites an existing one.

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

### A validation chain

Repository rules — Semgrep, a custom linter, a licence check — belong in
validation, and they need no integration in Jade. Declare one command that
runs the steps in order, joined with `&&`:

```json
{
  "validate": {
    "run": "go test ./... && semgrep scan --config .semgrep.yml --error",
    "description": "tests, then repository rules"
  }
}
```

or, without editing the file, `declare_command(name: "validate", run: "…")`.

`run_command(name: "validate")` runs it inside Jade, so the run is part of the
session's record. Exit status decides: a rule that fails fails the run, the
steps after it do not run, and the summary leads with the failing output.
Use `semgrep scan --error` or the equivalent flag of your tool — a tool that
prints findings and exits 0 passes.

---

## Language support

Structure comes from tree-sitter grammars compiled into the binary, so it
works with nothing installed. Semantics come from a real language server,
which you provide — `jade-mcp install` installs it for you — and jade starts
it on first use, reuses it for the session, and shuts it down on exit.

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
3. **Every edit has a precondition and returns consequences.** An edit can
   name the revision it expects and is refused if Jade's revision has moved;
   it returns the revision transition, what changed and the diagnostics — not
   "success". Changes made outside Jade do not yet move the revision (release
   plan Phase 5).
4. **Conclusions before logs.** The verdict leads. Raw output expands on
   request.
5. **Semantic operations before textual ones.** But textual escape hatches stay
   available, because the semantic path does not always exist.
6. **State is explicit.** Revisions, checkpoints and change sets are objects,
   not implications.
7. **Validation waits by default, backgrounds on request.** `check`,
   `run_tests` and `run_command` return the verdict; a long run can return a
   job to poll instead.
8. **An approximation must announce itself.** When Jade falls back to a text
   scan or a name-matched graph, the caveat travels *with the data*, in the
   response — not in documentation the agent will never read.
9. **Jade is model- and harness-independent.** MCP is an adapter, not the
   architecture.
10. **Measure agent outcomes, not infrastructure sophistication.** Tokens and
    turns per completed task — and token reduction is worthless if the success
    rate drops with it.
11. **Repository-native execution.** Builds, tests and lint run through the
    repository's own commands — discovered, or declared in
    `.jade/commands.json` — inside Jade, so validation is part of the record
    instead of a shell side trip.
12. **Cheaper than the escape hatch.** If the shell is easier, faster and
    cheaper for a workflow, Jade has failed that workflow. The benchmark, not
    opinion, says which ([docs/benchmark.md](docs/benchmark.md)).

The longer design document is [docs/scope.md](docs/scope.md).

---

## When the shell is still the right tool

Jade does not try to match the shell's composability. Using the shell is a
decision, not a leak, when the work is one of these:

- **Git operations**: commit, branch, rebase, push, blame. Jade reads git
  state (`changes`, `diff`, `history`) and never moves it.
- **One-off probes**: `curl` a local server, inspect a process, check a port,
  read an environment variable.
- **Debugging a script or a build system itself**, where the question is
  what a shell command does rather than what the code says.
- **Installing dependencies and toolchains**: `npm install`, `go install`,
  `pip install`.
- **Network access** of any kind.

What stays on Jade's side of the line, even though a shell could do it:

- **Builds, typechecks, tests, lint and codegen.** Run them with `check`,
  `run_tests` or a declared command (`declare_command`, then `run_command`).
  Validation run from the shell is validation the change transaction cannot
  see: no verdict in the edit record, no scoped test runner, no failure
  summary.
- **Reading and searching code**, and **editing it**. That is where Jade's
  revisions, diagnostics and provenance apply.

A command you keep running from the shell for validation belongs in
`.jade/commands.json`, or in `.jade/project.json` as an area's build or test
command.

---

## What Jade does not do yet

**Windows.** Jade is built and tested for macOS and Linux only, and we'd
rather do those two really well than three halfway. Until further notice we
don't build, test or look at Windows. If you'd like Jade on Windows, please 👍
[issue #2](https://github.com/julianbei/jade/issues/2) — and if you think
this is the wrong call, say so there; honest feedback is welcome. WSL 2 runs
Linux, so the Linux build may work there, but it isn't tested.

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
  it at a repository you would not run `make` in. What running Jade inside a
  sandbox or container does and does not cover:
  - **Covered by Jade itself:** reads and writes stay inside the workspace root,
    symlinks included; dependency sources are read-only; a repository cannot
    make Jade launch a binary it ships (declared commands run through the
    shell you already trust, and a `.jade/project.json` interpreter is a path
    you review in the diff).
  - **Covered only by the sandbox:** what a declared command, a Makefile
    target, an npm script or a test suite does when `check`, `run_tests` or
    `run_command` runs it — network access, files outside the workspace,
    credentials in the environment. Jade runs the repository's own commands
    with your environment; a malicious repository's `make test` is as
    dangerous under Jade as in your shell.
  - **Not covered at all:** an agent asked to declare a harmful command, and
    language servers, which execute project configuration of their own
    (build scripts, plugins) when they index a workspace.

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

### Update check

Separate from telemetry, Jade looks up the newest release tag on GitHub — one
unauthenticated request for `releases/latest`, carrying nothing about your
workspace or how you use Jade — at most once a day, in the background, with a
three-second timeout. A failed or offline check also waits a day. When a newer
release exists, it says so only where you asked what you are running:

```
$ jade-mcp --version
jade-mcp v0.0.10
update available: v0.0.11 (running v0.0.10) · curl -fsSL https://raw.githubusercontent.com/julianbei/jade/main/install.sh | sh, then reconnect your MCP client
```

and as the second line of `jade.capabilities`. It never appears in the server
instructions or in other tool responses. `JADE_UPDATE_CHECK=0` turns it off;
it is also off in CI (`CI` set) and for development builds.

It exists because response *cost* is invisible to whoever is reading the
response. Its first live reading found a tool returning 4.6KB in 704ms on a
routine call — something sixteen tasks of hand-written notes had never noticed.

---

## Reporting problems

[docs/reporting.md](docs/reporting.md) says what makes a useful report. There
are four issue templates:

- **feedback** — how it went after some real use, or why you turned it off.
- **bug** — it did the wrong thing.
- **friction** — *"I used the shell instead."* This is the valuable one.
- **feature wish** — it should be able to do X.

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
