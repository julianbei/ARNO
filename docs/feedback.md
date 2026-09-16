# arno feedback — why the agent reached for bash

Kept by the agent doing the work, updated at the end of each task loop.
Consolidated 2026-09-12 after Phase 12 closed.

**Why this file exists.** If arno does not cover a need, or covers it more
expensively than the shell, the model silently falls back to bash — and every
fallback is a hole in arno's telemetry, guardrails and revision tracking. A
bash fallback is not a small inefficiency; it is arno not being in the loop at
all. So each one is recorded here with its reason, and reasons that recur
become backlog items.

Honest framing: some fallbacks are arno's fault (missing capability, worse
ergonomics) and some are mine (habit, when a arno tool existed and I did not
reach for it). Both are recorded, because "the model defaulted to bash out of
habit" is also a product problem — it means the arno path was not obviously
better at the moment of choosing.

---

## Current state

Phases 7–12 produced sixteen tasks of evidence. Nine recurring bash reasons
were identified; **seven are now closed**, and the closures are measurable:
the last four tasks ran every source edit through arno with zero bash edits
and zero splice repairs.

| # | Fallback | Status |
|---|---|---|
| 1 | `go build` / `vet` / `test` | **Closed** — 12.2 `check`, 12.11 `run_command` for anything else |
| 2 | `grep -n "func X" -A 30` — locate *and read* a declaration | **Closed** — 12.8 `find` |
| 3 | `grep -rn` — find a struct field, string literal, config key | **Closed** — 13.1 `grep` |
| 4 | `python3 <<PY` — multi-site edits in one shot | **Closed** — 12.10 `apply` |
| 5 | `cat >> file` — append to an existing file | **Closed** — 12.10 `insert` |
| 6 | `gofmt -l` / `-w` | **Closed** — 12.7, formats on every edit path |
| 7 | `git log -L` / `diff` / `status` | **Closed** — 8.2, 9.3, 3.2. Residual: no `blame`, no `diff HEAD~3` |
| 8 | `sed -n '30,60p'` — read a known range | **Never a gap** — `read_range` does this. Habit. |
| 9 | `ls` after an edit — verify the write landed | **Not a gap** — independent verification is the point of dogfooding |

**The two highest-cost fallbacks in the whole log both caused real damage,
and both are now closed.** The python3 multi-edit produced the session's only
outage — a replace that hit the wrong anchor left a duplicate `fmt` import and
took the MCP server down mid-reconnect. And `grep -rn` was the most frequent
single reason to leave arno, recurring in every task that touched unfamiliar
code, for three tasks running after `find` shipped and did not close it.

**What replaced them is now the load-bearing surface**, so its quality matters
more than anything still on this list: `apply` (atomic multi-file edits with
one revision and one validation), `find` (locate + read a declaration), `grep`
(everything that is not a declaration name), `check` and `run_command`.

---

## Open, in priority order

Reprioritized 2026-09-12. Ranked by *how much a gap costs when hit*, not by
how often it is hit — a rare fallback that silently corrupts state outranks a
frequent one that merely wastes tokens.

### ~~1. Telemetry~~ — **shipped 13.2**

Records per tool: calls, response bytes, wall time, and a failure *class* —
with the classes that plausibly send a caller back to bash (`ambiguous`,
`stale_revision`, `timeout`, `not_found`, `unavailable`) rolled up as the
headline number. Content-free by design: no arguments, no response bodies, no
error text, so the log is shareable.

**This file should now be checked against the log rather than trusted.** Every
number above this line is recollection; from the next task on, the bash counts
and the fallback counts have an independent source. Where they disagree, the
log wins.

~~Caveat: in-band failures were counted as successes.~~ **Closed by 13.7.**
The audit found `SymbolResolution` on `InspectResponse` is the only response
field that reports failure in band; it is now read at `jsonResult` and counted.
Everything else that fails returns a real error.

Two exclusions the metric depends on, worth not re-litigating: a `Passed=false`
check is a **verdict, not a fallback** (a red build is arno working, and
counting it would swamp the signal with ordinary broken code), and an empty
`grep`/`find` result is a correct answer, not a failure. Both have tests.

~~Remaining known hole: a waited `check` that times out returns
`Status: "running"`, indistinguishable from a deliberate `wait=false` call.~~
Closed 2026-09-13 (0.0.3): validation carries a typed outcome, and a waited
run that ran out of time is `timed out`, classified as `timeout`. A missing
tool is `unavailable`.

### ~~2. No whole-file read~~ — **shipped 13.3**

