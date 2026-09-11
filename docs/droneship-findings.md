# Droneship Findings for JADE

This document records source-grounded findings from /Users/julianamelung/Projects/droneship.

The consultant memo in [reference_droneship.md](../reference_droneship.md) is treated as non-authoritative.
Patterns below were selected from code behavior, not claims.

## Reuse Now

- Event bus with normalized runtime events.
- Workspace freshness that compares index snapshots with current HEAD and dirty paths.
- Progressive disclosure: outline first, then symbol-level source expansion.
- Bounded symbol reads to keep context usage predictable.
- Language-neutral service interfaces with language-specific adapters.

## Reuse Later

- Centrality-ranked repository maps.
- Hybrid search blending exact/fuzzy/semantic evidence.
- Branch overlays and worktree-universe optimization.
- Decisive build/test output summarization with full-log fallback.

## Avoid Carrying Forward

- Regex-only symbol boundaries for edit operations.
- Assuming index freshness from branch name alone.
- Worktree placement inside the project directory for multi-agent execution.

## JADE Direction

JADE remains its own runtime with Go-first implementation and language-neutral protocol semantics.
Droneship contributes proven runtime patterns, while JADE replaces approximation-heavy internals with parser-accurate indexing and edit safety over time.
