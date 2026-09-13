package code

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/lsp"
	"github.com/julianbei/jade/internal/pathguard"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/textutil"
)

const parserModeEnv = "JADE_GO_SYMBOL_PARSER"

// Symbol is the minimum structural unit surfaced to agents.
type Symbol struct {
	ID   string
	Kind string
	Name string
	Path string
	From int
	To   int
}

// OutlineSections groups declarations for progressive disclosure consumers.
type OutlineSections struct {
	Imports   []string
	Types     []Symbol
	Classes   []Symbol
	Functions []Symbol
	Methods   []Symbol
	Other     []Symbol
}

// Index provides lightweight structural inspection primitives.
// It intentionally prioritizes bounded context and deterministic results.
type Index struct {
	root string
	bus  *events.Bus

	// servers provides real language servers for references, rename and
	// semantic diagnostics. Nil is a supported state and means jade behaves
	// as it did before the LSP client existed: gopls CLI for Go, approximate
	// name matching elsewhere, rename refused. Every use of this field must
	// therefore tolerate nil rather than assume a server.
	servers *lsp.Manager

	cacheMu sync.RWMutex
	cache   map[string]fileSymbolCache

	// ignore caches what git ignores, for walks to skip.
	ignore ignoreCache
}

// fileSymbolCache holds the last-parsed symbols for a file, keyed against
// its modification time and size so a subsequent edit (mtime and/or size
// always changes on write) is detected without needing an explicit
// invalidation call from the edit path.
type fileSymbolCache struct {
	modTime time.Time
	size    int64
	symbols []Symbol
	mode    string
}

func NewIndex(root string, bus *events.Bus) *Index {
	return &Index{root: root, bus: bus, cache: make(map[string]fileSymbolCache)}
}

// UseLanguageServers attaches a language server manager. Separate from
// NewIndex so the three commands that build an Index can opt in individually:
// the benchmark deliberately does not, since a warm language server would
// measure something other than what it claims to.
func (i *Index) UseLanguageServers(manager *lsp.Manager) {
	i.servers = manager
}

// languageClient returns a started server for path's language, or false when
// there is none. The false case is ordinary, not exceptional.
func (i *Index) languageClient(ctx context.Context, path string) (*lsp.Client, string, bool) {
	if i.servers == nil {
		return nil, "", false
	}
	language := lsp.LanguageForPath(path)
	if language == "" {
		return nil, "", false
	}
	client, ok := i.servers.ClientFor(ctx, language)
	if !ok {
		return nil, language, false
	}
	spec, _ := lsp.SpecFor(language)
	if err := i.servers.Sync(client, path, spec.LanguageID); err != nil {
		return nil, language, false
	}
	return client, language, true
}

func (i *Index) Outline(path string) ([]Symbol, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(i.root, absolute)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}

	lines := splitLines(string(data))
	symbols, mode := i.symbolsForPath(absolute, rel, data, lines)

	if i.bus != nil {
		i.bus.Publish(events.Event{
			Type:   "INDEX_UPDATED",
			Entity: "code_index",
			Payload: map[string]string{
				"path":    rel,
				"mode":    mode,
				"symbols": fmt.Sprintf("%d", len(symbols)),
			},
		})
	}

	return symbols, nil
}

// grammarLanguages maps the extensions jade has a real tree-sitter grammar
// for. Anything absent here is served by the heuristic scanner.
var grammarLanguages = map[string]string{
	".go":    "go",
	".ts":    "typescript",
	".tsx":   "tsx",
	".rs":    "rust",
	".py":    "python",
	".rb":    "ruby",
	".java":  "java",
	".scala": "scala",
	".sc":    "scala",
	".js":    "javascript",
	".jsx":   "javascript",
	".mjs":   "javascript",
	".cjs":   "javascript",
}

// knownLanguages names extensions jade recognises but does not parse with a
// grammar, so the heuristic's note can say which language it is guessing at
// rather than the unhelpful "this file".
var knownLanguages = map[string]string{
	// Extensions with a real grammar live in grammarLanguages and must not be
	// repeated here: ParserFor consults that map first, so a duplicate entry
	// would be unreachable and would drift the moment one of them changed.
	".kt":    "kotlin",
	".swift": "swift",
	".c":     "c",
	".h":     "c",
	".cc":    "c++",
	".cpp":   "c++",
	".hpp":   "c++",
	".cs":    "c#",
	".php":   "php",
	".sh":    "shell",
	".sql":   "sql",
	".ex":    "elixir",
	".exs":   "elixir",
	".lua":   "lua",
	".pl":    "perl",
	".r":     "r",
	".dart":  "dart",
	".zig":   "zig",
}

// ParserFor describes how path's symbols were obtained. mode is the value
// symbolsForPath returned, so this reports what actually happened rather than
// what the extension suggests should have — a Go file whose tree-sitter parse
// failed and fell through to the heuristic is reported as heuristic.
func ParserFor(path string, mode string) protocol.ParserInfo {
	ext := strings.ToLower(filepath.Ext(path))

	if mode == "tree-sitter" {
		language := grammarLanguages[ext]
		return protocol.ParserInfo{Language: language, Parser: "grammar", Complete: true}
	}

	language := grammarLanguages[ext]
	if language == "" {
		language = knownLanguages[ext]
	}

	info := protocol.ParserInfo{Language: language, Parser: "heuristic"}
	switch {
	case language == "":
		info.Note = "no grammar for this file type — declarations were found by a text scan and some may be missing"
	case grammarLanguages[ext] != "":
		// A language jade *does* have a grammar for, which nonetheless did not
		// parse. Worth distinguishing: this usually means a syntax error, not
		// an unsupported language.
		info.Note = fmt.Sprintf("%s grammar did not parse this file (often a syntax error) — fell back to a text scan, some declarations may be missing", language)
	default:
		info.Note = fmt.Sprintf("no %s grammar — declarations were found by a text scan and some may be missing", language)
	}
	return info
}

