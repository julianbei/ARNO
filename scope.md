# Jade — Julian's Agentic Development Environment

## Product, Architecture and Implementation Concept

**Status:** Initial concept / team briefing
**Project name:** **Jade**
**Expansion:** **Julian's Agentic Development Environment**
**Initial languages:** TypeScript, Go, Rust
**Later languages:** Java, Python, JavaScript, C#, Ruby, Scala, others

---

# 1. Executive summary

**Jade is an Agent Development Environment designed specifically for software-engineering agents.**

Today's coding agents typically interact with repositories through abstractions inherited from human shell usage:

```text
read file
grep
find
sed
write file
run command
run tests
```

These tools work, but they are poorly matched to LLMs.

A human developer using a modern IDE rarely reads entire files or manually searches every reference. The IDE continuously provides structural information, diagnostics, navigation, refactoring support, test status, type information and incremental feedback.

Jade applies the same principle to agents, but without copying the graphical IDE.

> **The objective is to expose the information advantage of a modern IDE through an agent-native interface optimized for tokens, model turns, latency and correctness.**

For example, instead of sending a 2,000-line TypeScript file to an agent, Jade can return:

```text
src/auth/session.ts

imports ×14

class SessionManager
  constructor(...)                     L21-L29
  createSession(...)                   L31-L76
  refreshSession(...)                  L78-L143
  revokeSession(...)                   L145-L181
```

The agent can then request only:

```text
open SessionManager.refreshSession
```

Likewise, instead of having an agent edit a file and then spend another model turn asking a compiler whether the edit was valid, Jade returns immediate diagnostics as part of the edit result.

```text
replace SessionManager.refreshSession with <new code>

→ edit applied
→ parser OK
→ 1 linter error
→ 0 type diagnostics
→ affected tests started asynchronously
```

Jade therefore sits between the agent and the development workspace:

```text
Agent / Agent Harness
        │
        ▼
       Jade
        │
 ┌──────┼────────┬─────────┐
 ▼      ▼        ▼         ▼
Code   Edit   Diagnostics  Tests
Model  Engine     / LSP    / Build
 │
 ▼
Repository
```

The shell remains available as an escape hatch, but should no longer be the normal interface for understanding and changing source code.

---

# 2. Problem

Current coding agents waste a significant amount of model capacity on operations that a normal IDE already solves deterministically.

A typical agent may execute:

```text
grep for symbol
read 500 lines
grep for usages
read another file
edit file
run compiler
read compiler output
fix syntax error
run compiler again
run tests
read thousands of lines of test output
```

This creates four major inefficiencies.

### Context inefficiency

Repositories are much larger than the code needed for an individual task. Raw file operations routinely place irrelevant source code into the model context.

### Round-trip inefficiency

Agents frequently use separate LLM/tool cycles for:

```text
edit
→ compile
→ interpret
→ lint
→ interpret
→ test
→ interpret
```

Many of those checks could happen automatically following an edit.

### Semantic weakness

Files and line numbers are a poor model of software. Agents actually reason about:

```text
functions
methods
classes
types
interfaces
references
callers
dependencies
tests
```

The development environment should expose those concepts directly.

### Tool-output inefficiency

Commands such as builds, linters and test runners produce output intended for humans and CI systems.

An agent normally needs:

```text
1 failed test
relevant error
relevant stack
```

not 10,000 lines of stdout.

---

# 3. Product definition

## Jade's job

Jade owns the agent's interaction with a software workspace.

It provides four classes of capability:

```text
                         JADE

          ┌───────────────┼───────────────┐
          │               │               │
       INSPECT          MODIFY         VALIDATE
          │               │               │
          └───────────────┼───────────────┘
                          │
                        STATE
```

### Inspect

Efficiently understand code.

### Modify

Precisely change code.

### Validate

Immediately determine whether the resulting state is valid.

### State

Maintain a coherent understanding of the workspace and what has changed.

---

# 4. What Jade is not

Jade should have a deliberately clear boundary.

It is **not**:

* an LLM;
* an agent reasoning loop;
* a replacement for Claude Code, Codex or another harness;
* a CI system;
* a source-control hosting system;
* a general MCP collection;
* a graphical IDE;
* a replacement for the shell.

The relationship should instead be:

