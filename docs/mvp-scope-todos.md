# JADE MVP Scope TODOs

Goal: use JADE itself to implement future JADE features and measure whether the runtime reduces agent context and round trips during real development tasks.

## MVP Acceptance Goal

- [ ] An agent can implement non-trivial feature work in this repository using JADE primitives first, shell second.
- [ ] The same feature task shows lower context usage and fewer tool/LLM turns versus a file+shell baseline.
- [ ] No drop in feature correctness (build/test pass and code review quality remains stable).

## Scope Boundaries

In scope:

- [ ] Inspect: outline, read_symbol, read_range, search_text, search_symbol.
- [ ] Modify: replace_symbol, replace_range, create_file, delete_file.
- [ ] Validate: immediate diagnostics + async jobs for tests/checks.
- [ ] State: changes, diff summaries, event polling, checkpoint/revert.
- [ ] Initial languages: TypeScript, Go, Rust.

Out of scope for MVP:

- [ ] Full semantic call graph accuracy across all languages.
- [ ] Debugger integration.
- [ ] Distributed execution and remote caches.
- [ ] Broad language expansion beyond TypeScript/Go/Rust.

## TODO Execution Plan

### A. Transport and API

- [x] Finalize internal API request/response contracts for inspect/modify/validate/state.
- [x] Implement MCP transport endpoints that map 1:1 to internal API operations.
- [x] Add events(after_cursor, limit) polling endpoint with stable cursor semantics.

### B. Inspection

- [ ] Replace regex-only symbol boundaries with tree-sitter-backed ranges.
- [ ] Add file outlines that separate imports, types, classes, functions.
- [ ] Add symbol disambiguation behavior (exact, ambiguous, not found).
- [ ] Add conservative repository map with token budget and omission reporting.

### C. Mutation and Safety

- [ ] Enforce expected_revision checks on all mutable operations.
- [ ] Return structured edit consequences: changed symbols, diagnostics, jobs.
- [ ] Add checkpoint() and revert(target?) primitives.

### D. Validation

- [ ] Implement immediate diagnostics adapters:
  - [ ] TypeScript: tsserver/tsc + lint
  - [ ] Go: gopls/go vet
  - [ ] Rust: rust-analyzer/cargo check
- [ ] Add async job runner for tests/build/check.
- [ ] Summarize decisive failure lines before raw logs.

### E. Dogfooding Harness

- [ ] Define 3-5 feature tasks to build with JADE itself.
- [ ] Record baseline metrics using shell/file workflow.
- [ ] Re-run same tasks through JADE primitives.
- [ ] Capture: tokens, tool calls, turns, wall-clock, pass/fail.
- [ ] Publish per-task comparison in a markdown report.

### F. Documentation

- [ ] Keep README current with implemented MVP capabilities.
- [ ] Maintain a living changelog of which TODOs are complete.
- [ ] Document known limitations and confidence levels.

## Definition of Done for This MVP

- [ ] JADE can be used as the primary interface to implement at least two new JADE features end-to-end.
- [ ] Measured improvements exist in context/turn efficiency on repeated feature tasks.
- [ ] Structured edit feedback and async validation are stable enough for daily use.