// OutlineStructured returns the grouped outline, the flat symbol list, and a
// ParserInfo saying how those symbols were obtained — see ParserFor for why
// the last one is not optional.
func (i *Index) OutlineStructured(path string) (OutlineSections, []Symbol, protocol.ParserInfo, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return OutlineSections{}, nil, protocol.ParserInfo{}, err
	}
	rel, err := filepath.Rel(i.root, absolute)
	if err != nil {
		return OutlineSections{}, nil, protocol.ParserInfo{}, err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return OutlineSections{}, nil, protocol.ParserInfo{}, err
	}

	lines := splitLines(string(data))
	symbols, mode := i.symbolsForPath(absolute, rel, data, lines)
	sections := groupSymbols(symbols)
	sections.Imports = extractImports(lines)

	if i.bus != nil {
		i.bus.Publish(events.Event{
			Type:   "INDEX_UPDATED",
			Entity: "code_index",
			Payload: map[string]string{
				"path":    rel,
				"mode":    mode,
				"symbols": fmt.Sprintf("%d", len(symbols)),
			},
		})
	}

	return sections, symbols, ParserFor(rel, mode), nil
}

// WorkspaceTree returns a plain, bounded structural listing of the
// workspace — orientation ("what does this repo look like"), unlike
// RepositoryMap which ranks files against a query. Directories and files
// matching shouldSkipPath (vendor, node_modules, .git, build output, etc.)
// are pruned from traversal entirely rather than merely filtered, since a
// vendored tree can be large enough to matter even just to walk.
func (i *Index) WorkspaceTree(maxEntries int) (protocol.WorkspaceTreeResponse, error) {
	if maxEntries <= 0 {
		maxEntries = 500
	}

	entries := make([]protocol.WorkspaceTreeEntry, 0)
	err := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil {
			return nil
		}

		rel, err := filepath.Rel(i.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}

		if i.skipped(path) || i.gitIgnored(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		entries = append(entries, protocol.WorkspaceTreeEntry{Path: rel, IsDir: info.IsDir()})
		return nil
	})
	if err != nil {
		return protocol.WorkspaceTreeResponse{}, err
	}

	sort.Slice(entries, func(a, b int) bool { return entries[a].Path < entries[b].Path })

	truncated := false
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
		truncated = true
	}

	summary := fmt.Sprintf("%d entries", len(entries))
	if truncated {
		summary += fmt.Sprintf(" (truncated at %d)", maxEntries)
	}

	return protocol.WorkspaceTreeResponse{
		Root:      i.root,
		Entries:   entries,
		Truncated: truncated,
		Summary:   summary,
	}, nil
}

func (i *Index) RepositoryMap(query string, maxTokens int) (protocol.RepositoryMapResponse, error) {
	if maxTokens <= 0 {
		maxTokens = 12000
	}

	query = strings.TrimSpace(query)
	terms := splitQueryTerms(query)
	candidates := make([]protocol.RepositoryMapItem, 0)

	err := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if i.skipped(path) || i.leavesWorkspace(path, info) {
			return nil
		}
		if !isTextLike(path) {
			return nil
		}

		rel, err := filepath.Rel(i.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		symbols, _ := i.symbolsForPath(path, rel, data, splitLines(string(data)))
		score := scoreRepositoryFile(rel, query, terms, string(data))
		cost := estimateTokenCost(len(data), len(symbols))
		candidates = append(candidates, protocol.RepositoryMapItem{
			Path:          rel,
			Score:         score,
			EstimatedCost: cost,
			SymbolCount:   len(symbols),
		})
		return nil
	})
	if err != nil {
		return protocol.RepositoryMapResponse{}, err
	}

	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].Score == candidates[b].Score {
			return candidates[a].Path < candidates[b].Path
		}
		return candidates[a].Score > candidates[b].Score
	})

	included := make([]protocol.RepositoryMapItem, 0)
	omitted := make([]protocol.RepositoryMapItem, 0)
	used := 0
	for _, item := range candidates {
		if used+item.EstimatedCost <= maxTokens || len(included) == 0 {
			included = append(included, item)
			used += item.EstimatedCost
			continue
		}
		omitted = append(omitted, item)
	}

	summary := fmt.Sprintf("query=%q included=%d omitted=%d max_tokens=%d used_tokens=%d", query, len(included), len(omitted), maxTokens, used)
	if query == "" {
		summary = fmt.Sprintf("included=%d omitted=%d max_tokens=%d used_tokens=%d", len(included), len(omitted), maxTokens, used)
	}

	return protocol.RepositoryMapResponse{
		Query:      query,
		MaxTokens:  maxTokens,
		UsedTokens: used,
		Included:   included,
		Omitted:    omitted,
		Summary:    summary,
		Provenance: rankedProvenance(len(omitted) > 0),
	}, nil
}

// rankedProvenance is the provenance of a ranked answer from the text index:
// names and terms matched, with a fixed vocabulary expansion, never resolved.
func rankedProvenance(cut bool) protocol.Provenance {
	provenance := protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessMayBeIncomplete}
	if cut {
		provenance.Completeness = protocol.CompletenessCut
	}
	return provenance
}