```text
Agent Harness
    │
    ├── Jade ─────────── code workspace
    │
    ├── browser ──────── web
    │
    ├── GitHub ───────── repository hosting
    │
    ├── Jira ─────────── issues
    │
    └── other skills ─── external systems
```

**Jade is specifically the development environment of the agent.**

---

# 5. North Star

## North Star definition

> **Jade is a stateful, language-aware development runtime that allows autonomous software-engineering agents to inspect, modify and validate arbitrarily large codebases while consuming only the context necessary for the current task.**

The intended long-term experience is that an agent rarely needs to operate on raw files.

Instead it interacts with progressively richer software concepts:

```text
Repository
   │
   ▼
Project / package
   │
   ▼
File
   │
   ▼
Class / type / function
   │
   ▼
Implementation
   │
   ▼
Block / source range
```

---

# 6. North Star design principles

## 6.1 Progressive disclosure

Jade should send the **minimum sufficient representation** first.

A file request does not necessarily mean "return the file."

```text
open src/payments/service.ts
```

should initially be capable of returning:

```text
imports ×21

type PaymentResult                       L18-L27

class PaymentService                    L30-L284
  constructor(...)                      L36-L49
  authorize(...)                        L51-L113
  capture(...)                          L115-L186
  refund(...)                           L188-L251

function mapProviderError(...)           L286-L324
```

The agent can subsequently expand individual regions.

Progressive disclosure should exist for:

* repositories;
* directories;
* files;
* symbols;
* changes;
* diagnostics;
* test output;
* call graphs;
* dependency graphs;
* build output.

---

# 7. Structural addressing

Lines remain useful, but **symbols must become first-class objects**.

For example:

```text
src/auth/session.ts::SessionManager.refreshSession
```

Jade should maintain stable internal symbol identifiers where possible.

That enables operations such as:

```text
read(symbol)

replace(symbol, new_code)

references(symbol)

callers(symbol)

history(symbol)
```

without requiring the agent to rediscover the source location every time.

Line positions are returned for orientation but should not be the primary identity of a code element.

---

# 8. In-place replacement

This is one of Jade's core primitives.

The agent must be able to say:

```text
replace function SessionManager.refreshSession with:

<new implementation>
```

Jade finds the existing function boundaries and replaces exactly that function.

The new implementation may contain:

* fewer lines;
* more lines;
* changed formatting;
* changed nested structures.

Jade reparses the resulting file and recalculates symbol locations automatically.

## Range replacement

Textual replacement must also remain available:

```text
replace src/auth/session.ts L123-L128 with:

<new code>
```

Again, replacement length is unrestricted.

This is required for code that cannot conveniently be addressed semantically.

---

# 9. Revision safety

Line ranges become dangerous once the workspace changes.

Every mutable workspace therefore needs a revision.

For example:

```text
replace_range(
    file = "session.ts",
    lines = 123..128,
    expected_revision = "r184",
    content = ...
)
```

If the file changed since the agent inspected it:

```text
EDIT_REJECTED

reason:
  stale revision

agent saw:
  r184

current:
  r187
```

The agent must then inspect the new state rather than accidentally changing the wrong source region.

This becomes essential later if multiple agents can operate on related workspaces.

---

# 10. Diagnostics-on-edit

This should be a defining Jade behavior.

> **An edit must never return only "success."**

After every modification Jade should immediately perform all validation that fits within a small latency budget.

Conceptually:

```text
Agent
  │
  │ replace function
  ▼
Jade
  │
  ├── write change
  ├── parse
  ├── syntax validation
  ├── LSP diagnostics
  ├── fast linting
  ├── detect changed symbols
  └── schedule expensive validation
  │
  ▼
Agent
```

A response could be:

```text
EDIT_APPLIED

revision:
  r184 → r185

changed:
  SessionManager.refreshSession

diff:
  +14 -9

parser:
  OK

diagnostics:

  ERROR session.ts:112
    Property 'expiresAt' does not exist on type RefreshToken.

  WARNING session.ts:119
    'now' is declared but never used.

background:
  typecheck #293 running
  affected-tests #294 running
```

The model can immediately repair the problem.

Without Jade the interaction may require:

```text
LLM → edit
LLM → run lint
LLM → understand lint output
LLM → edit
```

