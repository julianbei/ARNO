# JADE Dogfooding Comparison Report

Goal: compare baseline shell/file workflow against JADE-first workflow on the same feature tasks.

## Summary Table

| Task | Baseline result | JADE-first result | Delta turns | Delta tool calls | Delta time | Delta tokens | Verdict |
| --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| Task 1 | Manual ambiguity detection via grep/read | Structured exact/ambiguous/not_found response + candidate IDs | 0 | +2 | +~9m | +small | Better correctness semantics |
| Task 2 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| Task 3 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| Task 4 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |
| Task 5 | TBD | TBD | TBD | TBD | TBD | TBD | TBD |

## Findings

### What improved

- Symbol resolution behavior is explicit and machine-usable for exact, ambiguous, and not_found outcomes.
- Candidate symbol IDs are returned for ambiguous queries, removing guesswork from caller workflows.

### What regressed

- No runtime regressions observed.
- Implementation time increased versus baseline because this run included feature development.

### Neutral outcomes

- Build/test status remained green in both modes.

## Recommendation

- Continue MVP as-is | Adjust scope | Rework core primitives

## Evidence

- Run logs: [docs/dogfooding-run-log.md](dogfooding-run-log.md)
- Task definitions: [docs/dogfooding-feature-tasks.md](dogfooding-feature-tasks.md)