func (i *Index) Search(query string, mode string, limit int) (protocol.SearchResponse, error) {
	if limit <= 0 {
		limit = 10
	}
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "auto"
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return protocol.SearchResponse{Query: query, Mode: mode}, nil
	}

	terms := splitQueryTerms(query)
	semanticTerms := expandSemanticTerms(terms)
	hits := make([]protocol.SearchHit, 0)

	err := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if i.skipped(path) || !isTextLike(path) || i.leavesWorkspace(path, info) {
			return nil
		}
		rel, err := filepath.Rel(i.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := strings.ToLower(string(data))
		base := filepath.Base(path)
		baseLower := strings.ToLower(base)
		matchScore := 0.0
		winningReason := ""
		for _, term := range terms {
			if term == "" {
				continue
			}
			if strings.Contains(content, term) {
				matchScore += 3.0
				winningReason = "content"
			}
			if strings.Contains(baseLower, term) {
				matchScore += 4.0
				winningReason = "path"
			}
		}
		for _, term := range semanticTerms {
			if term == "" {
				continue
			}
			if strings.Contains(content, term) {
				matchScore += 2.5
				if winningReason == "" || winningReason == "path" {
					winningReason = "semantic"
				}
			}
		}
		if matchScore == 0.0 && mode == "semantic" {
			return nil
		}
		if matchScore == 0.0 && mode == "auto" {
			return nil
		}

		symbols, _ := i.symbolsForPath(path, rel, data, splitLines(string(data)))
		for _, symbol := range symbols {
			if symbol.Name == "" {
				continue
			}
			symbolScore := matchScore
			if strings.Contains(strings.ToLower(symbol.Name), strings.ToLower(query)) {
				symbolScore += 6.0
				winningReason = "symbol"
			}
			for _, term := range terms {
				if strings.Contains(strings.ToLower(symbol.Name), term) {
					symbolScore += 2.5
				}
			}
			for _, term := range semanticTerms {
				if strings.Contains(strings.ToLower(symbol.Name), term) {
					symbolScore += 3.0
					if winningReason == "" {
						winningReason = "semantic"
					}
				}
			}
			hits = append(hits, protocol.SearchHit{
				Path:      rel,
				Symbol:    symbol.Name,
				Reason:    winningReason,
				Score:     symbolScore,
				Snippet:   strings.TrimSpace(strings.Join(splitLines(string(data))[max(0, symbol.From-1):min(len(splitLines(string(data))), symbol.To)], "\n")),
				Kind:      symbol.Kind,
				StartLine: symbol.From,
				EndLine:   symbol.To,
			})
		}
		if len(symbols) == 0 {
			hits = append(hits, protocol.SearchHit{
				Path:      rel,
				Symbol:    basenameSymbol(rel),
				Reason:    winningReason,
				Score:     matchScore,
				Snippet:   firstSnippet(data, 120),
				Kind:      "file",
				StartLine: 1,
				EndLine:   1,
			})
		}
		return nil
	})
	if err != nil {
		return protocol.SearchResponse{}, err
	}

	sort.Slice(hits, func(a, b int) bool {
		if hits[a].Score == hits[b].Score {
			if hits[a].Path == hits[b].Path {
				return hits[a].Symbol < hits[b].Symbol
			}
			return hits[a].Path < hits[b].Path
		}
		return hits[a].Score > hits[b].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return protocol.SearchResponse{Query: query, Mode: mode, Hits: hits, Provenance: rankedProvenance(len(hits) >= limit)}, nil
}

func (i *Index) Retrieve(query string, maxTokens int) (protocol.RetrievalResponse, error) {
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return protocol.RetrievalResponse{Query: query, MaxTokens: maxTokens, UsedTokens: 0, Candidates: nil, Summary: "empty query"}, nil
	}

	searchResp, err := i.Search(query, "auto", 50)
	if err != nil {
		return protocol.RetrievalResponse{}, err
	}

	byPath := make(map[string]protocol.RetrievalCandidate)
	for _, hit := range searchResp.Hits {
		if hit.Path == "" {
			continue
		}
		cost := 200 + len(hit.Snippet)
		candidate, ok := byPath[hit.Path]
		if !ok || hit.Score > candidate.Score {
			candidate = protocol.RetrievalCandidate{
				Path:          hit.Path,
				Symbol:        hit.Symbol,
				Reason:        hit.Reason,
				Score:         hit.Score,
				EstimatedCost: cost,
				Kind:          hit.Kind,
				StartLine:     hit.StartLine,
				EndLine:       hit.EndLine,
			}
			byPath[hit.Path] = candidate
		}
	}
	if len(byPath) == 0 {
		mapResp, err := i.RepositoryMap(query, maxTokens)
		if err != nil {
			return protocol.RetrievalResponse{}, err
		}
		candidates := make([]protocol.RetrievalCandidate, 0, len(mapResp.Included))
		for _, item := range mapResp.Included {
			candidates = append(candidates, protocol.RetrievalCandidate{
				Path:          item.Path,
				Symbol:        basenameSymbol(item.Path),
				Reason:        "repository_map",
				Score:         item.Score,
				EstimatedCost: item.EstimatedCost,
				Kind:          "file",
			})
		}
		used := 0
		for _, item := range candidates {
			used += item.EstimatedCost
		}
		exceeded := used > maxTokens
		summary := fmt.Sprintf("repository_map query=%q included=%d used=%d", query, len(candidates), used)
		if exceeded {
			summary += fmt.Sprintf(" (budget exceeded: used %d > max %d)", used, maxTokens)
		}
		return protocol.RetrievalResponse{
			Query:          query,
			MaxTokens:      maxTokens,
			UsedTokens:     used,
			Candidates:     candidates,
			Summary:        summary,
			BudgetExceeded: exceeded,
			Provenance:     rankedProvenance(exceeded),
		}, nil
	}

	candidates := make([]protocol.RetrievalCandidate, 0, len(byPath))
	for _, candidate := range byPath {
		candidates = append(candidates, candidate)
	}
	applyCallGraphBoost(candidates, i.callGraphNeighbors())
	sort.Slice(candidates, func(a, b int) bool {
		if candidates[a].Score == candidates[b].Score {
			return candidates[a].Path < candidates[b].Path
		}
		return candidates[a].Score > candidates[b].Score
	})

	chosen := make([]protocol.RetrievalCandidate, 0, len(candidates))
	used := 0
	for _, candidate := range candidates {
		if len(chosen) > 0 && used+candidate.EstimatedCost > maxTokens {
			continue
		}
		chosen = append(chosen, candidate)
		used += candidate.EstimatedCost
	}

	// A single candidate that alone exceeds maxTokens is still always
	// included (the alternative — returning zero candidates — is worse),
	// but that must be surfaced explicitly rather than silently violating
	// the caller's stated budget.
	exceeded := used > maxTokens
	summary := fmt.Sprintf("query=%q selected=%d used=%d max=%d", query, len(chosen), used, maxTokens)
	if exceeded {
		summary += fmt.Sprintf(" (budget exceeded: used %d > max %d)", used, maxTokens)
	}

	return protocol.RetrievalResponse{
		Query:          query,
		MaxTokens:      maxTokens,
		UsedTokens:     used,
		Candidates:     chosen,
		Summary:        summary,
		BudgetExceeded: exceeded,
		Provenance:     rankedProvenance(exceeded),
	}, nil
}

