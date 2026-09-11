# Tree-sitter Go Spike Notes

Date: 2026-09-11

Scope: Go symbol extraction path in [internal/code/index.go](../internal/code/index.go) with tree-sitter first and regex fallback.

## Parser Switch

Environment variable:

- JADE_GO_SYMBOL_PARSER=tree-sitter
- JADE_GO_SYMBOL_PARSER=regex

Behavior:

- For .go files, tree-sitter is used by default.
- If set to regex, the legacy regex extractor is forced.
- If tree-sitter extraction yields no symbols, JADE falls back to regex.

## Evidence from Fixture

Fixture:

- [docs/fixtures/go_treesitter_spike.go](fixtures/go_treesitter_spike.go)

Observed result:

- regex symbols: 2
- tree-sitter symbols: 4

Reason:

- Regex misses grouped type declarations (`type (...)`) where names are not on the same line as `type`.
- Tree-sitter captures each type_spec node and method/function nodes with parser-backed ranges.

## Known Limitations

- Current spike only uses tree-sitter for Go files.
- TypeScript and Rust remain regex-based in this branch.
- Symbol kind mapping is intentionally minimal (function, method, type, class-like struct).

## Next Step

- Extend parser-backed extraction coverage and tests for TypeScript and Rust while keeping fallback paths explicit.
