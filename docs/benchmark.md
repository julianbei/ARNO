# External benchmark

Does an agent with Jade get more done, at acceptable cost, than the same agent
with a shell? The release plan's Phase 2 asks this of repositories Jade was
not built in. `jade-bench agent` runs it.

## Method

Each task runs once per arm, per repeat, in a fresh clone of the repository at
a pinned commit:

| Arm | Built-in tools | Jade MCP server |
|---|---|---|
| `shell` | all | no |
| `shell-lean` | Bash, Read, Edit, Write | no |
| `jade` | none | yes |
| `jade+shell` | all | yes |

The agent is Claude Code headless. Every arm gets the same invocation except
tools and MCP:

```sh
claude -p "<task prompt>" --output-format json --no-session-persistence \
  --permission-mode bypassPermissions --setting-sources project \
  --strict-mcp-config --max-budget-usd <cap> --model sonnet \
  --tools default|"Bash,Read,Edit,Write"|"" [--mcp-config <jade only>]
```

`shell-lean` exists because the tool list is resent on every turn. The pilot
found that Claude Code's full built-in list is 38k tokens of prompt per turn and
Jade's core profile 14k, so a comparison against `shell` alone cannot say how
much of Jade's saving is its tools and how much is a shorter list.

`--strict-mcp-config` keeps the operator's own MCP servers out, and
`--setting-sources project` keeps user-level hooks and plugins out, so the
arms differ in exactly one thing. Arms are interleaved per task so drift over a
long run falls on all of them.

**Success is the task's verify command**, run after the agent stops: exit 0
passes. What the agent says about its work is not read.

Each run's workspace is a clone at the task's commit with its **history
replaced by one baseline commit**: tasks come from real fixes, and an agent
running `git log --all` would otherwise find the answer. The repository's
`setup` command (installing dependencies) runs before that baseline commit, so
its output is never counted as the agent's change. **Hidden tests** — a task's
`verifyFiles` — are written only after the agent stops and the diff has been
measured, overwriting any file of the same name.

Recorded per run, one JSON line each: success, verify output, cost, turns,
input/output/cache tokens, duration, permission denials, lines changed and
files changed (new files included).

## Scorecard

Fixed on 2026-09-13, before any run. Not changed after results are seen.

- **Non-negotiable:** an arm's task success is at least the `shell` arm's.
- **Tradeable:** its mean tokens may exceed the `shell` arm's by 10% for every
  full 5 points of success gained. Equal success allows no extra tokens.
- Invalid and unintended edits are also non-negotiable in the plan. They are
  not scored yet; lines and files changed are reported so they can be read.

Pilot: Sonnet 5, at most $50 in total. The harness charges a run that printed
no result its full per-run cap, so the budget cannot be overrun by crashes.

## Adding a repository

A task file, no code:

```json
{
  "repository": {
    "name": "cobra",
    "language": "go",
    "url": "https://github.com/spf13/cobra",
    "commit": "<sha>",
    "setup": "go mod download"
  },
  "tasks": [
    {
      "id": "flag-default-in-help",
      "prompt": "The help output omits defaults for duration flags. Fix it and add a test.",
      "commit": "<parent of the fix>",
      "verify": "go test -run TestDurationDefaultInHelp .",
      "verifyFrom": "<the fix commit>",
      "verifyPaths": ["command_test.go"],
      "timeoutSeconds": 900
    }
  ]
}
```

`verifyPaths` are read at `verifyFrom` from the source checkout when the run
starts, so task files never copy a repository's code; `verifyFiles` gives
contents inline instead, for tests that exist in no commit.

A verify command must fail before the task is done and pass after, or the
task measures nothing. Check both by hand when adding one.

## Running

```sh
git clone https://github.com/spf13/cobra /bench/cobra
jade-bench agent -tasks tasks/cobra.json -repo /bench/cobra \
  -model sonnet -budget 50 -per-run 2 -repeats 1 -out results.jsonl
```

**Agents run with permissions bypassed**, so `jade-bench agent` refuses to run
unless `JADE_BENCH_SANDBOX=1` is set (the benchmark container) or `-allow-host`
is passed. Only use `-allow-host` on a machine where an agent running arbitrary
shell commands can do no harm.

The report prints per arm: runs, success rate, mean tokens, turns, cost,
seconds, lines and files changed, agent errors — then each Jade arm's
scorecard verdict against `shell`.