func (i *Index) BuildRetrievalIndex() (protocol.RetrievalIndex, error) {
	docs := make([]protocol.RetrievalDocument, 0)
	err := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if i.skipped(path) || !isTextLike(path) || i.leavesWorkspace(path, info) {
			return nil
		}
		rel, err := filepath.Rel(i.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := splitLines(string(data))
		symbols, _ := i.symbolsForPath(path, rel, data, lines)
		fileTerms := tokenizeTerms(string(data))
		docs = append(docs, protocol.RetrievalDocument{
			Path:   rel,
			Symbol: basenameSymbol(rel),
			Kind:   "file",
			Terms:  fileTerms,
		})
		for _, symbol := range symbols {
			docs = append(docs, protocol.RetrievalDocument{
				Path:      rel,
				Symbol:    symbol.Name,
				Kind:      symbol.Kind,
				Terms:     tokenizeTerms(strings.Join(lines[max(0, symbol.From-1):min(len(lines), symbol.To)], "\n")),
				StartLine: symbol.From,
				EndLine:   symbol.To,
			})
		}
		return nil
	})
	if err != nil {
		return protocol.RetrievalIndex{}, err
	}
	return protocol.RetrievalIndex{Documents: docs}, nil
}

type fileSymbols struct {
	rel     string
	lines   []string
	symbols []Symbol
}

func (i *Index) BuildSymbolGraph() (protocol.SymbolGraph, error) {
	graph := protocol.SymbolGraph{Nodes: make([]protocol.SymbolGraphNode, 0), Edges: make([]protocol.SymbolGraphEdge, 0)}
	nodes := map[string]protocol.SymbolGraphNode{}
	edges := map[string]protocol.SymbolGraphEdge{}
	byName := map[string][]string{}
	files := make([]fileSymbols, 0)

	err := filepath.Walk(i.root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if i.skipped(path) || !isTextLike(path) || i.leavesWorkspace(path, info) {
			return nil
		}
		rel, err := filepath.Rel(i.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := splitLines(string(data))
		symbols, _ := i.symbolsForPath(path, rel, data, lines)
		files = append(files, fileSymbols{rel: rel, lines: lines, symbols: symbols})
		for _, symbol := range symbols {
			id := fmt.Sprintf("%s::%s", rel, symbol.Name)
			nodes[id] = protocol.SymbolGraphNode{ID: id, Path: rel, Name: symbol.Name, Kind: symbol.Kind}
			byName[symbol.Name] = append(byName[symbol.Name], id)
		}
		return nil
	})
	if err != nil {
		return protocol.SymbolGraph{}, err
	}

	for _, file := range files {
		for _, symbol := range file.symbols {
			id := fmt.Sprintf("%s::%s", file.rel, symbol.Name)
			sameFileID := id
			body := strings.Join(file.lines[max(0, symbol.From-1):min(len(file.lines), symbol.To)], "\n")
			for _, callee := range extractCalledSymbols(body) {
				calleeID, confidence := resolveCalleeID(file.rel, callee, nodes, byName)
				if calleeID == "" || calleeID == sameFileID {
					continue
				}
				key := id + "->" + calleeID + "|calls"
				edges[key] = protocol.SymbolGraphEdge{From: id, To: calleeID, Kind: "calls", Confidence: confidence}
			}
		}
	}

	for _, node := range nodes {
		graph.Nodes = append(graph.Nodes, node)
	}
	for _, edge := range edges {
		graph.Edges = append(graph.Edges, edge)
	}
	sort.Slice(graph.Nodes, func(a, b int) bool { return graph.Nodes[a].ID < graph.Nodes[b].ID })
	sort.Slice(graph.Edges, func(a, b int) bool {
		return graph.Edges[a].From+graph.Edges[a].To < graph.Edges[b].From+graph.Edges[b].To
	})

	graph.Source = "approximate"
	graph.Limitations = approximateGraphLimitations()
	return graph, nil
}

// approximateGraphLimitations states what a name-matched graph provably
// cannot see. These are not hypothetical — each one is a documented failure
// mode of name matching — and they are returned with every graph so the
// caveat cannot be separated from the data.
func approximateGraphLimitations() []string {
	return []string{
		"call edges are matched by name, not resolved by a compiler",
		"a callee whose name is declared in more than one file is dropped rather than guessed",
		"dynamic dispatch through interfaces and function values is invisible",
		"registry, reflection and string-keyed indirection are invisible",
		"cross-language calls are not tracked",
		"shadowed and locally-redeclared names may resolve to the wrong declaration",
	}
}

// resolveCalleeID resolves a called symbol name to its defining node ID,
// preferring a match in the caller's own file, then falling back to a
// repository-wide match when the callee name is unambiguous. The second
// return value records which of those two happened, so the resulting edge
// can carry its own provenance instead of the caller having to assume.
func resolveCalleeID(callerRel, callee string, nodes map[string]protocol.SymbolGraphNode, byName map[string][]string) (string, string) {
	sameFileID := fmt.Sprintf("%s::%s", callerRel, callee)
	if _, exists := nodes[sameFileID]; exists {
		return sameFileID, protocol.EdgeSameFile
	}
	candidates := byName[callee]
	if len(candidates) == 1 {
		return candidates[0], protocol.EdgeUniqueName
	}
	return "", ""
}

// callGraphNeighbors builds a symbol-id -> connected-symbol-ids adjacency map
// (both call directions) from the repository's symbol graph.
func (i *Index) callGraphNeighbors() map[string]map[string]bool {
	graph, err := i.BuildSymbolGraph()
	if err != nil {
		return nil
	}
	neighbors := make(map[string]map[string]bool, len(graph.Edges)*2)
	add := func(from, to string) {
		set, ok := neighbors[from]
		if !ok {
			set = make(map[string]bool)
			neighbors[from] = set
		}
		set[to] = true
	}
	for _, edge := range graph.Edges {
		add(edge.From, edge.To)
		add(edge.To, edge.From)
	}
	return neighbors
}

