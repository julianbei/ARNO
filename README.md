# Jade — Julian's Agentic Development Environment

![Jade logo](logo-cropped.png)

**Status:** Initial concept / team briefing  
**Project name:** **Jade**  
**Expansion:** **Julian's Agentic Development Environment**  
**Initial languages:** TypeScript, Go, Rust  
**Later languages:** Java, Python, JavaScript, C#, Ruby, Scala, others

## Scaffold Status

The repository now includes an initial Go scaffold aligned with the architecture in [scope.md](scope.md):

- [cmd/jade/main.go](cmd/jade/main.go): runtime entrypoint and service wiring
- [internal/workspace](internal/workspace): workspace state and revision manager
- [internal/code](internal/code): structural code index placeholder
- [internal/edit](internal/edit): mutation service placeholders
- [internal/diagnostics](internal/diagnostics): immediate diagnostics abstraction
- [internal/jobs](internal/jobs): asynchronous job runner abstraction
- [internal/languages](internal/languages): language adapter registry and stubs
- [internal/transport/mcp](internal/transport/mcp) and [internal/transport/internalapi](internal/transport/internalapi): transport facades
- [internal/protocol/types.go](internal/protocol/types.go): shared protocol types
- [configs/jade.example.yaml](configs/jade.example.yaml): starter runtime config

Quick commands:

```sh
make fmt
make test
make build
```

---

## 1. Executive summary

Jade is an Agent Development Environment designed specifically for software-engineering agents.

The objective is to expose the information advantage of a modern IDE through an agent-native interface optimized for tokens, model turns, latency, and correctness.

Jade sits between the agent and the development workspace:

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

---

## 2. Problem

Current coding agents spend too much context and too many turns on deterministic workflows that modern IDEs already solve.

Main inefficiencies:

- Context inefficiency
- Round-trip inefficiency
- Semantic weakness
- Tool-output inefficiency

---

## 3. Product definition

Jade owns the agent's interaction with a software workspace and provides:

- **Inspect**: Efficient code understanding
- **Modify**: Precise code changes
- **Validate**: Immediate correctness feedback
- **State**: Coherent workspace change tracking

---

## 4. What Jade is not

Jade is **not**:

- an LLM
- an agent reasoning loop
- a CI system
- a source-control hosting system
- a graphical IDE
- a shell replacement

Jade is specifically the development environment runtime for agents.

---

## 5. North Star

> Jade is a stateful, language-aware development runtime that allows autonomous software-engineering agents to inspect, modify, and validate arbitrarily large codebases while consuming only the context necessary for the current task.

---

## 6. North Star design principles

### 6.1 Progressive disclosure

Return the minimum sufficient representation first (outline before full source, summary before raw logs).

---

## 7. Structural addressing

Lines remain useful, but symbols are first-class objects.

Example target:

```text
src/auth/session.ts::SessionManager.refreshSession
```

Core operations:

```text
read(symbol)
replace(symbol, new_code)
references(symbol)
callers(symbol)
history(symbol)
```

---

## 8. In-place replacement

Core edit primitive:

```text
replace function SessionManager.refreshSession with <new implementation>
```

Also support range replacement with unrestricted replacement length.

---

## 9. Revision safety

Mutable edits must include expected revision and reject stale edits.

---

## 10. Diagnostics-on-edit

Every edit returns consequences, not just “success”, including parse/lint/type diagnostics plus background validation scheduling.

---

## 11. Asynchronous validation

Separate:

- **Immediate** feedback: parse/syntax/incremental diagnostics/fast lint
- **Asynchronous** jobs: full typecheck/build/tests

Provide event stream or polling fallback.

---

## 12. Structured test output

Return concise structured results first (status, failed tests, relevant stack), with raw output expandable on demand.

---

## 13. Changed-set awareness

Treat change sets as first-class objects with semantic summaries before textual diffs.

---

## 14. North Star inspection capabilities

- Structural inspection
- Search (text/regex/symbol/semantic/structural)
- Code intelligence (definitions/references/callers/callees/types)
- Context intelligence for change-oriented task scope

---

## 15. North Star modification capabilities

Long-term: symbol/range edits, inserts, create/delete/move files, rename, code actions, signature/refactor flows.

---

## 16. North Star validation capabilities

Parser + LSP + lint + typecheck/build/tests/integration/static/security.

