# Benchmark results: the 0.0.4 pilot

Four outside repositories, three real closed issues each, run with
`arno-bench agent` as described in [benchmark.md](benchmark.md). Claude Code
headless on Sonnet 5, one run per task per arm, $1.50 cap per run. Recorded
2026-09-13.

| Repository | Language | Tasks |
|---|---|---|
| [cobra](https://github.com/spf13/cobra) | Go | completion mutates `os.Args`; plugin version flag display name; `--help`/`--version` with completion and disabled flag parsing |
| [ky](https://github.com/sindresorhus/ky) | TypeScript | download progress drops response metadata; mixed-case method not uppercased; `parseJson` ignored by `clone` |
| [requests](https://github.com/psf/requests) | Python | content-type parameter without a value; preserve a double slash in the path; redirect history references itself |
| [ripgrep](https://github.com/BurntSushi/ripgrep) | Rust | hidden whitelist for a dot path; null data with line regexp; replace with multiline lookaround panics |
| [django](https://github.com/django/django) (from 2026-09-16) | Python | model formset ignores a custom `add_prefix()`; admin `__exact` search crashes on a choices field and over-matches booleans; FilteredRelation alias with `__` mis-resolved by `values()` and `order_by()` |

## Suite

ARNO at `a8fd3e9`. Means per run over 12 tasks.

| Arm | Solved | Tokens | Turns | Time | Cost | Scorecard vs `shell` |
|---|---|---|---|---|---|---|
| `shell` — all 27 built-in tools | 10 of 12 | 1.38M | 25.3 | 215s | $0.82 | — |
| `shell-lean` — Bash, Read, Edit, Write | 10 of 12 | 0.90M | 25.8 | 213s | $0.66 | pass (0.66x tokens) |
| `ARNO` — core profile, no built-in tools | 12 of 12 | 0.68M | 23.0 | 163s | $0.52 | pass (0.49x tokens, +17 points) |
| `ARNO+shell` — both, 39 tools | 11 of 12 | 1.50M | 25.8 | 191s | $0.84 | pass (1.09x tokens, +8 points) |

Tokens per run, ARNO alone against each shell arm:

| Repository | vs `shell` | vs `shell-lean` |
|---|---|---|
| cobra | −45% | −25% |
| ky | −70% | −22% |
| requests | −70% | +15% |
| ripgrep | −25% | −31% |
| all | −51% | −25% |

Both shell arms failed ky's download-progress task and ripgrep's null-data
task; ARNO alone solved both. `ARNO+shell` failed the ky task.

## Rerun after the pilot's fixes

ARNO at `4c7694c`: failed-call fixes, dependency source reads, tree-sitter
syntax checks and `.arno/project.json`. Only `ARNO` and `shell-lean`, same
tasks and cap.

| Arm | Solved | Tokens | Turns | Time | Failed ARNO calls |
|---|---|---|---|---|---|
| `shell-lean` | 11 of 12 | 0.99M | 29.0 | 200s | — |
| `ARNO` | 11 of 12 | 0.82M | 24.8 | 153s | 0.42 per run (suite: 0.67) |

ARNO alone against `shell-lean`: −18% tokens, −14% turns, −24% time. By
repository: cobra −18%, ky −31%, requests −58%, ripgrep −2%. Both failed ky's
download-progress task. The suite's requests result reversed, so it was noise.

## Rerun at 0.0.7

ARNO at `b4921b2` (v0.0.7): response budgets, provenance, preconditions,
impact checks and the deprecation of five overlapping tools. Only `ARNO` and
`shell-lean`, same tasks and cap, $14.85 in total. Recorded 2026-09-14.

| Arm | Solved | Tokens | Turns | Time | Cost |
|---|---|---|---|---|---|
| `shell-lean` | 10 of 12 | 0.94M | 25.9 | 182s | $0.63 |
| `ARNO` | 11 of 12 | 0.80M | 25.2 | 206s | $0.61 |

ARNO alone against `shell-lean`: −14% tokens, −3% turns, +13% time. By
repository: cobra +18%, ky −60%, requests +43%, ripgrep −6%. Both failed
ky's download-progress task; `shell-lean` also failed requests'
double-slash task, which ARNO solved.

Against the previous rerun, ARNO's tokens per run are level (0.82M to 0.80M)
and its success unchanged, so 0.0.5–0.0.7 did not cost tokens. Time got
worse: one ripgrep run took 706s against 314s for the shell. Per repository
the sign flips between runs (requests went from −58% to +43%), which is the
noise of one run per task; only the totals are worth quoting.

ARNO's runs made 13.8 calls before the first edit against 7.9 for the shell,
and re-read an unchanged target 4.2 times per run against none.

## Transcript fixes, and a larger core profile

ARNO at `15138d6`: timeout hints that name only the tool just called and join
the running job, test results with counts and the first failure, and
validation kept to its target. Two ARNO arms, no shell arm: `core` as shipped,
and `core-exec` with `run_command` and `diff` added to the core list. Same
tasks and cap, $15.06 in total. Recorded 2026-09-14. The shell and 0.0.7 rows
are from the rerun above.

| Arm | Solved | Tokens | Turns | Time | Cost |
|---|---|---|---|---|---|
| `shell-lean` (0.0.7 rerun) | 10 of 12 | 0.94M | 25.9 | 182s | $0.63 |
| `ARNO` at 0.0.7 | 11 of 12 | 0.80M | 25.2 | 206s | $0.61 |
| `core` at `15138d6` | 10 of 12 | 0.83M | 26.3 | 162s | $0.60 |
| `core-exec` | 11 of 12 | 0.95M | 26.8 | 185s | $0.66 |

- **The fixes cost nothing and saved time.** `core` is level with 0.0.7 on
  tokens (+4%) and 21% faster, 11% faster than the shell. Its one extra loss,
  ripgrep's hidden-whitelist task, hit the $1.50 cap with no edit made; 0.0.7
  and `core-exec` solved it near the cap too. No run timed out, so the job
  join was not exercised; 20 of `core`'s responses carried a named first
  failure.
- **`run_command` in the core profile did not pay, and could not have.** It
  runs only commands declared in `.arno/commands.json`, none of the four
  repositories declares any, and `declare_command` stayed unlisted. Its seven
  calls answered "no commands declared". `core-exec` spent 14% more tokens
  for a second tool schema and `diff`, which three calls used. The reproduction
  gap seen in the 0.0.7 transcripts remains.

The core profile stays as it was.

## Full suite, with a large repository

ARNO at the 2026-09-16 working tree (0.0.12 plus the stale-revision message
and the yield metrics; one arm also listed an experimental `inspect` tool).
Four arms on 15 tasks: the twelve above and three from Django — 7,091
files, 165k lines of framework Python, pyright as the language server, tests
through `runtests.py` — added to test the claim that ARNO's advantage grows
with repository size. Django runs had a $2 cap, the rest $1.50 as before.
$36.22 in total, one run per cell. Raw results in
[bench/results/2026-09-16](../bench/results/2026-09-16).

Small repositories, 12 tasks:

| Arm | Solved | Tokens | Turns | Time | Cost |
|---|---|---|---|---|---|
| `shell` | 11 of 12 | 1.63M | 25.2 | 236s | $0.59 |
| `shell-lean` | 11 of 12 | 0.98M | 27.2 | 249s | $0.44 |
| `core` | 11 of 12 | 0.88M | 28.5 | 193s | $0.41 |
| `core+inspect` | 12 of 12 | 1.04M | 32.5 | 185s | $0.45 |

Django, 3 tasks:

| Arm | Solved | Tokens | Turns | Time | Cost |
|---|---|---|---|---|---|
| `shell` | 3 of 3 | 3.55M | 45.7 | 378s | $1.23 |
| `shell-lean` | 3 of 3 | 2.32M | 47.7 | 356s | $0.93 |
| `core` | 3 of 3 | 2.73M | 51.3 | 328s | $1.14 |
| `core+inspect` | 3 of 3 | 3.34M | 60.7 | 401s | $1.26 |

Scorecard against `shell`, all 15 tasks: `core` passes at 0.62x tokens and
equal success; `core+inspect` passes at 0.74x tokens and +7 points.

- **Size did not favour ARNO here.** On Django `core` spent 23% fewer tokens
  than `shell` and 18% more than `shell-lean`, with 8% more turns, and every
  arm solved all three tasks. Three tasks at one run each settle nothing,
  but the round gives the claim that bash loses in a large repository no
  support either.
- **`inspect` was never called.** Zero calls in 15 runs with it listed. The
  arm spent 19% more tokens and 14% more turns than `core` on the small
  repositories, 22% and 18% on Django, and solved one task more — ky's
  download-progress task, which every other arm failed in every round; one
  run cannot say whether that was the tool or the dice. Agents batched with
  what they had: `queries` on 18% of `grep` calls, `ranges` on 8% of
  `read_range` calls, and 2.5 to 3.2 parallel calls per round trip against
  1.1 for the shell arms. Removed the same day.
- **Yield.** The shell arms' tool results showed 89–91% of the fix's
  pre-fix lines, ARNO's 74–85%. Per 1k tokens of tool result, `shell`
  delivered 4.3 gold lines on the small repositories and 0.9 on Django,
  `core` 2.6 and 0.3. ARNO's runs put 151 KB of tool results into the
  context per Django task against 64 KB for `shell`; 75 KB of it was edit
  responses.
- **The edit response was the leak.** A Django test module carried 260
  pyright errors before any edit (no Django stubs), and every `replace_text`
  returned all of them: one response was 583 lines and 42 KB, none of it
  caused by the edit. Fixed after the round: diagnostics are snapshotted
  before the write and only new ones are listed, with one line counting the
  rest, and a response lists at most 12, nearest the edit first, when no
  snapshot exists. The same edit now returns 2 KB with a cold checker and
  0.6 KB warm, the new error included.
- **Tool errors.** 2.5 per ARNO run on Django, 0.7 for `shell`. Twelve
  `read_range` and four `create_file` calls named `/tmp/...`: agents
  redirected a declared command's output there because `job_output` is not
  in the core profile, so a full log has nowhere else to go. Three
  `run_command` calls guessed a name before declaring one. One `apply` of 14
  edits rolled back on one bad anchor.
- **Pyright answered a pull before re-analysing** when two edits came 0.8 s
  apart: the second edit's new error was missing until the next call. Seen
  in the live check after the round, not in a run.

## What the numbers say

- **Most of the saving over Claude Code's defaults is the shorter tool list.**
  The built-in tools are 38.2k tokens of prompt on every turn; trimmed to four
  they are 15.8k, ARNO's core profile 13.9k. The trimmed shell alone saves 34%.
- **ARNO's own share is 18–25% fewer tokens than a trimmed shell,** with fewer
  turns, less time and at least equal success.
- **ARNO added to the shell does not pay.** The agent sent half its calls to
  Bash and paid for both tool lists: 9% more tokens than the full shell.
  ARNO is recommended in place of the built-in tools, not beside them.

## Tool usage

From `telemetry` in the 24 ARNO-arm suite runs:

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
differ. The rerun's 12 ARNO runs had the same order, with `read_range` first
(90) and `outline`, `create_file` and `delete_file` never called.

For 0.0.5, which merges and cuts tools by this data:

- **`outline` is listed in the core profile and was never called** in 36
  ARNO-only runs. `grep` and `read_range` carried the reading.
- **`find` is a small share** (13 of 325 calls) next to `grep`; agents search
  text first, declarations rarely.
- **The rest of the inspect cluster** — `search`, `retrieve`, `context`,
  `repository_map`, `read_symbol` — is outside the core profile, and no task
  needed it. The pilot cannot say whether they help; it says agents solved
  every task without them.

## Caveats

- **One run per cell.** The same task moved ARNO alone from 13 to 32 turns
  between rounds. Read per-task differences under about 30% as noise; the
  repository and overall means are the claims.
- **Four languages, not five.** No Java repository yet: no JDK on the
  benchmark machine.
- **Tuned on these tasks.** ARNO was changed between per-repository rounds
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