// applyCallGraphBoost increases each candidate's score for every call-graph
// edge connecting it to another candidate in the same result set, so
// dependency-related symbols outrank textually-stronger but unconnected ones.
const callGraphBoostPerEdge = 5.0

func applyCallGraphBoost(candidates []protocol.RetrievalCandidate, neighbors map[string]map[string]bool) {
	if len(neighbors) == 0 || len(candidates) < 2 {
		return
	}
	ids := make([]string, len(candidates))
	for idx, candidate := range candidates {
		ids[idx] = fmt.Sprintf("%s::%s", candidate.Path, candidate.Symbol)
	}
	for a := range candidates {
		for b := range candidates {
			if a == b {
				continue
			}
			if neighbors[ids[a]][ids[b]] {
				candidates[a].Score += callGraphBoostPerEdge
			}
		}
	}
}

func extractCalledSymbols(body string) []string {
	calls := make([]string, 0)
	matcher := regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	matches := matcher.FindAllStringSubmatch(body, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := match[1]
		if isReservedName(name) {
			continue
		}
		calls = append(calls, name)
	}
	return calls
}

func tokenizeTerms(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
	seen := map[string]bool{}
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, term := range splitIdentifierTerms(part) {
			clean := strings.TrimSpace(strings.ToLower(term))
			if clean == "" {
				continue
			}
			if seen[clean] {
				continue
			}
			seen[clean] = true
			terms = append(terms, clean)
		}
	}
	return terms
}

func splitIdentifierTerms(value string) []string {
	if value == "" {
		return nil
	}
	out := make([]string, 0, 4)
	buf := make([]rune, 0, len(value))
	for i, r := range value {
		if r == '_' {
			if len(buf) > 0 {
				out = append(out, string(buf))
				buf = buf[:0]
			}
			continue
		}
		if i > 0 {
			prev := []rune(value)[i-1]
			if unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
				if len(buf) > 0 {
					out = append(out, string(buf))
					buf = buf[:0]
				}
			}
		}
		buf = append(buf, r)
	}
	if len(buf) > 0 {
		out = append(out, string(buf))
	}
	return out
}

func expandSemanticTerms(terms []string) []string {
	aliases := map[string][]string{
		"auth":     {"auth", "session", "login", "token", "identity"},
		"session":  {"session", "auth", "login", "token", "validate"},
		"login":    {"login", "auth", "session", "token", "identity"},
		"check":    {"check", "validate", "verify", "guard", "assert"},
		"validate": {"validate", "check", "verify", "guard", "assert"},
		"verify":   {"verify", "validate", "check", "assert"},
		"user":     {"user", "account", "principal", "actor"},
		"account":  {"account", "user", "principal", "identity"},
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(terms)*4)
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if aliases[term] != nil {
			for _, alt := range aliases[term] {
				if !seen[alt] {
					seen[alt] = true
					out = append(out, alt)
				}
			}
		}
		if !seen[term] {
			seen[term] = true
			out = append(out, term)
		}
	}
	return out
}

func basenameSymbol(path string) string {
	name := filepath.Base(path)
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx]
	}
	return name
}

func firstSnippet(data []byte, maxLen int) string {
	text := strings.TrimSpace(string(data))
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (i *Index) ReadSymbol(path string, symbolID string, maxLines int) (Symbol, string, error) {
	if maxLines <= 0 {
		maxLines = 150
	}

	absolute, err := i.resolvePath(path)
	if err != nil {
		return Symbol{}, "", err
	}
	rel, err := filepath.Rel(i.root, absolute)
	if err != nil {
		return Symbol{}, "", err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return Symbol{}, "", err
	}

	lines := splitLines(string(data))
	symbols, _ := i.symbolsForPath(absolute, rel, data, lines)
	symbol, err := resolveSymbolInFile(symbols, symbolID)
	if err != nil {
		return Symbol{}, "", err
	}

	to := symbol.To
	if to-symbol.From+1 > maxLines {
		to = symbol.From + maxLines - 1
	}

	if symbol.From < 1 || to > len(lines) || symbol.From > to {
		return Symbol{}, "", fmt.Errorf("symbol %s has invalid range", symbol.ID)
	}

	body := strings.Join(lines[symbol.From-1:to], "\n")
	if to < symbol.To {
		body += "\n// ... truncated"
	}
	return symbol, body, nil
}

// ReadRange reads lines [start,end] (inclusive, 1-indexed) of path verbatim
// — the read-side escape hatch for content that can't be addressed by
// symbol (comments, config files, generated code with no clean symbol
// boundaries), mirroring ReplaceRangeSource's write-side equivalent.
// maxReadRangeBytes bounds a single read so one enormous file cannot blow the
// caller's context. Large enough for any configuration file — the reason
// whole-file reads exist — and small enough that hitting it is a signal to ask
// a narrower question.
const maxReadRangeBytes = 20000

// ReadRange reads lines start..end inclusive.
//
// An unset start means "from the beginning" and an unset end means "to the
// end", so a call with neither reads the whole file. That case is the point:
// go.mod, a Makefile, .mcp.json and every JSON/YAML/TOML config have no
// symbols to address and cannot be reached by read_symbol or find — yet they
// are exactly the files an agent opens first in an unfamiliar repo. Requiring
// a line range to read them meant every one of them was a `cat`, which is how
// this showed up in the dogfooding log.
//
// An end past the last line is clamped to the last line, and ReadRangeInfo
// reports that it was. This used to be an error, on the theory that a caller
// naming an end states a belief about the file's length worth correcting. In
// use it was the opposite: "from here to the end" was nearly always the intent,
// and the rejection cost a whole extra turn every time.
func (i *Index) ReadRange(path string, start int, end int) (string, error) {
	read, err := i.ReadRangeInfo(path, start, end)
	return read.Source, err
}

// RangeRead is a line range as actually read: the bounds used, the file's
// length, and whether the end was clamped.
type RangeRead struct {
	Source     string
	Start      int
	End        int
	Total      int
	ClampedEnd bool
	// NextLine is the first line a paged read left out, or 0 when it reached
	// Through, the end of the range that was asked for.
	NextLine int
	Through  int
}

// ReadRangeInfo reads a line range and reports the bounds it used.
//
// An end line past the end of the file is clamped to the last line, not
// rejected: "from here to the end" is what that request nearly always means,
// and rejecting it cost the caller a whole extra turn each time. The clamp is
// reported so the caller knows it got less than it named. A start past the end
// is still an error — there is nothing there to read.
func (i *Index) ReadRangeInfo(path string, start int, end int) (RangeRead, error) {
	read, lines, err := i.rangeLines(path, start, end)
	if err != nil || len(lines) == 0 {
		return read, err
	}
	text := strings.Join(lines[read.Start-1:read.End], "\n")
	read.Source, _ = textutil.Clamp(text, maxReadRangeBytes, "request a narrower line range")
	return read, nil
}

// ReadRangePage reads start..end like ReadRangeInfo, but whole lines up to
// maxBytes, at least one, instead of cutting out the middle: a caller can ask
// for the rest from NextLine rather than lose it.
func (i *Index) ReadRangePage(path string, start int, end int, maxBytes int) (RangeRead, error) {
	read, lines, err := i.rangeLines(path, start, end)
	if err != nil || len(lines) == 0 {
		return read, err
	}
	read.Through = read.End
	size, last := 0, read.Start
	for n := read.Start; n <= read.End; n++ {
		cost := len(lines[n-1]) + 1
		if n > read.Start && size+cost > maxBytes {
			break
		}
		size += cost
		last = n
	}
	if last < read.End {
		read.NextLine = last + 1
		read.End = last
	}
	read.Source = strings.Join(lines[read.Start-1:read.End], "\n")
	return read, nil
}

// rangeLines resolves a range against path's lines: the bounds, the file's
// length and whether the end was clamped.
func (i *Index) rangeLines(path string, start int, end int) (RangeRead, []string, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return RangeRead{}, nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return RangeRead{}, nil, err
	}

	lines := splitLines(string(data))
	if len(lines) == 0 {
		return RangeRead{}, nil, nil
	}

	// A negative bound is a caller mistake and is reported, but zero is not:
	// an omitted integer arrives over JSON-RPC as zero, so zero has to mean
	// "unset" for omission to be expressible at all.
	if start < 0 || end < 0 {
		return RangeRead{}, nil, fmt.Errorf("line numbers cannot be negative (got %d-%d for %s)", start, end, path)
	}

	if start == 0 {
		start = 1
	}
	clampedEnd := false
	switch {
	case end == 0:
		end = len(lines)
	case end > len(lines):
		end = len(lines)
		clampedEnd = true
	}

	if start > len(lines) || start > end {
		return RangeRead{}, nil, fmt.Errorf("range %d-%d is invalid for %s (%d lines)", start, end, path, len(lines))
	}

	return RangeRead{Start: start, End: end, Total: len(lines), ClampedEnd: clampedEnd}, lines, nil
}