Rule: conclusions first, raw output expandable.

---

## 17. North Star runtime/debugging capabilities

Debugger-style structured runtime insight is in North Star, not MVP.

---

## 18. North Star history capabilities

Semantic history and blame at symbol level are North Star capabilities.

---

## 19. Architecture

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
│  │ revisions       │      │ parser / symbols         │ │
│  │ worktrees       │      │ LSP                      │ │
│  │ checkpoints     │      │ search                   │ │
│  └────────┬────────┘      └────────────┬──────────────┘ │
│           │                            │                 │
│           │       ┌────────────────────┘                 │
│           ▼       ▼                                      │
│      ┌─────────────────┐                                 │
│      │ Edit Engine     │                                 │
│      └────────┬────────┘                                 │
│               │                                          │
│               ▼                                          │
│      ┌─────────────────────┐                             │
│      │ Validation Manager  │                             │
│      └────────┬────────────┘                             │
│               │                                          │
│               ▼                                          │
│          Event Stream                                    │
└───────────────────┬──────────────────────────────────────┘
                    │
                    ▼
              Git Workspace
```

---

## 20. Architectural components

- Workspace Manager
- Parser / structural index (Tree-sitter)
- Language Intelligence Adapter

Language-neutral protocol, language-specific adapters.

---

## 21. Language support strategy

### Phase 1 languages

1. TypeScript
2. Go
3. Rust

Later: Java, Python, JavaScript, C#, Ruby, Scala, others.

---

## 22. Jade protocol

### Inspect

```text
workspace()
outline(target)
read(target)
search(query, mode)
references(symbol)
relations(symbol, relation_type)
```

### Modify

```text
replace(target, content)
insert(target, position, content)
delete(target)
rename(symbol, new_name)
```

### Validate

```text
diagnostics(scope)
test(scope)
build(scope)
events(cursor)
```

### State

```text
changes()
diff(target?)
checkpoint()
revert(target?)
```

---

## 23. Target model

Generic targetable objects:

- File
- Symbol
- Revision-scoped range
- Change set

---

## 24. Transport

Internal API is transport-independent. MCP is an adapter, not the architecture.

---

## 25. Shell strategy

Use Jade primitives when available; fallback to shell when needed.

---

## 26. MVP objective

Determine whether agents using Jade complete real engineering tasks more efficiently and reliably than shell/file-based workflows.

---

## 27. MVP scope

Exactly TypeScript, Go, Rust.

---

## 28. MVP — inspection

Implement:

```text
workspace_tree()
outline(file)
read_symbol(symbol)
read_range(file, start_line, end_line)
search_text(query)
search_symbol(query)
```

---

## 29. MVP — editing

Implement:

```text
replace_symbol(symbol, new_code)
replace_range(file, revision, start_line, end_line, new_code)
create_file(path, content)
delete_file(path)
```

---

## 30. MVP — edit response

Every edit returns structured revision transition + changed symbols + diagnostics + background jobs.

---

## 31. MVP — immediate diagnostics

Each edit triggers parse, syntax, language diagnostics, and fast lint feedback within a latency budget.

---

## 32. MVP — asynchronous validation

Provide job runner and event mechanism for typecheck/check/tests/build.

---

## 33. MVP — tests

Initial test scopes:

```text
run_test(test)
run_tests_for_file(file)
run_changed_tests()
run_project_tests(project)
```

---

## 34. MVP — structured execution output

Summarize test/build outcomes first; allow raw logs on explicit request.

---

## 35. MVP — workspace state

Implement:

```text
changes()
diff(target?)
checkpoint()
revert(target?)
```

Prefer semantic change summaries first.

---

## 36. MVP architecture

Compact architecture around workspace manager, structural index, and validation manager.

---

## 37. Suggested internal modules

```text
jade/
├── workspace/
├── code/
├── edit/
├── diagnostics/
├── jobs/
├── languages/
└── transport/
```

---

## 38. Core implementation language

Default recommendation: **Go** for Jade runtime.

---

## 39. MVP technology choices

Jade should heavily reuse deterministic tooling:

- **Structure:** Tree-sitter
- **Text search:** ripgrep
- **Source control:** Git / Git worktrees

Language tooling:

- **TypeScript:** TypeScript tooling, configured linter, configured test runner
- **Go:** gopls, Go tooling, go test
- **Rust:** rust-analyzer, cargo check, clippy where appropriate, cargo test

Jade orchestrates and normalizes these systems; it does not reimplement them.

---

## 40. North Star capabilities explicitly excluded from MVP

Do not initially build:

- vector embeddings
- semantic search
- complete call graphs
- full dependency graphs
- debugger integration
- semantic Git history
- coverage-driven affected-test inference
- AI-generated repository summaries
- automatic complex refactoring
- cross-repository indexing
- multi-agent concurrent workspace editing
- remote execution
- distributed build caches
- elaborate graphical UI
- broad language support
- custom compiler/language-server implementations

These are valid North Star items, but not MVP scope.

---

## 41. MVP execution flow

Target loop:

1. Agent receives task
2. Search symbol
3. Outline file
4. Read specific symbol
5. Inspect references (where reliable)
6. Replace symbol
7. Receive immediate diagnostics + job IDs
8. Fix diagnostics
9. Receive async test event
10. Expand only failed-test details

This sequence is Jade’s value proposition.

---

## 42. Product metrics

Primary metrics:

- Tokens per successful engineering task
- LLM/tool round trips per successful task

---

## 43. Secondary metrics

Track:

- Task success rate ↑
- Input tokens/task ↓↓
- Tool calls/task ↓↓
- LLM turns/task ↓↓
- Cost/successful task ↓↓
- Wall-clock time/task ↓
- Invalid edits ↓↓
- Repeated/full-file reads ↓↓
- Redundant test execution ↓
- Regressions introduced ↓

Constraint: token reduction is not useful if success rate drops.

---

## 44. Benchmark strategy

Run A/B benchmark using the same model/harness/repo/task/start commit:

- **Control:** shell + filesystem tooling
- **Jade:** same agent + Jade

Collect completion, tokens, turns, tool calls, wall time, cost, tests, and final diff quality.

---

## 45. Implementation phases

### Phase 0 — benchmark foundation
Baseline with existing shell/file tooling.

### Phase 1 — structural workspace
Workspace manager, worktree isolation, Tree-sitter, outlines, symbol/range read, ripgrep search.

### Phase 2 — mutation
Revisions, replace symbol/range, create/delete, changed symbols, compact diff, changes/checkpoint/revert.

### Phase 3 — diagnostics-on-edit
Integrate TS/gopls/rust-analyzer + parser/language/lint diagnostics.

### Phase 4 — asynchronous validation
Job runner, events, typecheck/check, tests, structured summaries, raw-log expansion.

### Phase 5 — evaluate MVP
Confirm tokens/turns/time decrease without reducing task success.

---

## 46. Major engineering risks

- Language abstraction leakage
- Symbol identity across edits
- LSP reliability and lifecycle handling
- Large monorepo startup/perf
- Generated code boundaries
- Arbitrary execution risks in test/build scripts

---

## 47. Product principles

1. **Structure before source.**
2. **Deterministic tools before LLM reasoning.**
3. **Every edit returns consequences.**
4. **Conclusions before logs.**
5. **Semantic operations before textual operations.**
6. **Textual escape hatches remain available.**
7. **State must be explicit.**
8. **Long-running work is asynchronous.**
9. **Jade remains model- and harness-independent.**
10. **Measure agent outcomes, not infrastructure sophistication.**

---

## 48. MVP definition of done

MVP is complete when an external agent can reliably:

1. Create isolated workspace
2. Inspect repository structure
3. Inspect file without full-file read
4. Expand selected symbol
5. Search files and symbols
6. Replace full symbol
7. Replace arbitrary range
8. Receive parser/language/linter feedback in edit response
9. Observe semantic change set
10. Start tests without blocking
11. Receive structured async test results
12. Inspect raw output only when needed
13. Checkpoint or revert

And benchmark evidence shows measurable context/turn reduction without degraded success.

---

## 49. One-sentence internal pitch

> Jade gives coding agents the equivalent of structural navigation, precise editing, and continuous feedback that modern IDEs give human developers—redesigned for LLM context, latency, and autonomous execution instead of GUI interaction.

Technical variant:

> Jade is the language-aware workspace runtime between an agent and its repository, providing progressive code disclosure, precise mutation, and automatic validation while minimizing tokens and round trips.
