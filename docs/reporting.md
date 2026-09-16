# What makes a useful ARNO report

Short version: **the tool call, what came back, and what you expected.** ARNO's
responses are plain text and usually a few lines, so pasting them verbatim
costs you almost nothing and saves a round trip.

## Check this first

**Did you reconnect your MCP client?** The tool catalog is fixed at connection
time, so a newly added tool does not appear, and a newly built binary is not
used, until the client reconnects. During arno's own development this accounted
for every single "the tool is missing" report — without exception.

## The report we most want

Not bugs. **Friction**: the moments you reached for `grep`, `sed` or `cat`
even though ARNO was right there.

ARNO's premise is that an agent falling back to the shell is operating outside
any tooling you control — no revision tracking, no guardrails, no telemetry.
So a fallback is a design failure whether or not ARNO had a working tool for
the job.

**"It was just habit" is a real answer and we want it.** It means the ARNO path
was not the obvious one at the moment of choosing, which is our problem. This
project's own development log ([feedback.md](feedback.md)) records sixteen
tasks of exactly these, and the pattern it found was blunt: the fallbacks that
lasted longest closed within two tasks of being *written down*, not when the
tool shipped. `check` existed, worked, and kept losing to `go test` for three
tasks. Naming it fixed it.

So please report the boring ones.

## What to include

**Always:**

- `arno-mcp --version`
- The tool call — name and arguments
- The response, verbatim
- What you expected instead

**Often decisive:**

- **The language of the file.** ARNO has real tree-sitter grammars for Go,
  TypeScript, TSX and Rust. Everything else falls back to a text scan that
  finds some declarations and misses others. ARNO says so in the response
  (`! no python grammar — …`), but if it did not, that itself is a bug.
- **Whether `gopls` is installed**, for anything involving `references` or
  `rename`. Without it, `references` degrades to a textual approximation and
  `rename` refuses outright rather than guessing.
- **Whether the repo is a git repository with at least one commit.** ARNO works
  without either, but `changes`, `diff`, `history` and `checkpoint` are limited
  and will say so.

**Very helpful, and safe to paste:**

Output of the `arno.telemetry` tool. It records **no arguments, no response
bodies and no error message text** — only tool names, wall time, response sizes
and failure classes. That is deliberate: a telemetry file containing source
code could not be attached to an issue, which would defeat the point of
collecting it. It is local-only and never transmitted; `ARNO_TELEMETRY=0`
disables it entirely.

The line worth pasting is the fallback summary:

```
23 calls across 8 tools, 1 errors
fallback risk: not_found 1
```

Those failure classes — `not_found`, `ambiguous`, `stale_revision`, `timeout`,
`unavailable` — are the moments ARNO failed and the shell was one keystroke
away. They are the most direct evidence we can get.

## What not to bother with

- **Exact response wording.** It is not frozen and will change. Report that an
  answer was *wrong* or *unclear*, not that it was phrased differently than
  last week. See [tool-contract.md](tool-contract.md) for what is and is not
  stable.
- **Known limitations from the README's "What ARNO does not do yet".** Unless
  you hit one in real work, in which case say so — that is priority
  information, and worth more than a feature request.
- **Polishing the report.** A one-line "this made me use grep and I don't know
  why" is worth more than a well-written issue that never gets filed.

## Where

GitHub issues, using one of the three templates: **bug**, **friction**, or
**feature**. If you are unsure which, pick friction; it is the cheapest to
write and the easiest for us to act on.

---

*Note on filenames: this file is `docs/reporting.md` rather than `FEEDBACK.md`
because the repository already contains `docs/feedback.md` — the internal
bash-fallback log — and on a case-insensitive filesystem, which is the macOS
default, the two are the same file. Creating the second would have silently
destroyed the first.*