// ReplaceSymbolSource resolves symbolID to its defining file, splices newCode
// over the symbol's current line range, and writes the file back to disk.
// It returns the replaced symbol and the source lines it displaced.
func (i *Index) ReplaceSymbolSource(symbolID string, newCode string) (Symbol, []string, error) {
	relPath, ok := symbolIDPath(symbolID)
	if !ok {
		return Symbol{}, nil, fmt.Errorf("invalid symbol id: %s", symbolID)
	}

	absolute, err := i.resolvePath(relPath)
	if err != nil {
		return Symbol{}, nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Symbol{}, nil, err
	}

	lines := splitLines(string(data))
	symbols, _ := i.symbolsForPath(absolute, relPath, data, lines)
	symbol, err := resolveSymbolInFile(symbols, symbolID)
	if err != nil {
		return Symbol{}, nil, err
	}
	if symbol.From < 1 || symbol.To > len(lines) || symbol.From > symbol.To {
		return Symbol{}, nil, fmt.Errorf("symbol %s has invalid range", symbol.ID)
	}

	oldLines, err := spliceAndWrite(absolute, lines, symbol.From, symbol.To, newCode)
	if err != nil {
		return Symbol{}, nil, err
	}
	return symbol, oldLines, nil
}

// ReplaceRangeSource splices newCode over lines [start,end] (inclusive,
// 1-indexed) of path and writes the file back to disk. It returns the
// source lines it displaced.
func (i *Index) ReplaceRangeSource(path string, start int, end int, newCode string) ([]string, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}

	lines := splitLines(string(data))
	if start < 1 || end > len(lines) || start > end {
		return nil, fmt.Errorf("range %d-%d is invalid for %s (%d lines)", start, end, path, len(lines))
	}

	return spliceAndWrite(absolute, lines, start, end, newCode)
}

// CreateFile writes a brand-new file. It refuses to overwrite an existing
// one — an agent that wants to modify existing content should use
// ReplaceSymbolSource/ReplaceRangeSource, which carry revision checks;
// CreateFile has none, so silently overwriting would be unsafe.
func (i *Index) CreateFile(path string, content string) error {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absolute); err == nil {
		return fmt.Errorf("file already exists: %s (use replace_symbol/replace_range to modify it)", path)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return err
	}

	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(absolute, []byte(content), 0o644)
}

// ReplaceFileSource overwrites an existing file wholesale and returns the line
// count it had, for the caller's diff accounting.
//
// This is the counterpart to CreateFile, and the two are deliberately
// symmetric: CreateFile refuses when the file exists, ReplaceFileSource
// refuses when it does not. Neither can do the other's job by accident, so no
// flag is needed to make the destructive case explicit — choosing the tool is
// the explicit act.
//
// It exists because every anchored edit needs something to anchor on.
// create_file cannot overwrite, replace_text and apply need text that is
// already in the file, and none of them can express "this document now says
// something else" — a rewritten README, a regenerated fixture, a consolidated
// notes file. That gap is total rather than merely awkward: there is no
// partial workaround, so it fell to a raw shell write every single time.
func (i *Index) ReplaceFileSource(path string, content string) (int, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return 0, err
	}

	existing, err := os.ReadFile(absolute)
	if os.IsNotExist(err) {
		return 0, fmt.Errorf("file does not exist: %s (use create_file to create it)", path)
	}
	if err != nil {
		return 0, err
	}

	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		return 0, err
	}

	// The symbol cache keys on mtime and size, so the write self-invalidates.
	return countLinesIn(string(existing)), nil
}