With Jade:

```text
LLM → edit
       ↳ diagnostics included
LLM → corrected edit
```

One complete model turn can disappear.

At scale that matters considerably.

---

# 11. Asynchronous validation

Not every check belongs in the synchronous edit path.

Jade should distinguish between **fast feedback** and **background validation**.

### Immediate

Typically:

```text
parse
syntax
incremental LSP diagnostics
fast lint checks
```

### Asynchronous

Typically:

```text
full typecheck
compilation
unit tests
affected tests
integration tests
coverage
larger builds
```

An agent should not sit idle while a 30-second test suite executes.

Instead:

```text
Agent                    Jade

 replace ---------------->

         <--------------- EDIT_OK
                          diagnostics
                          tests #281 running

 continue investigation

         <--------------- EVENT #281
                          test failed
```

For harnesses without asynchronous event delivery:

```text
events(after_cursor)
```

provides the fallback.

---

# 12. Structured test output

Jade should not normally give the LLM raw test-runner stdout.

Instead:

```text
TEST_RESULT #281

status:
  FAILED

duration:
  3.8s

tests:
  47 passed
  1 failed

failed:
  SessionManagerTest.refreshesExpiredToken

assertion:
  expected: false
  actual: true

relevant stack:
  SessionManager.refreshSession    session.ts:108
  AuthService.authenticate         auth.ts:217
```

Raw output remains available through explicit expansion:

```text
expand_test_output(#281)
```

This is progressive disclosure applied to execution output.

---

# 13. Changed-set awareness

A normal IDE always gives a developer some awareness of what has changed.

Jade should do the same.

```text
changes()
```

might return:

```text
3 files changed
4 symbols modified
+62 -31

src/auth/session.ts
  SessionManager.refreshSession     modified

src/auth/token.ts
  RefreshToken                      modified

tests/session.test.ts
  refreshExpiredToken              added
  refreshValidToken                added
```

The agent can then request:

```text
expand change SessionManager.refreshSession
```

to obtain the textual diff.

Jade should therefore treat a **change set as a first-class object**, not merely as raw Git output.

---

# 14. North Star inspection capabilities

Over time Jade should provide:

### Structural inspection

```text
workspace tree
directory outline
file outline
symbol outline
expand/collapse
source ranges
```

### Search

```text
exact text search
regex search
symbol search
semantic search
structural search
```

### Code intelligence

```text
go to definition
find references
callers
callees
type information
implementations
interfaces
inheritance
overrides
dependency relationships
```

### Context intelligence

Eventually Jade should be capable of producing a **context view**:

```text
context(
  target = SessionManager.refreshSession,
  purpose = "modify"
)
```

which could assemble:

```text
implementation

relevant types:
  Session
  RefreshToken

direct callers:
  AuthService.authenticate
  SessionController.refresh

relevant tests:
  SessionManagerTest.*

current diagnostics:
  none

recent change:
  modified 12 days ago
```

This is effectively an agent-native replacement for the collection of panels a human IDE presents simultaneously.

---

# 15. North Star modification capabilities

Long term:

```text
replace symbol
replace range

insert before symbol
insert after symbol

create file
delete file
move file

delete symbol
rename symbol

organize imports

apply compiler/LSP code action

change signature

extract method

semantic refactoring
```

Where deterministic tooling can safely make the modification, Jade should prefer deterministic tooling over asking an LLM to regenerate code.

---

# 16. North Star validation capabilities

Eventually:

```text
parser diagnostics
LSP diagnostics
linting

typecheck
compile
build

unit tests
affected tests
integration tests

coverage
runtime diagnostics

formatter
static analysis
security scanners
```

All output should follow Jade's rule:

> **Return the conclusion first. Raw output is expandable.**

---

# 17. North Star runtime/debugging capabilities

A later Jade version should expose debugger information in an agent-native form.

Instead of reproducing a visual debugger:

```text
debug test foo
```

could return:

```text
exception:
  InvalidTokenError

stack:
  Session.refresh()             Session.ts:93
  AuthService.authenticate()    AuthService.ts:144
  LoginController.login()       LoginController.ts:61
```

Then:

```text
inspect_frame(0)
```

might expose only relevant locals.

This belongs in the North Star but **not the MVP**.

---

