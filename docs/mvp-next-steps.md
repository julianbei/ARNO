# JADE MVP Next Steps

## Implemented in this pass

- Root-aware workspace manager with revision tracking and git freshness checks.
- In-process event bus with non-blocking publish semantics.
- Progressive code inspection primitives:
  - file outline
  - symbol read with bounded expansion
- Edit responses that include immediate diagnostics and background job IDs.
- Job runner status model with started/completed events.

## Next build targets

1. Replace regex symbol extraction with tree-sitter-backed ranges for TypeScript, Go, and Rust.
2. Add transport endpoints for inspect/modify/validate/state operations.
3. Add decisive-line summarization for async job output.
4. Add repository map generation with stable ranking and token budget.
5. Add checkpoint and revert behavior over explicit revisions.