// DeleteFile removes a file and evicts its cached symbols (if any), and
// returns the line count it had for the caller's diff accounting.
func (i *Index) DeleteFile(path string) (int, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return 0, err
	}
	lineCount := len(splitLines(string(data)))

	if err := os.Remove(absolute); err != nil {
		return 0, err
	}

	i.cacheMu.Lock()
	delete(i.cache, absolute)
	i.cacheMu.Unlock()

	return lineCount, nil
}

// spliceAndWrite replaces lines [from,to] (inclusive, 1-indexed) of lines
// with newCode's lines, writes the result to absolute, and returns the
// displaced old lines.
func spliceAndWrite(absolute string, lines []string, from int, to int, newCode string) ([]string, error) {
	oldLines := append([]string(nil), lines[from-1:to]...)
	newLines := splitLines(newCode)

	updated := make([]string, 0, len(lines)-len(oldLines)+len(newLines))
	updated = append(updated, lines[:from-1]...)
	updated = append(updated, newLines...)
	updated = append(updated, lines[to:]...)

	if err := os.WriteFile(absolute, []byte(strings.Join(updated, "\n")+"\n"), 0o644); err != nil {
		return nil, err
	}
	return oldLines, nil
}

// symbolIDPath extracts the file path portion of a "path::Name@line" symbol ID.
func symbolIDPath(symbolID string) (string, bool) {
	idx := strings.LastIndex(symbolID, "::")
	if idx < 0 {
		return "", false
	}
	return symbolID[:idx], true
}

func (i *Index) SymbolsByName(path string, symbolName string) ([]Symbol, error) {
	absolute, err := i.resolvePath(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(i.root, absolute)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}

	all, _ := i.symbolsForPath(absolute, rel, data, splitLines(string(data)))
	matches := make([]Symbol, 0)
	for _, symbol := range all {
		if symbol.Name == symbolName {
			matches = append(matches, symbol)
		}
	}
	return matches, nil
}

func (i *Index) symbolsForPath(absolute string, relPath string, data []byte, lines []string) ([]Symbol, string) {
	if info, err := os.Stat(absolute); err == nil {
		i.cacheMu.RLock()
		cached, hit := i.cache[absolute]
		i.cacheMu.RUnlock()
		if hit && cached.modTime.Equal(info.ModTime()) && cached.size == info.Size() {
			return cached.symbols, cached.mode
		}

		symbols, mode := i.parseSymbolsForPath(absolute, relPath, data, lines)
		i.cacheMu.Lock()
		i.cache[absolute] = fileSymbolCache{modTime: info.ModTime(), size: info.Size(), symbols: symbols, mode: mode}
		i.cacheMu.Unlock()
		return symbols, mode
	}

	// Stat failed (e.g. a race with deletion) — parse without caching
	// rather than failing the whole request.
	return i.parseSymbolsForPath(absolute, relPath, data, lines)
}

// parseSymbolsForPath does the actual parse work that symbolsForPath caches.
func (i *Index) parseSymbolsForPath(absolute string, relPath string, data []byte, lines []string) ([]Symbol, string) {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv(parserModeEnv)))
	if mode != "regex" {
		var symbols []Symbol
		var err error
		switch strings.ToLower(filepath.Ext(absolute)) {
		case ".go":
			symbols, err = extractGoSymbolsTreeSitter(relPath, data)
		case ".ts":
			symbols, err = extractTypeScriptSymbolsTreeSitter(relPath, data)
		case ".tsx":
			symbols, err = extractTSXSymbolsTreeSitter(relPath, data)
		case ".rs":
			symbols, err = extractRustSymbolsTreeSitter(relPath, data)
		case ".py":
			symbols, err = extractPythonSymbolsTreeSitter(relPath, data)
		case ".rb":
			symbols, err = extractRubySymbolsTreeSitter(relPath, data)
		case ".java":
			symbols, err = extractJavaSymbolsTreeSitter(relPath, data)
		case ".scala", ".sc":
			symbols, err = extractScalaSymbolsTreeSitter(relPath, data)
		case ".js", ".jsx", ".mjs", ".cjs":
			symbols, err = extractJavaScriptSymbolsTreeSitter(relPath, data)
		}
		if err == nil && len(symbols) > 0 {
			return symbols, "tree-sitter"
		}
	}

	return extractSymbols(relPath, lines), "regex"
}

// resolvePath turns a caller's path into an absolute one inside the
// workspace, refusing absolute, `..` and symlink escapes. Every read and write
// the index does on a caller's behalf starts here.
func (i *Index) resolvePath(path string) (string, error) {
	return pathguard.Resolve(i.root, path)
}

// leavesWorkspace reports a path that is not repository content: a symlink
// whose target lies outside the workspace, or an untracked file git ignores.
// Walks skip both: a link into ~/.aws must not be read through, and build
// output answers every search a second time.
func (i *Index) leavesWorkspace(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 && !pathguard.Contains(i.root, path) {
		return true
	}
	return i.gitIgnored(path)
}

