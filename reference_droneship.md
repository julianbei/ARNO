# Historical reference: Droneship memo

This file is kept only as a historical note. It is not the source of truth for JADE.

The active source-grounded decisions live in [docs/droneship-findings.md](docs/droneship-findings.md), [scope.md](scope.md), and the code in this repository itself.

## Why this memo is not authoritative

- It was written by a consultant and reflects a different project context.
- It makes strong claims about Droneship without requiring repository-grounded validation.
- It was useful as a directionally interesting comparison, but not as a design contract for JADE.

## What we actually reused

The JADE runtime has intentionally borrowed a few proven patterns from the Droneship lineage, but only after checking them against the repo and validating them in code:

- event bus patterns for async status updates
- workspace freshness checks against current HEAD and dirty files
- progressive disclosure: outline first, then symbol bodies
- bounded reads and token-aware context discipline
- repository-level relevance ranking concepts
- validation job patterns for decisive summaries and raw-output fallback

## What we explicitly do not carry forward

- rough regex-based symbol boundaries as the basis for edits
- assuming consultant design claims are correct without source verification
- treating a different project’s architecture as a direct implementation template for JADE
- worktree or branch overlay assumptions that do not fit this repo’s own architecture

## JADE direction

JADE is and should remain its own runtime:

- Go-first implementation
- language-neutral protocol boundaries
- parser-aware structural indexing
- edit safety and revision checks
- source-grounded behavior over consultant-era assumptions

This file exists only to preserve a record of the initial comparison. The real design decisions are in the repository itself and in [docs/droneship-findings.md](docs/droneship-findings.md).

---

# 7. The call graph and blast radius are already implemented

Current Droneship has `code_edges` and graph operations for:

```text
callers
calls
importers
blast
```

The graph returns references rather than source bodies.

`blastRadius(file)` already answers the Jade question:

> “What could changing this touch?”

The tests verify that it finds callers and importers of declarations in a file, excludes the file itself, and deliberately returns references rather than stuffing the callers' implementations into context. That impact information is also inserted into reviewer briefs.

That is directly reusable as Jade's:

```text
impact(target)
```

or:

```text
relations(
    target,
    kind = dependents
)
```

## But don't reuse its implementation as Jade's final graph

The current graph is deliberately shallow and name-based. It does not resolve actual target declarations and explicitly says it cannot reliably handle:

* duplicate same-named functions;
* dynamic dispatch;
* registries;
* string-keyed dispatch;
* cross-language calls;
* computed names.

The test suite correctly treats those limitations as part of the contract rather than hiding them.

For Jade, that suggests a good migration path:

```text
MVP:
existing approximate graph

        ↓

TypeScript:
tsserver/LSP resolved references

Go:
gopls

Rust:
rust-analyzer

        ↓

Jade normalized graph API
```

The **query API and explicit confidence/limitations** should survive. The regex graph should not be presented as authoritative IDE-grade semantics.

---

# 8. Freshness is very reusable

`src/index/freshness.ts` deserves to move almost wholesale at the conceptual level.

Droneship already learned the important lesson:

> “The index is current with master” is useless if an agent is standing in another worktree with uncommitted edits.

Its freshness model compares the indexed commit against **the caller's actual HEAD**, includes uncommitted paths, identifies changed/deleted files, and can tell whether an individual search hit is actually affected by drift.

That is exactly the problem Jade's revision system has to solve.

Jade should generalize this to:

```text
WorkspaceRevision

baseCommit
HEAD
workingTreeRevision
file revisions
index revision
diagnostic revision
```

and every result can be tagged:

```text
computed_at_revision: r182
```

Every edit then produces:

```text
r182 → r183
```

### Improvement Jade can make

Droneship's index still has to reason about index-vs-worktree freshness because agents can edit outside the index.

Jade owns the edit path.

Therefore:

> **Every Jade edit should immediately update the structural index for the modified document.**

That largely eliminates the stale-local-file problem rather than merely detecting it.

---

# 9. Branch overlays should be kept

Droneship's branch indexing is already efficient.

It maintains a base index and can represent a branch as only the changed/deleted files over the exact base commit. Queries compose the overlay with unchanged base data.

This is a very useful Jade architectural primitive:

```text
Shared base index
      +
Workspace delta
      =
Agent's current code view
```

For a fleet of 50 agents, that is much better than building 50 complete repository indexes.

Jade should take it one step further:

