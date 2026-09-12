# jade response style

The acceptance bar for every jade tool response. Adding a tool means adding
a renderer that follows these rules.

## Why this exists

The consumer of a jade response is a language model, not a parser. MCP
already delivers tool results as text, so JSON is a tax paid on every call:
braces, quotes, repeated field names, indentation, and nulls for fields
nobody populated.

This is measured, not asserted. The `jade-bench` harness (`cmd/jade-bench`)
asks seven realistic questions and answers each twice — once through jade,
once through the shell command an agent would otherwise use:

| | tokens | vs shell |
|---|---|---|
| JSON responses | 8,317 | 5.63x |
| rendered text | 1,270 | 0.85x |

jade went from costing **5.6x more than grep and sed** to **cheaper than
them**, with no capability removed. That 6.5x reduction is the entire
justification for this document. Re-run `go run ./cmd/jade-bench` after any
change to response shape.

## The rules

**1. Decisive content first, on the first line where possible.**
An agent that stops reading after one line should still have the answer.
`references` returns locations before its caveat. `inspect` leads with an
ambiguity when the symbol did not resolve, because an ambiguous symbol *is*
the answer and an outline below it is unusable.

**2. Omit empty, null and zero-valued fields entirely.**
Never serialize a field as evidence of its own absence. A null `Outline`,
six null `Sections` and an empty `Resolve` on every read were a large share
of the 5.63x above.

**3. Never render to nothing.**
An empty response leaves the agent unable to distinguish a result from a
dropped call. Degenerate cases get an explicit `no changes`, `(no job)`,
`(empty)`. This rule exists because three renderers violated it and the
coverage guard caught all three.

**4. Source code is emitted as source code.**
Not as a quoted string with `\n` and `\t` escapes. This is the single
largest saving in the renderer.

**5. Say a count, not a list, when the list is available elsewhere.**
`Freshness` became `drifted: 35 files`; the paths live in `changes()`, which
exists to report exactly that. Two byte-identical 35-element arrays rode
along on every single read before this.

**6. Never send the same data twice.**
`changes()` carried `Paths` *and* `Files`, where `Paths` was exactly the
`Path` field of every `Files` entry.

**7. Keep truncation visible.**
Clamped output always states how many bytes were dropped. A reader who
cannot see that bytes are missing will read the remainder as complete —
worse than returning less.

**8. Structure only where a caller must branch on it.**
Labels like a reference's `(unique-name)` confidence earn their place
because behavior depends on them. Field names for their own sake do not.

**9. Stable ordering.**
Sort anything derived from a map before rendering. Unstable output makes
responses undiffable and re-reads look like changes.

## Enforcement

`internal/render/coverage_test.go` parses `internal/protocol/types.go` for
every exported `*Response` struct and fails if one has no renderer, and
separately fails any renderer that returns empty for a zero value. A new
response type cannot ship as raw JSON by being forgotten — the guard has to
be told about it, and it checks its own list back against the source.

## Escape hatch

`JADE_JSON=1` restores the JSON wire format for a consumer that genuinely
parses responses. Types without a renderer fall back to JSON automatically
rather than to a guessed format, so an unhandled response degrades instead
of losing content.