func splitLines(input string) []string {
	scanner := bufio.NewScanner(strings.NewReader(input))
	lines := make([]string, 0, 64)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

var declarationPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{kind: "struct", pattern: regexp.MustCompile(`^\s*type\s+([A-Z][A-Za-z0-9_]*)\s+struct\b`)},
	{kind: "type", pattern: regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s`)},
	{kind: "function", pattern: regexp.MustCompile(`^\s*func\s+(?:\([^)]+\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
	{kind: "function", pattern: regexp.MustCompile(`^\s*export\s+function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
	{kind: "function", pattern: regexp.MustCompile(`^\s*function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
	{kind: "class", pattern: regexp.MustCompile(`^\s*export\s+class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)},
	{kind: "class", pattern: regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)},
	{kind: "method", pattern: regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)\s*\{`)},
	{kind: "method", pattern: regexp.MustCompile(`^\s*(?:pub\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)},
}

func extractSymbols(relPath string, lines []string) []Symbol {
	out := make([]Symbol, 0)
	for lineNo, line := range lines {
		for _, candidate := range declarationPatterns {
			match := candidate.pattern.FindStringSubmatch(line)
			if len(match) < 2 {
				continue
			}

			name := match[1]
			if isReservedName(name) {
				continue
			}
			from := lineNo + 1
			to := findEndLine(lines, lineNo)

			id := fmt.Sprintf("%s::%s@%d", relPath, name, from)
			out = append(out, Symbol{
				ID:   id,
				Kind: candidate.kind,
				Name: name,
				Path: relPath,
				From: from,
				To:   to,
			})
			break
		}
	}

	sort.Slice(out, func(a int, b int) bool {
		if out[a].From == out[b].From {
			return out[a].Name < out[b].Name
		}
		return out[a].From < out[b].From
	})

	return out
}

func isReservedName(name string) bool {
	switch name {
	case "if", "for", "switch", "while", "catch", "return", "else", "do", "try":
		return true
	default:
		return false
	}
}

// skipped applies shouldSkipPath to the part of path below the index's root.
// The directories above the root are the root's own business: an index rooted
// in node_modules/left-pad, to read a dependency, skipped every file in it, and
// a workspace under a directory named build skipped the whole repository.
func (i *Index) skipped(path string) bool {
	rel, err := filepath.Rel(i.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return shouldSkipPath(path)
	}
	return shouldSkipPath(rel)
}

// shouldSkipPath reports whether path lies inside a directory that should
// never be walked into (vendor, build output, VCS metadata, etc.). Matching
// is done per path segment rather than by substring: a substring check
// would (and, before this fix, did) also match unrelated names that merely
// contain a skip name as a prefix — e.g. "/repo/.gitignore" contains the
// substring "/.git", so it was incorrectly treated the same as ".git"
// itself. It also missed a bare skip directory's own path (e.g.
// ".../node_modules" with no trailing slash), which only matters once a
// caller (like WorkspaceTree) lists directories themselves, not just the
// files inside them.
func shouldSkipPath(path string) bool {
	skipNames := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, "dist": true,
		"build": true, "target": true, ".next": true, ".cache": true,
		"coverage": true, ".idea": true, ".vscode": true,
		// Jade's own telemetry directory is never repository content.
		".jade": true,
	}
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if skipNames[strings.ToLower(segment)] {
			return true
		}
	}
	return false
}

func isTextLike(path string) bool {
	if strings.HasSuffix(strings.ToLower(path), ".png") || strings.HasSuffix(strings.ToLower(path), ".jpg") || strings.HasSuffix(strings.ToLower(path), ".jpeg") || strings.HasSuffix(strings.ToLower(path), ".gif") || strings.HasSuffix(strings.ToLower(path), ".pdf") || strings.HasSuffix(strings.ToLower(path), ".zip") || strings.HasSuffix(strings.ToLower(path), ".exe") || strings.HasSuffix(strings.ToLower(path), ".bin") {
		return false
	}
	return true
}

func splitQueryTerms(query string) []string {
	if query == "" {
		return nil
	}
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			terms = append(terms, part)
		}
	}
	return terms
}

func scoreRepositoryFile(path string, query string, terms []string, content string) float64 {
	lowerPath := strings.ToLower(path)
	lowerContent := strings.ToLower(content)
	basename := strings.ToLower(filepath.Base(path))
	pathScore := 0.0

	if query == "" {
		return 1.0
	}

	for _, term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(lowerPath, term) {
			pathScore += 4.0
		}
		if strings.Contains(basename, term) {
			pathScore += 2.5
		}
		if strings.Contains(lowerContent, term) {
			pathScore += 2.0
		}
	}

	if strings.Contains(lowerPath, "readme") || strings.Contains(lowerPath, "scope") || strings.Contains(lowerPath, "doc") {
		pathScore *= 0.7
	}
	if path == "README.md" || strings.HasSuffix(lowerPath, ".md") {
		pathScore *= 0.75
	}
	if strings.HasPrefix(lowerPath, "internal/") {
		pathScore += 1.5
	}
	if strings.Contains(lowerPath, "auth") || strings.Contains(lowerPath, "session") || strings.Contains(lowerPath, "user") {
		pathScore += 1.0
	}
	if strings.Contains(lowerPath, "/test") || strings.Contains(lowerPath, "_test") || strings.Contains(lowerPath, "/tests/") {
		pathScore += 1.0
	}
	if strings.Contains(lowerPath, "cmd/") || strings.Contains(lowerPath, "/internal/") {
		pathScore += 0.5
	}
	return pathScore
}

func estimateTokenCost(fileBytes int, symbolCount int) int {
	if fileBytes <= 0 {
		fileBytes = 200
	}
	cost := fileBytes / 12
	if symbolCount > 0 {
		cost += symbolCount * 12
	}
	if cost < 64 {
		cost = 64
	}
	return cost
}

func findEndLine(lines []string, start int) int {
	max := start + 149
	if max >= len(lines) {
		max = len(lines) - 1
	}

	for i := start + 1; i <= max; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			return i
		}

		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") || strings.HasPrefix(trimmed, "class ") || strings.HasPrefix(trimmed, "export class ") || strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "pub fn ") {
			return i - 1
		}
	}

	return max + 1
}

func groupSymbols(symbols []Symbol) OutlineSections {
	sections := OutlineSections{}
	for _, symbol := range symbols {
		switch symbol.Kind {
		case "type", "struct":
			sections.Types = append(sections.Types, symbol)
		case "class":
			sections.Classes = append(sections.Classes, symbol)
		case "function":
			sections.Functions = append(sections.Functions, symbol)
		case "method":
			sections.Methods = append(sections.Methods, symbol)
		default:
			sections.Other = append(sections.Other, symbol)
		}
	}
	return sections
}

func extractImports(lines []string) []string {
	imports := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "use ") {
			imports = append(imports, trimmed)
			continue
		}

		if strings.HasPrefix(trimmed, "package ") || strings.HasPrefix(trimmed, "mod ") {
			imports = append(imports, trimmed)
		}
	}
	return imports
}