# 18. North Star history capabilities

Source history should eventually also become semantic.

Rather than:

```text
git log -p
```

Jade could provide:

```text
history(SessionManager.refreshSession)
```

and return only commits that affected that symbol.

Likewise:

```text
blame(symbol)
history(symbol)
show_change(symbol, commit)
```

This allows an agent to understand why a piece of code exists without searching large Git histories.

---

# 19. Architecture

The proposed North Star architecture is:

```text
                           Agent Harness
                                │
                                │ Jade Protocol
                                ▼
┌──────────────────────────────────────────────────────────┐
│                         JADE                             │
│                                                          │
│  ┌─────────────────┐      ┌───────────────────────────┐ │
│  │ Workspace       │      │ Code Intelligence        │ │
│  │ Manager         │      │                           │ │
│  │                 │      │ parser / symbols         │ │
│  │ revisions       │      │ LSP                      │ │
│  │ worktrees       │      │ search                   │ │
│  │ checkpoints     │      │ relationships            │ │
│  └────────┬────────┘      └────────────┬──────────────┘ │
│           │                            │                 │
│           │       ┌────────────────────┘                 │
│           ▼       ▼                                      │
│      ┌─────────────────┐                                 │
│      │ Edit Engine     │                                 │
│      │                 │                                 │
│      │ symbol edits    │                                 │
│      │ range edits     │                                 │
│      │ diff            │                                 │
│      └────────┬────────┘                                 │
│               │                                          │
│               ▼                                          │
│      ┌─────────────────────┐                             │
│      │ Validation Manager  │                             │
│      │                     │                             │
│      │ diagnostics         │                             │
│      │ build/typecheck     │                             │
│      │ test jobs           │                             │
│      │ event queue         │                             │
│      └────────┬────────────┘                             │
│               │                                          │
│               ▼                                          │
│          Event Stream                                    │
│                                                          │
└───────────────────┬──────────────────────────────────────┘
                    │
                    ▼
              Git Workspace
```

---

# 20. Architectural components

## Workspace Manager

Owns:

* repository checkout;
* isolated working tree;
* current revision;
* file revisions;
* checkpoints;
* changed set;
* revert operations.

For the MVP, a **Git worktree per agent/task** is a practical isolation mechanism.

---

## Parser / structural index

Maintains:

* files;
* symbols;
* symbol kinds;
* signatures;
* source ranges;
* parent/child relationships.

For the first implementation, **Tree-sitter** is a strong fit for this layer.

It provides parsers for all three initial languages and makes incremental structural parsing possible.

---

## Language Intelligence Adapter

Each supported language gets an adapter.

The conceptual interface remains language-neutral:

```text
outline
diagnostics
definition
references
types
format
test
```

but adapters integrate appropriate language tooling.

### TypeScript

Potential integrations:

```text
Tree-sitter TypeScript
TypeScript language service / LSP
ESLint or equivalent
project-specific test command
```

### Go

```text
Tree-sitter Go
gopls
go compiler / go vet
go test
```

### Rust

```text
Tree-sitter Rust
rust-analyzer
cargo check
clippy
cargo test
```

The architecture must not expose implementation-specific concepts such as `gopls` to agents.

An agent sees:

```text
diagnostics()
```

not:

```text
run_gopls()
```

---

# 21. Language support strategy

## Phase 1 languages

Jade initially supports:

1. **TypeScript**
2. **Go**
3. **Rust**

These three are useful because they exercise different compiler and ecosystem characteristics while all having strong machine-readable tooling.

## Later

Planned adapters:

```text
Java
Python
JavaScript
C#
Ruby
Scala
...
```

JavaScript should likely reuse substantial parts of the TypeScript adapter.

The key architectural requirement is:

> **Jade's protocol must remain language-neutral even though its adapters are language-specific.**

---

# 22. Jade protocol

The public API should remain small and composable.

Avoid building dozens of narrowly named tools such as:

```text
show_function
show_class
show_method
```

Those are all instances of:

```text
read(target)
```

A conceptual API could be:

## Inspect

```text
workspace()
outline(target)
read(target)
search(query, mode)
references(symbol)
relations(symbol, relation_type)
```

## Modify

```text
replace(target, content)
insert(target, position, content)
delete(target)
rename(symbol, new_name)
```