`read_range` now takes only `path`. Unset start means line 1, unset end means
EOF, so neither set reads the whole file. Clamped at 20,000 bytes. Verified
live: `arno_read_range("go.mod")` returned the file that was a `cat` in the
three preceding entries of this log.

### ~~3. `check` success output looks like a partial run~~ — **shipped 13.4**

Passing runs now count (`42 packages ok, 3 with no test files`) instead of
quoting the last three lines. Failing runs keep the decisive-line extraction
unchanged, which was always the good half. Applied at all three sites that
share `checkPassed`, so `check`, `run_tests` and `run_command` cannot drift.

### ~~4. No whole-file overwrite~~ — **shipped 13.5**

`replace_file` overwrites an existing file wholesale, symmetric with
`create_file`: that one refuses when the file exists, this one refuses when it
does not. No force flag — choosing the tool is the explicit act, which also
lets each error name the right alternative. This file's own 13.x rewrite was
the motivating case.

### 5. `search` answers a different question than it appears to — **settled at the 11.5 freeze**

Kept, with its description rewritten to say plainly that it matches names only.
`docs/tool-contract.md` records the `search`/`grep`/`find` overlap as
deliberate rather than accidental, so it is not "fixed" later by someone
reading it as a bug. Original note follows.


Now documented rather than fixed. Its description said "hybrid search over
symbols and files"; it ranks *symbol names* by similarity and does not read
file contents, so asked for `ChangedPaths` it returned `ChangedFile`,
`CheckRequest` and `CommitInfo` — none of which contain the string — while
missing the field itself and all eight files using it. 13.1 rewrote the
description to say so plainly and added `grep` as the real answer. Whether
`search` earns its place alongside `find` and `grep` at all is a live
question for 11.5's tool-surface freeze.

### ~~6. Residual git gaps~~ — **13.6: diff shipped, blame declined**

`diff(since="HEAD~3")` shipped — it answers "what has this branch done", which
the working-tree diff cannot once any work is committed.

`blame` deliberately not built: `history` already answers the question blame
gets used for, with commit messages attached, and per-line authorship rarely
changes what an agent should do. Its output is also verbose, and 13.8 had just
established response cost as arno's least-noticed failure mode. Recorded as a
decision for the user to overturn, not a silent drop.

### 7. Habit, not capability