```text
base commit index
     +
branch committed delta
     +
working-tree/uncommitted delta
```

That gives each agent a precise live view with maximal sharing.

---

# 10. Git workspace isolation should probably be extracted

Droneship has already spent real engineering effort discovering all the sharp edges here.

Its isolation model includes:

* worktrees outside the project tree;
* one workspace per agent;
* one task branch per task;
* detached reviewer checkouts;
* dirty-worktree preservation;
* sparse checkout;
* copy-on-write clones of large dependencies/assets;
* full copy-on-write “universes” where supported;
* protection against project tools accidentally indexing nested worktrees.

The implementation lives principally in `src/git/git-service.ts`; it also has stable worktree paths and repository-root handling that accounts for resolved paths/case/symlink issues.

This is too much solved operational knowledge to throw away.

I would define a Jade abstraction:

```text
WorkspaceProvider

create(base_ref)
current_revision()
changes()
checkpoint()
revert()
destroy()
```

and port Droneship's worktree implementation behind it.

Whether **Droneship or Jade ultimately owns workspace creation** is an architectural boundary decision. My preference:

```text
Droneship/Fleet Manager
    decides WHEN and WHY a workspace exists

Jade
    owns HOW the development workspace behaves
```

So Droneship asks Jade to create/open a workspace rather than duplicating Git logic.

---

# 11. `diffSummary()` should become part of Jade State

Droneship already added a bounded diff summary because reviewers were wasting turns rediscovering the comparison range. The Git service exposes `diffSummary(repo, from, to)`, and Droneship uses it to provide changed-file sets rather than making an agent work out the range itself.

This should feed directly into Jade:

```text
jade.changes()
```

MVP:

```text
3 files
+91 -42

src/a.ts       +31 -8
src/b.ts       +52 -30
test/a.test.ts +8  -4
```

Then Jade layers semantic changed-symbol detection on top.

So the current Git implementation provides the **text/change-set foundation**.

---

# 12. The validation runner already solves half of Jade's async test system

Droneship's probe system is much more reusable than it first appears.

Projects declare executable validation with:

```yaml
name
kind
cost
command
timeout
artifacts
applies_to
rubric
```

and probes can be selected by path/label and validation tier.

Current actual Droneship probes already distinguish:

```text
lint       cheap
typecheck  cheap
unit-tests medium
```

with the 97-second unit suite deliberately excluded from the cheap gate.

That's almost exactly the synchronous/asynchronous distinction we're making for Jade.

The implementation already has a detached process runner that:

* captures stdout/stderr;
* enforces timeout;
* kills the complete process group;
* gives runs isolated output directories;
* uses a constructed execution environment;
* records run duration and result;
* parses `summary.json`;
* stores artifacts;
* emits events.

It also contains an important hard-earned lesson: synchronous probe execution blocked the server for hundreds of seconds, so server-side execution now has a detached asynchronous path.

### Recycle this as

```text
Jade Validation Job Runner
```

but strip away Droneship-specific concepts such as:

```text
reviewer
sprint
hardening
regression occurrence
human rubric
```

Jade should expose generic jobs:

```text
parser
diagnostics
lint
typecheck
test
build
custom validator
```

Droneship can then use those jobs for its review lifecycle.

---

# 13. Project command discovery should be moved into Jade

Droneship already implements the idea that an agent should not discover:

```text
npm test
vs
npm run test:unit
```

by failing first.

`discoverCommands()` derives known test/lint/typecheck/build commands from project manifests and verifies that the invocation exists rather than executing entire candidate builds. It currently handles NPM scripts and Cargo metadata.

This belongs squarely in Jade.

For the initial language support I would turn this into language adapters:

```text
TypeScript
  package.json
  workspace config
  npm/pnpm/yarn/bun
  test/lint/typecheck/build scripts

Go
  go.mod
  go test ./...
  go vet ./...
  project-specific Make targets

Rust
  Cargo.toml / workspace
  cargo check
  cargo test
  cargo clippy
```

Notably, **Go command discovery is not present in the current Droneship implementation we inspected**, so that is Jade work.

---

# 14. Output clamping should be a Jade kernel feature

`clampToolOutput()` is almost exactly the generic progressive-disclosure mechanism Jade needs for builds/tests.

The implementation was motivated by measured data: only 2.8% of results exceeded 8,000 characters, but those results accounted for 31% of tool output. Oversized output is saved in full; the agent receives a bounded head, failure-looking lines, and a path from which it can explicitly retrieve more.