## Validate

```text
diagnostics(scope)
test(scope)
build(scope)
events(cursor)
```

## State

```text
changes()
diff(target?)
checkpoint()
revert(target?)
```

---

# 23. Target model

Most APIs operate on a generic target.

Examples:

```text
File:
  src/auth/session.ts
```

```text
Symbol:
  src/auth/session.ts::SessionManager.refreshSession
```

```text
Range:
  src/auth/session.ts
  revision r183
  lines 123..128
```

```text
Change set:
  changeset-73
```

This keeps the API small while preserving precision.

---

# 24. Transport

Jade should not become tightly coupled to one agent framework.

The internal Jade API should therefore be transport-independent.

Possible adapters include:

```text
MCP
native SDK
local RPC
HTTP/gRPC
CLI
```

For an MVP, an **MCP façade is reasonable** because existing agent harnesses can consume it easily.

However:

> MCP should be a transport adapter around Jade, not Jade's internal architecture.

This keeps Jade usable by future harnesses that may use a different tool protocol.

---

# 25. Shell strategy

Jade should not prohibit shell execution.

Unknown repositories inevitably contain:

* custom scripts;
* generators;
* unusual build systems;
* legacy tooling;
* one-off debugging workflows.

The architecture should instead encourage:

```text
               Operation required
                       │
                       ▼
          Jade primitive available?
               /               \
             yes               no
              │                 │
             Jade              Shell
```

The long-term goal is that normal source-code work happens through Jade while shell use becomes exceptional.

---

# 26. MVP objective

The MVP must answer one question:

> **Does an agent using Jade complete real software-engineering tasks more efficiently and reliably than the same agent using conventional shell and file tools?**

The MVP should therefore avoid attractive features that don't help answer that question.

---

# 27. MVP scope

## Supported languages

Exactly:

```text
TypeScript
Go
Rust
```

No additional languages until the architecture and benchmark demonstrate value.

---

# 28. MVP — inspection

Implement:

```text
workspace_tree()

outline(file)

read_symbol(symbol)

read_range(file, start_line, end_line)

search_text(query)

search_symbol(query)
```

### `outline(file)`

This is probably the most important MVP inspection primitive.

Example:

```text
src/server/router.ts

imports ×12

type RouteConfig                       L17-L28

class Router                           L31-L291
  constructor(...)                    L37-L49
  register(...)                       L51-L91
  dispatch(...)                       L93-L183
  handleError(...)                    L185-L221

function normalizePath(...)            L293-L326
```

No method implementation is returned until requested.

---

# 29. MVP — editing

Implement:

```text
replace_symbol(symbol, new_code)

replace_range(file, revision, start_line, end_line, new_code)

create_file(path, content)

delete_file(path)
```

The first two are the critical operations.

### Symbol replacement

```text
replace_symbol(
  SessionManager.refreshSession,
  <replacement>
)
```

Jade:

1. resolves the symbol;
2. verifies its current revision;
3. replaces its source region;
4. reparses the file;
5. updates symbol locations;
6. determines changed symbols;
7. generates the compact diff;
8. calculates immediate diagnostics;
9. schedules configured background validation.

---

# 30. MVP — edit response

Every edit returns a structured state transition.

Example:

```text
EDIT_APPLIED

revision:
  r43 → r44

target:
  SessionManager.refreshSession

change:
  +17 -12

changed symbols:
  SessionManager.refreshSession

parser:
  OK

diagnostics:

  introduced:
    ERROR session.ts:114
      Cannot assign string to TokenId.

  resolved:
    ERROR session.ts:107
      Unknown identifier expiry.

background jobs:
  typecheck #928
  tests #929
```

This behavior is not optional polish.

It is central to the Jade hypothesis.

---

# 31. MVP — immediate diagnostics

Every edit should trigger:

```text
structural parse
syntax errors
incremental language-server diagnostics
fast configured lint checks
```

The synchronous pipeline needs a latency budget.

If a diagnostic provider cannot reliably execute within that budget, it becomes asynchronous.

The product behavior is more important than exactly which tool runs synchronously.

---

# 32. MVP — asynchronous validation

Implement a small job system.

Initially support:

```text
typecheck / check
tests
build command
```

depending on language/project configuration.

