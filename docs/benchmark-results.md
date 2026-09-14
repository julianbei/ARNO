# Benchmark results: the 0.0.4 pilot

Four outside repositories, three real closed issues each, run with
`jade-bench agent` as described in [benchmark.md](benchmark.md). Claude Code
headless on Sonnet 5, one run per task per arm, $1.50 cap per run. Recorded
2026-09-13.

| Repository | Language | Tasks |
|---|---|---|
| [cobra](https://github.com/spf13/cobra) | Go | completion mutates `os.Args`; plugin version flag display name; `--help`/`--version` with completion and disabled flag parsing |
| [ky](https://github.com/sindresorhus/ky) | TypeScript | download progress drops response metadata; mixed-case method not uppercased; `parseJson` ignored by `clone` |
| [requests](https://github.com/psf/requests) | Python | content-type parameter without a value; preserve a double slash in the path; redirect history references itself |
| [ripgrep](https://github.com/BurntSushi/ripgrep) | Rust | hidden whitelist for a dot path; null data with line regexp; replace with multiline lookaround panics |

## Suite

Jade at `a8fd3e9`. Means per run over 12 tasks.

| Arm | Solved | Tokens | Turns | Time | Cost | Scorecard vs `shell` |
|---|---|---|---|---|---|---|
| `shell` — all 27 built-in tools | 10 of 12 | 1.38M | 25.3 | 215s | $0.82 | — |
| `shell-lean` — Bash, Read, Edit, Write | 10 of 12 | 0.90M | 25.8 | 213s | $0.66 | pass (0.66x tokens) |
| `jade` — core profile, no built-in tools | 12 of 12 | 0.68M | 23.0 | 163s | $0.52 | pass (0.49x tokens, +17 points) |
| `jade+shell` — both, 39 tools | 11 of 12 | 1.50M | 25.8 | 191s | $0.84 | pass (1.09x tokens, +8 points) |

Tokens per run, Jade alone against each shell arm:

| Repository | vs `shell` | vs `shell-lean` |
|---|---|---|
| cobra | −45% | −25% |
| ky | −70% | −22% |
| requests | −70% | +15% |
| ripgrep | −25% | −31% |
| all | −51% | −25% |

Both shell arms failed ky's download-progress task and ripgrep's null-data
task; Jade alone solved both. `jade+shell` failed the ky task.

## Rerun after the pilot's fixes

Jade at `4c7694c`: failed-call fixes, dependency source reads, tree-sitter
syntax checks and `.jade/project.json`. Only `jade` and `shell-lean`, same
tasks and cap.

| Arm | Solved | Tokens | Turns | Time | Failed Jade calls |
|---|---|---|---|---|---|
| `shell-lean` | 11 of 12 | 0.99M | 29.0 | 200s | — |
| `jade` | 11 of 12 | 0.82M | 24.8 | 153s | 0.42 per run (suite: 0.67) |

Jade alone against `shell-lean`: −18% tokens, −14% turns, −24% time. By
repository: cobra −18%, ky −31%, requests −58%, ripgrep −2%. Both failed ky's
download-progress task. The suite's requests result reversed, so it was noise.

## Rerun at 0.0.7

Jade at `b4921b2` (v0.0.7): response budgets, provenance, preconditions,
impact checks and the deprecation of five overlapping tools. Only `jade` and
`shell-lean`, same tasks and cap, $14.85 in total. Recorded 2026-09-14.

| Arm | Solved | Tokens | Turns | Time | Cost |
|---|---|---|---|---|---|
| `shell-lean` | 10 of 12 | 0.94M | 25.9 | 182s | $0.63 |
| `jade` | 11 of 12 | 0.80M | 25.2 | 206s | $0.61 |

Jade alone against `shell-lean`: −14% tokens, −3% turns, +13% time. By
repository: cobra +18%, ky −60%, requests +43%, ripgrep −6%. Both failed
ky's download-progress task; `shell-lean` also failed requests'
double-slash task, which Jade solved.

Against the previous rerun, Jade's tokens per run are level (0.82M to 0.80M)
and its success unchanged, so 0.0.5–0.0.7 did not cost tokens. Time got
worse: one ripgrep run took 706s against 314s for the shell. Per repository
the sign flips between runs (requests went from −58% to +43%), which is the
noise of one run per task; only the totals are worth quoting.

Jade's runs made 13.8 calls before the first edit against 7.9 for the shell,
and re-read an unchanged target 4.2 times per run against none.

## Transcript fixes, and a larger core profile

Jade at `15138d6`: timeout hints that name only the tool just called and join
the running job, test results with counts and the first failure, and
validation kept to its target. Two Jade arms, no shell arm: `core` as shipped,
and `core-exec` with `run_command` and `diff` added to the core list. Same
tasks and cap, $15.06 in total. Recorded 2026-09-14. The shell and 0.0.7 rows
are from the rerun above.

| Arm | Solved | Tokens | Turns | Time | Cost |
|---|---|---|---|---|---|
| `shell-lean` (0.0.7 rerun) | 10 of 12 | 0.94M | 25.9 | 182s | $0.63 |
| `jade` at 0.0.7 | 11 of 12 | 0.80M | 25.2 | 206s | $0.61 |
| `core` at `15138d6` | 10 of 12 | 0.83M | 26.3 | 162s | $0.60 |
| `core-exec` | 11 of 12 | 0.95M | 26.8 | 185s | $0.66 |

- **The fixes cost nothing and saved time.** `core` is level with 0.0.7 on
  tokens (+4%) and 21% faster, 11% faster than the shell. Its one extra loss,
  ripgrep's hidden-whitelist task, hit the $1.50 cap with no edit made; 0.0.7
  and `core-exec` solved it near the cap too. No run timed out, so the job
  join was not exercised; 20 of `core`'s responses carried a named first
  failure.
- **`run_command` in the core profile did not pay, and could not have.** It
  runs only commands declared in `.jade/commands.json`, none of the four
  repositories declares any, and `declare_command` stayed unlisted. Its seven
  calls answered "no commands declared". `core-exec` spent 14% more tokens
  for a second tool schema and `diff`, which three calls used. The reproduction
  gap seen in the 0.0.7 transcripts remains.

The core profile stays as it was.

## What the numbers say

- **Most of the saving over Claude Code's defaults is the shorter tool list.**
  The built-in tools are 38.2k tokens of prompt on every turn; trimmed to four
  they are 15.8k, Jade's core profile 13.9k. The trimmed shell alone saves 34%.
- **Jade's own share is 18–25% fewer tokens than a trimmed shell,** with fewer
  turns, less time and at least equal success.
- **Jade added to the shell does not pay.** The agent sent half its calls to
  Bash and paid for both tool lists: 9% more tokens than the full shell.
  Jade is recommended in place of the built-in tools, not beside them.

## Tool usage

From `telemetry` in the 24 Jade-arm suite runs:

| Tool | Calls |
|---|---|
| `grep` | 86 |
| `read_range` | 83 |
| `run_tests` | 60 |
| `replace_text` | 37 |
| `check` | 18 |
| `find` | 13 |
| `insert` | 11 |
| `apply` | 10 |
| `workspace_tree` | 4 |
| `create_file` | 2 |
| `delete_file` | 1 |
| `outline` | 0 |

Retried after a failed answer: `apply` after `not_found`, 4 times — anchors
written from memory, since answered with the line where file and anchor
differ. The rerun's 12 Jade runs had the same order, with `read_range` first
(90) and `outline`, `create_file` and `delete_file` never called.

For 0.0.5, which merges and cuts tools by this data:

- **`outline` is listed in the core profile and was never called** in 36
  Jade-only runs. `grep` and `read_range` carried the reading.
- **`find` is a small share** (13 of 325 calls) next to `grep`; agents search
  text first, declarations rarely.
- **The rest of the inspect cluster** — `search`, `retrieve`, `context`,
  `repository_map`, `read_symbol` — is outside the core profile, and no task
  needed it. The pilot cannot say whether they help; it says agents solved
  every task without them.

## Caveats

- **One run per cell.** The same task moved Jade alone from 13 to 32 turns
  between rounds. Read per-task differences under about 30% as noise; the
  repository and overall means are the claims.
- **Four languages, not five.** No Java repository yet: no JDK on the
  benchmark machine.
- **Tuned on these tasks.** Jade was changed between per-repository rounds
  from what their transcripts showed. Fixes were general, but the suite ran on
  the repositories they came from. The 0.1.0 rerun adds repositories nobody
  tuned for.
- **Language servers.** Only Go had one during the suite; TypeScript, Python
  and Rust edits were unchecked until the syntax fallback, which only the
  rerun measured.
- **Environment noise.** ky's full test script runs browser suites, and
  requests has proxy tests that fail in the sandbox for unrelated reasons. All
  arms saw the same noise.
- **Budget stops.** `shell` and `shell-lean` on ripgrep null-data stopped at
  the $1.50 cap.
- Spend: about $72 in total, pilot rounds included.