Recurring and worth stating separately because no feature fixes it: several
fallbacks happened while a working arno tool sat unused. The clearest case is
the shell validation trio, which kept winning for three tasks *after* `check`
shipped and was verified — because the loop prompt names `go build && go vet
&& go test` literally, and whatever names the command wins. A tool being
better is not sufficient. This is an argument for tool descriptions that say
when to prefer them (13.1's `grep` and `search` descriptions both do), and for
not naming shell commands in prompts that have tool equivalents.

---

## Trend across tasks

**Once `replace_text` (12.1) existed, every source edit went through arno and
landed first try** — no line numbers, no re-reads, no splice repairs. Every
earlier multi-file task needed at least one fix-up edit.

**Once `apply` (12.10) existed, mistakes became cheap instead of dangerous.**
Four batches across 12.5 and 12.11 failed their own build check on my typos —
a dropped `//`, a `PLACEHOLDER` token I forgot to fill — and each came back
with exact diagnostics in the same response. That class of mistake previously
surfaced as a broken server.

**Two `apply` semantics confirmed by use, worth not re-deriving:**
- When every edit lands but the `check` fails, it does *not* roll back. It
  reports diagnostics and keeps the edits. Correct: rollback is for edits that
  fail, not code that fails to compile, and silently reverting a half-finished
  intentional refactor would be worse.
- Preflight rejects a bad anchor before writing anything, so a batch with one
  stale anchor costs nothing and leaves no partial state.

**Live dogfooding catches what unit tests do not.** 12.5's full suite was green
on a `changes()` response that was visibly wrong — directory entries rendered
as changed files. Only calling the tool for real showed it.

### Per-task bash counts

| Task | bash calls | Dominant reason |
|---|---|---|
| 7.1–9.3 | not counted | no `check`, no batch edit, no `find` |
| 12.1–12.4 | not counted | validation trio, append, locate |
| 12.6 | — | locate, append |
| 12.9 | 3 | format, locate, validate-by-habit |
| 12.10 | 4 | format, validate-by-habit, locate, write file |
| 12.8 | 4 | format, locate, one-off probe |
| 12.7 | 2 | one combined validation, one file write |
| 12.5 | 4 | **3 of 4 were `grep -rn`** |
| 12.11 | 6 | 2 `grep -rn`, 1 whole-file read, validation |
| 13.1 | 3 | validation, one whole-file read, one whole-file write |
| 13.2 | 4 | validation ×2, one whole-file read (`.gitignore`), one failed probe |
| 13.7 | 3 | validation ×2, one grep for in-band outcome fields |
| 13.3 | 3 | validation ×3 — and none of them needed to be bash |
| 13.8 | **0** | — |
| 13.4 | **0** | — |
| 13.5 | 1 | `go test -run X -v` — one named test's verbose output |

**13.5's single call is the last recurring read-side gap**: running one test by
name and seeing its output. `run_tests` has a `test` scope that covers it and I
did not reach for it — habit, third appearance. Not a capability gap, but the
tool is clearly not reaching for itself.

| 13.6 | **0** | used `run_tests(scope=file)` — the habit above, finally closed |
| 11.1 | 2 | both measurement: latency timing loop, tool-catalog probe |
| 11.2 | 1 | building a foreign repo and handshaking the binary against it |
| 11.3 | 1 | same — foreign-repo verification |
| 11.4 | 1 | cross-checking the README tool list against the live catalog |
| 11.5 | 2 | extracting the live schema; proving the new guard fails |
| 11.6 | 4 | driving the binary against an unrelated private Go repository, both benchmark arms |
| 11.7 | 2 | checking filesystem case-sensitivity; validating template YAML |
| 11.8 | 1 | verifying tag-based version stamping with a throwaway tag |
| 11.10 | 3 | isolating why the release gate exited non-zero |

**11.10's calls are the honest kind of shell use**: bisecting a shell script's
behaviour under `set -e`. arno runs commands; it does not debug them, and it
should not try to. The finding was worth the calls — `gofmt -l … | (! grep .)`
aborts the whole script under `set -e`, so the release gate had been exiting 1
while reporting `pass`. The false-green bug was hiding a genuinely broken gate.

**11.8 found a false-green in arno's own validation surface**, and found it by
using that surface: a `release-gate` command printed `pass` alongside
`exit status 1`. `run_command` decides pass/fail by scanning output text and
never looks at the process exit status, so any command that exits non-zero
while printing something cheerful reads as green. Confirmed with
`echo "everything looks fine"; exit 3` → `pass`.

That is the 12.11 registry's whole purpose undermined: `check` covers Go tools
that announce their own failures in words, but lint, codegen and migrate
scripts signal by exit code. Filed as **11.10**; 0.0.1 is not tagged until it
is fixed.

**11.7's first call prevented a data loss.** The task said to write
`FEEDBACK.md`; `ls` showed it already resolving to this file, because the
filesystem is case-insensitive. Creating it would have silently overwritten
sixteen tasks of log. Nothing in arno's surface answers "does this path already
exist under a different case" — `create_file` would have refused, which is the
right behaviour and would also have been the first warning. Not filed as a gap:
the guardrail worked, it just was not the thing that caught it first.

## First outside test — arno lost, 2026-09-12

11.6 ran arno against an unrelated private Go repository — a real codebase,
not this one — on a real question ("is the CAS store's write atomic?").

| | calls | est. tokens |
|---|---|---|
| arno | 3 | ~646 |
| shell | 2 | ~476 |

**1.36x, against the 0.85x this project quotes.** Both numbers are honest: the
0.85x came from seven scenarios in arno's own repo, this from one scenario in a
foreign one. The quoted figure should carry that caveat from now on.

The cause was an ambiguous-symbol round trip — two `Put` methods on different
receivers — where arno returned bare IDs and forced a third call. Fixed in
**11.9**: candidates now carry their declaration line, so the receiver is
visible without another call.

**Re-measured afterwards, and the diagnosis above was half wrong.** Turns went
3 → 2, but tokens only moved 1.36x → 1.31x. The token cost was never the round
trip; it is `read_symbol` returning a 60-line body (which `grep -A 60` returned
too) plus `find Store` answering with two Store types when one was wanted.
Worth fixing on turns alone, but the token claim was mine and it was wrong. Worth noting the shell arm only *looked* cleaner because I had typed
the receiver into the pattern; an agent grepping `func.*Put` would have read
one of two candidates without knowing there was a choice. arno paid a turn for
a correctness property grep did not have, which is defensible — but paying it
was avoidable, which is the bug.

**The other finding is about me, not the tool.** My first arno attempt used
`grep` with 18 lines of context across five matches, and was worse than my
second attempt with `find` + `read_symbol`. The tool that fits is not the one
that comes to hand first — even for the caller who wrote the tools. Every
remaining entry in this log's "habit" section is the same shape.

**11.5's second call is one this log should record approvingly.** Deliberately
breaking the new contract test to watch it fail, then restoring — a guard
nobody has seen fail is not yet a guard. arno has no way to do that to itself
and should not.

**11.4's call found a documentation bug and then became a tool.** Diffing the
binary's `tools/list` against the README caught `insert` documented as a tool
it is not, and `search_nudge` missing. That check is now a declared
`docs-check` command, so the one-off bash became a repeatable arno call —
which is the intended lifecycle for this whole log: bash reveals the gap, the
gap becomes a command.

**Phase 11's bash calls are all one category: verifying arno against a
repository that is not this one.** arno structurally cannot do this for itself
— it only ever sees the workspace it was launched against — so this is not a
gap and should not be closed. It is also, twice running, where the real bugs
were: 11.2 found a silently truncated Python outline, 11.3 found four lines of
German git stderr prefixing every response in a fresh repo. Neither was
reachable from inside arno's own checkout.

**11.2's call is the same non-gap category as 11.1's**: verifying the shipped
artifact against a repository outside this one, which arno structurally cannot
do for itself — it only ever sees the workspace it was launched against.

**11.1's two calls are a category this log has not had before, and they are
not a gap.** Timing a binary's startup and probing its JSON-RPC catalog are
things arno should not do — it would mean arno measuring arno, which is
circular, and the whole point was checking the artifact independently of the
running server. Recorded so the count stays honest, but nothing to build.

**Four of the last five tasks used no bash at all.** The open list in this file
is now empty; every capability gap it recorded across Phases 7–13 is either
shipped or explicitly declined. What remains is item 7 below — habit — and the
evidence there is genuinely good: the three fallbacks that survived longest
(validation, whole-file read, single-test run) all closed within two tasks of
being named explicitly in this log, not when the tool shipped.

That is the file's own strongest finding. Writing down *why* a tool lost is
what closed the gap, more reliably than building the tool did.

**13.8 is the first task with no bash at all.** Validation ran through
`run_command(vet)`, `run_command(fmt-check)` and `check(tests)`; every edit
through `apply`; every read through `grep`, `read_range` and `read_symbol`.
Sixteen tasks of this log ranked the validation trio as the single most
frequent fallback, and it is now gone — not because a tool finally existed,
but because `run_command` let the repo name the commands the prompt names.

**13.7 note:** the grep was an audit sweep across the protocol types — "which
responses carry a status field" — which `arno_grep` will cover once live. The
other two were the validation trio again, now genuinely closable via
`run_command`. Worth watching whether the telemetry log agrees with these
hand-counts once a reconnect lands; that comparison is the first real test of
whether this file has been accurate.

**13.2 note:** one of the four was a throwaway `go test` probe written to a
temp file to inspect a function's return values — it failed on a package-name
mismatch and was abandoned. A scratch-evaluation path ("call this function,
show me what it returns") has no arno equivalent and is the second time a
one-off probe has appeared in this log. Not yet frequent enough to file.

---

## First live telemetry reading — 2026-09-12

The instrument is running. First session after the reconnect that made
`grep`, `telemetry`, `run_command` and whole-file `read_range` live:

```
7 calls across 6 tools, 1 errors
fallback risk: not_found 1
arno.run_command  2 calls  372B  avg 94ms
arno.changes      1 calls  4.6KB avg 704ms
arno.grep         1 calls  990B  avg 19ms
arno.read_range   1 calls  144B  avg 16ms
arno.read_symbol  1 calls  30B   avg 19ms  1 errors (not_found 1)
arno.telemetry    1 calls  172B  avg 0ms
```

**What the log says that this file did not.** `arno.changes` costs 4.6KB and
704ms — roughly 5x the bytes and 35x the latency of anything else. Sixteen
tasks of hand-written feedback never mentioned it, because response cost is
invisible when you are the one reading the response. Filed as **13.8**.

That is the argument for telemetry in one line: the gaps a model notices are
the ones that annoy it, not the ones that cost the most.

---

## Standing note for future loops

At the end of each task, append: which bash commands were run, whether a arno
tool could have done it, and if so why it was not used. "Habit" is a valid and
important answer — it means the arno path was not the obvious one at the
moment of choosing, which is a design problem, not a discipline problem.

Keep this file consolidated. It is a backlog input, not an append-only log:
when a gap closes, move it to the table above rather than leaving a stale
"open" note behind.