An edit may start:

```text
job-173 typecheck
job-174 tests
```

The agent receives completion through:

```text
events(cursor)
```

or push events when supported by the harness.

---

# 33. MVP — tests

Do **not** attempt sophisticated affected-test prediction initially.

Start with configurable scopes:

```text
run_test(test)

run_tests_for_file(file)

run_changed_tests()

run_project_tests(project)
```

`run_changed_tests()` can initially use conservative project-specific rules.

Advanced test selection can come later using:

```text
dependency graphs
coverage data
historical execution
call relationships
```

---

# 34. MVP — structured execution output

Test and build output should be summarized.

Example:

```text
TEST_RESULT #929

FAILED

18 passed
2 failed

failures:

1. refreshExpiredToken
   session.test.ts:183

   expected:
     401

   received:
     500

2. refreshWithoutUser
   session.test.ts:244

   panic:
     user must not be nil
```

Raw logs:

```text
read_job_output(#929)
```

are available only when requested.

---

# 35. MVP — workspace state

Implement:

```text
changes()

diff(target?)

checkpoint()

revert(target?)
```

The change representation is semantic before textual.

Example:

```text
CHANGESET

2 files changed
3 symbols modified

session.ts
  SessionManager.refreshSession    modified

token.ts
  RefreshToken.isExpired           modified

session.test.ts
  refreshExpiredToken              added
```

The agent then expands only what it needs.

---

# 36. MVP architecture

A deliberately compact MVP can look like:

```text
                     Agent
                       │
                  MCP / API
                       │
                       ▼
              ┌────────────────┐
              │      Jade      │
              └───────┬────────┘
                      │
        ┌─────────────┼──────────────┐
        │             │              │
        ▼             ▼              ▼
 Workspace       Structural      Validation
 Manager         Code Index       Manager
        │             │              │
        │        Tree-sitter         ├─ LSP
        │        ripgrep             ├─ lint/check
        │                            └─ tests
        │
        ▼
   Git worktree
```

No distributed architecture is required initially.

---

# 37. Suggested internal modules

A reasonable codebase structure would be:

```text
jade/
├── workspace/
│   ├── repository
│   ├── revisions
│   ├── worktrees
│   └── checkpoints
│
├── code/
│   ├── parser
│   ├── symbols
│   ├── outline
│   └── search
│
├── edit/
│   ├── replace
│   ├── ranges
│   └── diff
│
├── diagnostics/
│   ├── parser
│   ├── lsp
│   └── lint
│
├── jobs/
│   ├── runner
│   ├── tests
│   ├── builds
│   └── events
│
├── languages/
│   ├── typescript
│   ├── go
│   └── rust
│
└── transport/
    ├── mcp
    └── internal-api
```

---

# 38. Core implementation language

My default recommendation would be **Go for the Jade runtime**, unless there is a strong existing team reason to choose something else.

The runtime primarily needs:

* filesystem management;
* process supervision;
* LSP process communication;
* Git orchestration;
* concurrency;
* asynchronous jobs;
* JSON/RPC handling;
* a simple distributable binary.

Go fits that profile well and reduces implementation complexity.

Rust would also be technically strong, but Jade does not initially have a problem where memory safety or zero-cost abstractions justify taking on additional implementation complexity.

The language adapters remain independent of this choice.

---

# 39. MVP technology choices

The implementation should heavily reuse deterministic tooling.

```text
Structure:
  Tree-sitter

Text search:
  ripgrep

Source control:
  Git / Git worktrees

TypeScript:
  TypeScript language tooling
  configured linter
  configured project test runner

Go:
  gopls
  go tooling
  go test

Rust:
  rust-analyzer
  cargo check
  clippy where appropriate
  cargo test
```

Jade should orchestrate these systems and **normalize their information**, not rebuild them.

---

# 40. North Star capabilities explicitly excluded from MVP

Do not initially build:

* vector embeddings;
* semantic search;
* complete call graphs;
* full dependency graphs;
* debugger integration;
* semantic Git history;
* coverage-driven affected-test inference;
* AI-generated repository summaries;
* automatic complex refactoring;
* cross-repository indexing;
* multi-agent concurrent workspace editing;
* remote execution;
* distributed build caches;
* elaborate graphical UI;
* broad language support;
* custom compiler/language-server implementations.