This should become something like:

```text
Jade Result Envelope

summary
important[]
truncated
full_result_ref
```

rather than being confined to Droneship's Claude PostToolUse hook.

Relatedly, `gateFailureSummary()` already extracts decisive compiler/test failure lines and puts them ahead of bounded raw output because actual failures were getting buried inside several kilobytes of unrelated output.

That belongs in Jade's test/build adapters.

---

# 15. Droneship's tool-habit research may be more valuable than some code

There is one result I would treat as a **Jade product requirement**, not merely historical context.

Droneship measured 8,535 tool calls:

```text
Bash                         4,869
all index tools                 89

shell locating calls         1,746
shell file-reading calls     2,149

shell : index replacement work ≈ 44 : 1
```

Prompts already told agents to use the semantic tools. They largely did not.

The response was clever: Droneship started attaching the better bounded answer to the shell/Grep/Glob operation the model naturally chose, instead of trying to lecture it into choosing another tool. `search-nudge.ts` implements that behavior and includes empirically selected thresholds.

The Serena experiment reinforces this. Serena could answer a symbol lookup in only **38 tokens versus 635** for Droneship's search, but its tool schema itself cost roughly **2,549 tokens/session for seven tools**, and models barely chose those tools. The measured break-even was about 4.3 lookups/session, which wasn't reached.

This strongly affects Jade's interface design.

## Jade should NOT be

```text
21 optional MCP tools that sit beside:

Read
Edit
Grep
Bash
```

and hope the model prefers them.

I'd make Jade much more invasive:

```text
agent asks to read file
        ↓
Jade can return structural outline

agent edits source
        ↓
Jade intercepts edit
        ↓
diagnostics automatically returned

agent runs tests
        ↓
Jade structures/truncates result automatically
```

The **default path needs to be Jade**, not a specialist capability the model must remember exists.

---

# 16. The benchmark infrastructure should be reused wholesale

Droneship already has a benchmark philosophy that fits Jade very well.

The code-index benchmark explicitly defines the goal as:

> **tokens to answer**

not search-engine precision for its own sake. It charges the retrieval system for candidates the agent must open and requires that smaller result sets not sacrifice recall. It runs deterministically against a pinned repository with hand-checked questions.

There are also existing benchmark assets for call graphs and tool-usage arms. The current repository contains dedicated call-graph question corpora and benchmarking code around whether agents use semantic versus shell tools.

I would **move/copy these benchmark corpora into Jade immediately**.

Then add Jade-specific arms:

```text
A  shell/filesystem baseline

B  Jade inspection only

C  Jade inspection + precise edit

D  Jade + edit diagnostics

E  full Jade + async validation
```

Measure marginal gain at every stage.

That will tell you exactly which part is earning its complexity.

---

# 17. What Droneship does *not* already provide

This is the important boundary.

## A. There is no real Jade edit engine

I found no native `replace_symbol` implementation in Droneship.

Agents still edit through their harness/file tools.

So these are genuinely new:

```text
replace(symbol, content)

replace(range, content)

insert(before/after symbol)

revision-preconditioned edits

changed-symbol calculation after edit
```

Serena was tested externally and exposed editing tools, but Droneship did not adopt that as its native edit layer.

---

## B. There is no native LSP layer

Droneship's own semantic structure is regex/index based.

The repository contains a Serena experiment in which its TypeScript language server worked, but Droneship itself doesn't currently provide the TypeScript/Go/Rust LSP abstraction Jade needs.

So Jade still needs:

```text
TypeScript → tsserver / language server
Go         → gopls
Rust       → rust-analyzer
```

normalized behind one interface.

---

## C. No diagnostics-on-edit

Droneship can run lint/typecheck/test checks, but it does not own edits and therefore cannot currently do:

```text
replace symbol

→ parse
→ incremental LSP diagnostics
→ linter diagnostics
→ changed symbols

all in one result
```

This remains one of Jade's genuinely differentiated features.

---

## D. The existing graph isn't semantic enough for edit safety

It's excellent as cheap orientation.

It is not sufficient to confidently drive:

```text
rename symbol
change signature
find every reference
safe delete
```

For those, Jade needs compiler/LSP resolution.

---

# 18. What should remain in Droneship and **not** move into Jade

I would be fairly strict here.

Do **not** let Jade absorb:

* tasks and backlog;
* epics/topics;
* sprints;
* worker/reviewer assignment;
* Claude session lifecycle;
* model selection;
* cost quotas;
* findings/regression triage;
* merge orchestration;
* human-review lifecycle;
* fleet scheduling.

Droneship describes itself as the control plane for disposable agent processes, with durable state and audit events in SQLite.

That is a separate concern.

The boundary should become:

```text
              Droneship
       Fleet / orchestration
               │
               │ creates workspace
               ▼
              Jade
       Development runtime
               │
        ┌──────┼──────┐
        │      │      │
      code    git   validators
```

Droneship asks:

> “Worker 7 needs a workspace for DRONE-123.”

Jade handles:

> “Here is its workspace, structural index, edits, diagnostics, tests and change state.”

That's clean.

---

# 19. My recommended extraction plan

I would **not fork Droneship and start deleting things**.

Create Jade as a separate boundary, then move concepts across in this order:

1. **Result/output model** — port `clampToolOutput`, decisive failure extraction, structured result references.
2. **Workspace/Git primitives** — extract or wrap worktree management, revisions, diff summaries and freshness.
3. **Current index as `RetrievalIndex`** — reuse local embeddings, search ranking, branch overlays, candidate paths, repo map.
4. **New `StructuralIndex`** — replace regex symbol truth with Tree-sitter adapters for TypeScript, Go and Rust.
5. **Port `read_symbol` semantics** to `jade.read(symbol)` using the structural index.
6. **Port graph/blast API** but progressively back it with LSP/compiler references instead of shallow name edges.
7. **Build the new Edit Engine** — symbol/range replacement + revision checks.
8. **Add LSP sessions and diagnostics-on-edit** — the major new piece.
9. **Extract the probe process runner into generic Validation Jobs** and connect tests/build/typecheck asynchronously.
10. **Reuse Droneship benchmark corpora and metrics** to compare Jade against the existing baseline.

Then make Droneship consume Jade and delete duplicated implementations only once Jade proves equivalent or better.

---

# 20. One architectural decision needs revisiting: Go vs TypeScript for Jade

Earlier I suggested **Go** for the Jade runtime.

After inspecting Droneship, I would no longer treat that as an obvious choice.

There is now a meaningful body of working TypeScript code in Droneship for:

```text
indexing
embeddings
symbols
graphs
repo maps
branch overlays
freshness
Git workspace handling
validation execution
output compression
benchmarks
```

The `src/index` subsystem alone includes `code-index.ts`, `embeddings.ts`, `file-kind.ts`, `freshness.ts`, `search-benchmark.ts`, `symbols.ts`, and `vocabulary.ts`.

If Jade is Go:

> **we conceptually reuse a lot but physically rewrite almost all of it.**

If Jade starts TypeScript:

> **we can extract working modules, preserve their tests, and replace internals incrementally.**

Given Jade's first supported language is TypeScript anyway, and LSP/process/Git workloads do not require Go performance, I would now lean toward:

### **Jade v1 in TypeScript**

unless there is a strong operational reason that Jade must be a standalone Go binary.

We can still put a clean process/RPC boundary around it later.

---

# Bottom line

I would characterize the current situation like this:

```text
                        JADE MVP

INSPECT        ~60–70% precursor exists in Droneship
  search              ██████████
  repo outline        ██████████
  read symbol         ██████████
  call graph          ███████░░░
  semantic accuracy   ███░░░░░░░

MODIFY         ~10% exists
  file/git state      ██████░░░░
  symbol edits        ░░░░░░░░░░
  safe range edits    ░░░░░░░░░░

VALIDATE       ~50% infrastructure exists
  command discovery   ███████░░░
  process runner      █████████░
  result filtering    █████████░
  LSP diagnostics     ░░░░░░░░░░
  diagnostics-on-edit ░░░░░░░░░░

STATE          ~70–80% precursor exists
  workspaces          █████████░
  branches            █████████░
  freshness           █████████░
  diff state          ████████░░
  live edit revision  ███░░░░░░░
```

So I would **not sell Jade internally as greenfield**.

A more accurate framing is:

> **Jade extracts the development-environment capabilities that have already emerged inside Droneship, replaces their approximate code understanding with proper language tooling, adds a first-class mutation layer, and closes the edit→diagnostics→validation loop.**

That gives the team a much better starting point—and importantly, several of Jade's core assumptions have already been tested against real agent behavior rather than invented on a whiteboard.
