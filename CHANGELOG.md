# Changelog

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
repository (droneship-next, "is the CAS store's write atomic?") came out at
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

[feedback.md](feedback.md) is the same log kept from the inside across
development. Its blunt finding: the fallbacks that lasted longest closed within
two tasks of being *written down*, not when the tool shipped. `check` existed,
worked, and kept losing to `go test` for three tasks. Naming it fixed it.