They are valid North Star features.

They are bad MVP features.

---

# 41. MVP execution flow

A normal task should eventually look roughly like this.

### 1. Agent receives task

```text
Fix refresh-token expiration handling.
```

### 2. Agent searches

```text
search_symbol("refresh")
```

Jade returns relevant symbols.

### 3. Agent inspects a file structurally

```text
outline("src/auth/session.ts")
```

Jade returns signatures, not 1,000 lines of source.

### 4. Agent expands a function

```text
read_symbol(SessionManager.refreshSession)
```

Only the implementation is returned.

### 5. Agent inspects references

North Star:

```text
references(SessionManager.refreshSession)
```

For MVP this may initially be supplied through available language-server support where reliable.

### 6. Agent replaces implementation

```text
replace_symbol(
    SessionManager.refreshSession,
    <new implementation>
)
```

### 7. Jade immediately responds

```text
edit accepted

parser OK

1 diagnostic:
  wrong TokenId type

tests #419 started
```

### 8. Agent fixes diagnostic

No explicit lint/compiler round trip was required.

### 9. Jade emits test event

```text
TEST_RESULT #419

17 passed
1 failed
```

### 10. Agent inspects only failed test

The rest of the test output never enters the model context.

That entire sequence represents the value proposition of Jade.

---

# 42. Product metrics

Jade should not be evaluated primarily on API elegance or parsing performance.

It should be benchmarked by what happens to an agent.

## Primary metrics

### Tokens per successful engineering task

```text
total model input + output tokens
────────────────────────────────
      successful tasks
```

This directly measures the value of progressive disclosure.

### LLM/tool round trips per successful task

Diagnostics-on-edit and automatic validation should materially reduce this.

---

# 43. Secondary metrics

Track:

| Metric                        | Desired effect |
| ----------------------------- | -------------: |
| Engineering task success rate |              ↑ |
| Input tokens/task             |             ↓↓ |
| Tool calls/task               |             ↓↓ |
| LLM turns/task                |             ↓↓ |
| Cost/successful task          |             ↓↓ |
| Wall-clock time/task          |              ↓ |
| Invalid edits                 |             ↓↓ |
| Repeated file reads           |             ↓↓ |
| Full-file reads               |             ↓↓ |
| Redundant test executions     |              ↓ |
| Regressions introduced        |              ↓ |

The important constraint is:

> **Token reduction is not valuable if task success deteriorates.**

Jade should reduce context while preserving or improving agent correctness.

---

# 44. Benchmark strategy

A benchmark harness should be built alongside Jade rather than after it.

Take the same:

```text
model
agent harness
repository
task
starting commit
```

and compare:

### Control

```text
Agent
+
shell
+
normal filesystem tools
```

against:

### Jade

```text
same Agent
+
Jade
```

Collect:

```text
completion
tokens
turns
tool calls
wall time
cost
tests
final diff quality
```

Without this A/B setup it will be very easy to build sophisticated infrastructure without proving that it improves agent performance.

---

# 45. Implementation phases

## Phase 0 — benchmark foundation

Before substantial Jade development:

* define representative TypeScript, Go and Rust repositories;
* define engineering tasks;
* establish baseline agent results using existing shell/file tooling;
* capture tokens, turns, latency and success.

This creates the baseline Jade must beat.

---

## Phase 1 — structural workspace

Build:

```text
workspace manager
Git worktree isolation
Tree-sitter parsing
workspace tree
file outline
symbol addressing
read symbol
read range
ripgrep search
```

At this point progressive disclosure can already be benchmarked.

---

## Phase 2 — mutation

Add:

```text
revision tracking
replace symbol
replace range
create/delete file
changed-symbol detection
compact diff
changes()
checkpoint/revert
```

The agent now has a complete basic editing loop.

---

## Phase 3 — diagnostics-on-edit

Integrate:

```text
TypeScript language intelligence
gopls
rust-analyzer

parser diagnostics
language diagnostics
fast lint feedback
```

Every edit now returns immediate deterministic feedback.

This is likely the point where Jade should begin showing significant reductions in agent turns.

---

## Phase 4 — asynchronous validation

Add:

```text
job runner
event stream
typecheck/check
test execution
structured result summarization
raw-log expansion
```

Now the agent can continue reasoning while validation executes.

---

## Phase 5 — evaluate MVP

Run the A/B benchmark.

Do not immediately add more features.

Determine whether:

```text
tokens ↓
turns ↓
time ↓
success ≥ baseline
```

If those effects are not clearly visible, understand why before expanding scope.

---

# 46. Major engineering risks

## Language abstraction leakage

TypeScript, Go and Rust have substantially different semantic models.

Trying to force every concept into one universal AST will create a poor abstraction.

Jade should normalize common operations while allowing language-specific metadata where required.

---

## Symbol identity after edits

A method can:

* move;
* be renamed;
* be deleted;
* split into several functions.

Stable symbol identity is therefore not trivial.

MVP symbol IDs can be revision-scoped if necessary rather than pretending identities are permanently stable.

---

## LSP reliability

Language servers:

* crash;
* become stale;
* produce delayed diagnostics;
* behave differently between repositories.

Jade needs lifecycle management and a clear distinction between:

```text
no errors
```

and:

```text
diagnostics unavailable
```

Those are not equivalent.

---

## Large monorepositories

Project discovery, language-server initialization and tests can become expensive.

MVP should initially support well-defined project roots rather than attempting perfect automatic monorepo understanding.

---

## Generated code

Generated files should ideally be marked so agents do not casually edit them.

This should eventually be part of workspace metadata.

---

## Arbitrary execution

Tests and build scripts execute repository-controlled code.

Therefore Jade validation is not inherently safe simply because the source editing API is structured.

Sandboxing belongs in the surrounding Agent Hangar / execution infrastructure and needs an explicit security model.

---

# 47. Product principles to keep

I would put the following rules in the repository README from day one.

### Rule 1

**Structure before source.**

Return outlines before entire implementations whenever possible.

### Rule 2

**Deterministic tools before LLM reasoning.**

If the parser, compiler, LSP or formatter already knows the answer, use it.

### Rule 3

**Every edit returns consequences.**

An edit response includes diagnostics, changed symbols and validation state.

### Rule 4

**Conclusions before logs.**

Summarize first. Raw output is explicitly expanded.

### Rule 5

**Semantic operations before textual operations.**

Prefer `replace(symbol)` over raw line manipulation when possible.

### Rule 6

**Textual escape hatches remain available.**

Agents need ranges and eventually shell access for cases Jade cannot model.

### Rule 7

**State must be explicit.**

Edits operate against known revisions.

### Rule 8

**Long-running work is asynchronous.**

Do not block an agent unnecessarily on tests and builds.

### Rule 9

**Jade remains model- and harness-independent.**

Claude, OpenAI, Grok or future agents should receive the same development interface.

### Rule 10

**Measure agent outcomes, not infrastructure sophistication.**

A feature belongs in Jade because it demonstrably helps agents engineer software—not because IDEs traditionally have it.

---

# 48. MVP definition of done

The Jade MVP is complete when an external agent can take a TypeScript, Go or Rust repository and reliably perform this sequence:

```text
1. Create isolated workspace

2. Inspect repository structure

3. Inspect a source file without reading its complete contents

4. Expand a selected function/type/method

5. Search files and symbols

6. Replace a complete symbol

7. Replace an arbitrary source range

8. Receive parser/language/linter feedback in the edit response

9. Observe the resulting semantic change set

10. Start tests without blocking

11. Receive structured asynchronous test results

12. Inspect raw output only when necessary

13. Checkpoint or revert changes
```

And, critically:

> **The same benchmark tasks show a measurable reduction in model context and/or model turns compared with conventional shell/file tooling without reducing task success.**

That is the point at which Jade has demonstrated that it is a product rather than merely another tooling abstraction.

---

# 49. One-sentence internal pitch

For explaining Jade internally:

> **Jade gives coding agents the equivalent of the structural navigation, precise editing and continuous feedback that a modern IDE gives human developers, but redesigns those capabilities around LLM context, latency and autonomous execution rather than a graphical user interface.**

Or, more technically:

> **Jade is the language-aware workspace runtime between an agent and its repository, providing progressive code disclosure, precise mutation and automatic validation while minimizing tokens and agent round trips.**

The second is probably the better long-term product definition.
