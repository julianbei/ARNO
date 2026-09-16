package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

func TestRepositoryMapRanksRelevantFilesWithinBudget(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	authFile := filepath.Join(root, "internal", "auth", "session.go")
	if err := os.WriteFile(authFile, []byte("package auth\n\nfunc NewSession() string { return \"ok\" }\nfunc RefreshSession() string { return \"refresh\" }\n"), 0o644); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	readmeFile := filepath.Join(root, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# Project\n\nThis project is about the session lifecycle.\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	docsFile := filepath.Join(root, "docs", "notes.md")
	if err := os.WriteFile(docsFile, []byte("# Notes\n\nThis is unrelated documentation.\n"), 0o644); err != nil {
		t.Fatalf("write docs file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.RepositoryMap("session", 4096)
	if err != nil {
		t.Fatalf("repository map: %v", err)
	}

	if resp.MaxTokens <= 0 {
		t.Fatalf("expected max tokens to be positive")
	}
	if resp.UsedTokens > resp.MaxTokens {
		t.Fatalf("used tokens %d exceeded max tokens %d", resp.UsedTokens, resp.MaxTokens)
	}
	if len(resp.Included) == 0 {
		t.Fatalf("expected at least one included file")
	}

	contained := false
	for _, item := range resp.Included {
		if item.Path == filepath.Join("internal", "auth", "session.go") {
			contained = true
		}
	}
	if !contained {
		t.Fatalf("expected relevant auth file to be included, got %#v", resp.Included)
	}
	if resp.Query != "session" {
		t.Fatalf("expected query to be preserved, got %q", resp.Query)
	}
}

func TestSearchSupportsSymbolAndSemanticRanking(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	authFile := filepath.Join(root, "internal", "auth", "session.go")
	if err := os.WriteFile(authFile, []byte("package auth\n\nfunc RefreshSession() string { return \"refresh\" }\nfunc ValidateSession() string { return \"validate\" }\n"), 0o644); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	cacheFile := filepath.Join(root, "internal", "cache", "memo.go")
	if err := os.WriteFile(cacheFile, []byte("package cache\n\nfunc Memoize() string { return \"memo\" }\n"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.Search("RefreshSession", "auto", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(resp.Hits) == 0 {
		t.Fatalf("expected at least one hit")
	}
	if resp.Hits[0].Path != filepath.ToSlash(filepath.Join("internal", "auth", "session.go")) {
		t.Fatalf("expected auth file first, got %#v", resp.Hits)
	}
	if resp.Hits[0].Reason == "" {
		t.Fatalf("expected a reason label on the top hit")
	}

	semanticResp, err := idx.Search("session validation", "semantic", 10)
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}
	if len(semanticResp.Hits) == 0 {
		t.Fatalf("expected semantic hits")
	}
}

func TestRetrieveBuildsBudgetAwareCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "session.go"), []byte("package auth\n\nfunc RefreshSession() string { return \"refresh\" }\nfunc ValidateSession() string { return \"validate\" }\n"), 0o644); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "cache", "memo.go"), []byte("package cache\n\nfunc Memoize() string { return \"memo\" }\n"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.Retrieve("session validation", 2048)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(resp.Candidates) == 0 {
		t.Fatalf("expected retrieval candidates")
	}
	if resp.Query != "session validation" {
		t.Fatalf("expected query to be preserved, got %q", resp.Query)
	}
	if resp.UsedTokens > resp.MaxTokens {
		t.Fatalf("used tokens %d exceeded max %d", resp.UsedTokens, resp.MaxTokens)
	}
	if resp.Candidates[0].Path != filepath.ToSlash(filepath.Join("internal", "auth", "session.go")) {
		t.Fatalf("expected auth file first, got %#v", resp.Candidates)
	}
}

func TestSemanticModeUsesSynonymAwareScoring(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "cache"), 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "session.go"), []byte("package auth\n\nfunc RefreshSession() string { return \"refresh\" }\nfunc ValidateSession() string { return \"validate\" }\n"), 0o644); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "cache", "memo.go"), []byte("package cache\n\nfunc Memoize() string { return \"memo\" }\n"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.Search("user login check", "semantic", 10)
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}
	if len(resp.Hits) == 0 {
		t.Fatalf("expected semantic hits")
	}
	if resp.Hits[0].Path != filepath.ToSlash(filepath.Join("internal", "auth", "session.go")) {
		t.Fatalf("expected synonym-aware auth file first, got %#v", resp.Hits)
	}
}

func TestBuildRetrievalIndexCachesTermsPerFileAndSymbol(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "session.go"), []byte("package auth\n\nfunc ValidateSession() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	idx := NewIndex(root, nil)
	retrieval, err := idx.BuildRetrievalIndex()
	if err != nil {
		t.Fatalf("build retrieval index: %v", err)
	}
	if len(retrieval.Documents) == 0 {
		t.Fatalf("expected retrieval documents")
	}
	if retrieval.Documents[0].Path == "" {
		t.Fatalf("expected document path to be set")
	}
	if !containsToken(retrieval.Documents[0].Terms, "validate") {
		t.Fatalf("expected validate term to be indexed, got %#v", retrieval.Documents[0].Terms)
	}
}

func containsToken(tokens []string, needle string) bool {
	for _, token := range tokens {
		if token == needle {
			return true
		}
	}
	return false
}

func TestRetrieveBoostsCandidatesConnectedByCallEdges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal", "misc"), 0o755); err != nil {
		t.Fatalf("mkdir misc: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "entry.go"), []byte("package auth\n\nfunc Entry() string { return ValidateThing() }\n"), 0o644); err != nil {
		t.Fatalf("write entry file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "validate.go"), []byte("package auth\n\nfunc ValidateThing() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write validate file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "misc", "unrelated.go"), []byte("package misc\n\nfunc UnrelatedHelper() string { return \"noop\" }\n"), 0o644); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.Retrieve("entry validatething unrelated", 4096)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	pathRank := map[string]int{}
	for rank, candidate := range resp.Candidates {
		pathRank[candidate.Path] = rank
	}

	validatePath := filepath.ToSlash(filepath.Join("internal", "auth", "validate.go"))
	unrelatedPath := filepath.ToSlash(filepath.Join("internal", "misc", "unrelated.go"))

	validateRank, ok := pathRank[validatePath]
	if !ok {
		t.Fatalf("expected validate.go among candidates, got %#v", resp.Candidates)
	}
	unrelatedRank, ok := pathRank[unrelatedPath]
	if !ok {
		t.Fatalf("expected unrelated.go among candidates, got %#v", resp.Candidates)
	}
	if validateRank >= unrelatedRank {
		t.Fatalf("expected call-edge-connected validate.go (rank %d) to outrank textually-stronger but unconnected unrelated.go (rank %d): %#v", validateRank, unrelatedRank, resp.Candidates)
	}
}

func TestSymbolLocationsAreRecalculatedAfterWriteWithNoStaleCache(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shifting.go")
	original := "package demo\n\nfunc First() string {\n\treturn \"one\"\n}\n\nfunc Second() string {\n\treturn \"two\"\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	before, err := idx.Outline("shifting.go")
	if err != nil {
		t.Fatalf("outline before: %v", err)
	}
	var firstID string
	var secondBeforeFrom int
	for _, symbol := range before {
		switch symbol.Name {
		case "First":
			firstID = symbol.ID
		case "Second":
			secondBeforeFrom = symbol.From
		}
	}
	if firstID == "" || secondBeforeFrom == 0 {
		t.Fatalf("expected First and Second symbols, got %#v", before)
	}

	// Replacing First with a longer body shifts every symbol below it.
	if _, _, err := idx.ReplaceSymbolSource(firstID, "func First() string {\n\t// extra line one\n\t// extra line two\n\treturn \"one\"\n}\n"); err != nil {
		t.Fatalf("replace symbol: %v", err)
	}

	after, err := idx.Outline("shifting.go")
	if err != nil {
		t.Fatalf("outline after: %v", err)
	}
	var secondAfterFrom int
	var secondAfterID string
	for _, symbol := range after {
		if symbol.Name == "Second" {
			secondAfterFrom = symbol.From
			secondAfterID = symbol.ID
		}
	}
	if secondAfterFrom == 0 {
		t.Fatalf("expected Second symbol after edit, got %#v", after)
	}
	if secondAfterFrom == secondBeforeFrom {
		t.Fatalf("expected Second's line range to shift after First grew, still at line %d", secondAfterFrom)
	}

	// The very next tool call must resolve Second by its new ID, proving
	// there is no stale in-memory index to invalidate.
	symbol, _, err := idx.ReadSymbol("shifting.go", secondAfterID, 0)
	if err != nil {
		t.Fatalf("read symbol by recalculated id: %v", err)
	}
	if symbol.From != secondAfterFrom {
		t.Fatalf("expected read_symbol to agree with outline's recalculated location, got From=%d want %d", symbol.From, secondAfterFrom)
	}
}

func TestReplaceRangeSourceWritesFileToDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write greeter file: %v", err)
	}

	idx := NewIndex(root, nil)
	oldLines, err := idx.ReplaceRangeSource("greeter.go", 4, 4, "\treturn \"hello\"")
	if err != nil {
		t.Fatalf("replace range source: %v", err)
	}
	if len(oldLines) != 1 || oldLines[0] != "\treturn \"hi\"" {
		t.Fatalf("expected displaced old line to be the original return statement, got %#v", oldLines)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("expected file on disk to contain new implementation, got %q", string(data))
	}
	if strings.Contains(string(data), "\"hi\"") {
		t.Fatalf("expected old implementation to be gone, got %q", string(data))
	}

	if _, err := idx.ReplaceRangeSource("greeter.go", 0, 1, "x"); err == nil {
		t.Fatalf("expected error for out-of-bounds start line")
	}
	if _, err := idx.ReplaceRangeSource("greeter.go", 3, 100, "x"); err == nil {
		t.Fatalf("expected error for out-of-bounds end line")
	}
}

func TestReplaceSymbolSourceWritesFileToDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "greeter.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Greet() string {\n\treturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write greeter file: %v", err)
	}

	idx := NewIndex(root, nil)
	symbols, err := idx.Outline("greeter.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	var symbolID string
	for _, symbol := range symbols {
		if symbol.Name == "Greet" {
			symbolID = symbol.ID
		}
	}
	if symbolID == "" {
		t.Fatalf("expected to find Greet symbol, got %#v", symbols)
	}

	oldSymbol, oldLines, err := idx.ReplaceSymbolSource(symbolID, "func Greet() string {\n\treturn \"hello\"\n}\n")
	if err != nil {
		t.Fatalf("replace symbol source: %v", err)
	}
	if oldSymbol.Name != "Greet" {
		t.Fatalf("expected replaced symbol to be Greet, got %#v", oldSymbol)
	}
	if len(oldLines) == 0 {
		t.Fatalf("expected displaced old lines to be returned")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("expected file on disk to contain new implementation, got %q", string(data))
	}
	if strings.Contains(string(data), "\"hi\"") {
		t.Fatalf("expected old implementation to be gone, got %q", string(data))
	}
}

func TestBuildSymbolGraphCapturesCallerCalleeEdges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "session.go"), []byte("package auth\n\nfunc RefreshSession() string { return \"ok\" }\nfunc ValidateSession() string { return RefreshSession() }\n"), 0o644); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	idx := NewIndex(root, nil)
	graph, err := idx.BuildSymbolGraph()
	if err != nil {
		t.Fatalf("build symbol graph: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Fatalf("expected graph nodes")
	}
	if len(graph.Edges) == 0 {
		t.Fatalf("expected call edges")
	}
	found := false
	for _, edge := range graph.Edges {
		if edge.From == "internal/auth/session.go::ValidateSession" && edge.To == "internal/auth/session.go::RefreshSession" && edge.Kind == "calls" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ValidateSession -> RefreshSession call edge, got %#v", graph.Edges)
	}
}

func TestRetrieveFlagsBudgetExceededForSingleOversizedCandidate(t *testing.T) {
	root := t.TempDir()
	longBody := strings.Repeat("x", 2000)
	content := "package huge\n\nfunc HugeFunc() string {\n\t// " + longBody + "\n\treturn \"done\"\n}\n"
	if err := os.WriteFile(filepath.Join(root, "huge.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write huge file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.Retrieve("hugefunc", 200)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("expected exactly one (oversized) candidate, got %#v", resp.Candidates)
	}
	if resp.UsedTokens <= resp.MaxTokens {
		t.Fatalf("expected UsedTokens to exceed MaxTokens for this oversized candidate, got used=%d max=%d", resp.UsedTokens, resp.MaxTokens)
	}
	if !resp.BudgetExceeded {
		t.Fatalf("expected BudgetExceeded=true, got response %#v", resp)
	}
	if !strings.Contains(resp.Summary, "budget exceeded") {
		t.Fatalf("expected summary to mention the budget was exceeded, got %q", resp.Summary)
	}
}

func TestRetrieveDoesNotFlagBudgetExceededWhenWithinBudget(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.go"), []byte("package small\n\nfunc SmallFunc() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.Retrieve("smallfunc", 4096)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if resp.BudgetExceeded {
		t.Fatalf("expected BudgetExceeded=false when comfortably within budget, got %#v", resp)
	}
	if strings.Contains(resp.Summary, "budget exceeded") {
		t.Fatalf("expected summary to not mention budget exceeded, got %q", resp.Summary)
	}
}

func TestSymbolCacheReturnsSameSliceForUnchangedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cached.go")
	if err := os.WriteFile(path, []byte("package cached\n\nfunc CachedFunc() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	first, err := idx.Outline("cached.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	if len(first) == 0 {
		t.Fatalf("expected symbols")
	}

	second, err := idx.Outline("cached.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	if len(second) == 0 {
		t.Fatalf("expected symbols on second call")
	}

	// A real reparse would allocate a new backing array; reusing the cached
	// slice means the second call's first element shares the same address
	// as the first call's.
	if &first[0] != &second[0] {
		t.Fatalf("expected the second call to reuse the cached symbol slice instead of reparsing the unchanged file")
	}
}

func TestSymbolCacheInvalidatesWhenFileChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cached.go")
	if err := os.WriteFile(path, []byte("package cached\n\nfunc First() string { return \"one\" }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	first, err := idx.Outline("cached.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	foundFirst := false
	for _, symbol := range first {
		if symbol.Name == "First" {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Fatalf("expected First symbol, got %#v", first)
	}

	if err := os.WriteFile(path, []byte("package cached\n\nfunc Second() string { return \"two-two\" }\n"), 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}

	second, err := idx.Outline("cached.go")
	if err != nil {
		t.Fatalf("outline: %v", err)
	}
	foundSecond := false
	for _, symbol := range second {
		if symbol.Name == "Second" {
			foundSecond = true
		}
	}
	if !foundSecond {
		t.Fatalf("expected cache to invalidate after the file changed and reparse to find Second, got %#v", second)
	}
}

func TestWorkspaceTreeListsFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "auth"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "auth", "session.go"), []byte("package auth\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.WorkspaceTree(0)
	if err != nil {
		t.Fatalf("workspace tree: %v", err)
	}

	byPath := map[string]protocol.WorkspaceTreeEntry{}
	for _, entry := range resp.Entries {
		byPath[entry.Path] = entry
	}

	if entry, ok := byPath["internal"]; !ok || !entry.IsDir {
		t.Fatalf("expected internal directory entry, got %#v", byPath)
	}
	if entry, ok := byPath["internal/auth/session.go"]; !ok || entry.IsDir {
		t.Fatalf("expected session.go file entry, got %#v", byPath)
	}
	if entry, ok := byPath["README.md"]; !ok || entry.IsDir {
		t.Fatalf("expected README.md file entry, got %#v", byPath)
	}
	if resp.Truncated {
		t.Fatalf("expected no truncation for a small tree")
	}
}

func TestWorkspaceTreePrunesSkippedDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "leftpad"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "leftpad", "index.js"), []byte("module.exports = {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	resp, err := idx.WorkspaceTree(0)
	if err != nil {
		t.Fatalf("workspace tree: %v", err)
	}

	for _, entry := range resp.Entries {
		if strings.Contains(entry.Path, "node_modules") {
			t.Fatalf("expected node_modules to be pruned entirely, got %#v", resp.Entries)
		}
	}

	found := false
	for _, entry := range resp.Entries {
		if entry.Path == "main.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected main.go to still be listed, got %#v", resp.Entries)
	}
}

func TestWorkspaceTreeTruncatesAtMaxEntries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(root, "file"+strings.Repeat("x", i+1)+".go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	idx := NewIndex(root, nil)
	resp, err := idx.WorkspaceTree(3)
	if err != nil {
		t.Fatalf("workspace tree: %v", err)
	}
	if len(resp.Entries) != 3 {
		t.Fatalf("expected exactly 3 entries after truncation, got %d", len(resp.Entries))
	}
	if !resp.Truncated {
		t.Fatalf("expected Truncated=true")
	}
}

func TestShouldSkipPathDoesNotFalsePositiveOnGitignore(t *testing.T) {
	// Regression: a substring check for "/.git" also matched "/.gitignore"
	// (which contains "/.git" as a prefix), incorrectly treating an
	// ordinary tracked file the same as VCS metadata.
	if shouldSkipPath(filepath.Join("repo", ".gitignore")) {
		t.Fatalf("expected .gitignore to NOT be treated as a skip path")
	}
	if !shouldSkipPath(filepath.Join("repo", ".git", "HEAD")) {
		t.Fatalf("expected a real .git directory to still be skipped")
	}
}

func TestShouldSkipPathMatchesBareSkipDirectoryItself(t *testing.T) {
	// Regression: the bare directory itself (no trailing separator, as
	// filepath.Walk passes it) didn't match a "/name/" substring pattern.
	if !shouldSkipPath(filepath.Join("repo", "node_modules")) {
		t.Fatalf("expected the bare node_modules directory itself to be skipped")
	}
}

func TestReadRangeReturnsVerbatimLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	content := "line one\nline two\nline three\nline four\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	got, err := idx.ReadRange("notes.txt", 2, 3)
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	want := "line two\nline three"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReadRangeRejectsOutOfBoundsRange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte("only one line\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	// An end past the end of the file is clamped, not rejected: "from here to
	// the end" is what that request means.
	if got, err := idx.ReadRange("notes.txt", 1, 5); err != nil || got != "only one line" {
		t.Fatalf("expected the end clamped to the last line, got %q, %v", got, err)
	}
	if _, err := idx.ReadRange("notes.txt", 4, 0); err == nil {
		t.Fatalf("expected an error for a start line past the end of the file")
	}
	// Zero means "unset", not "invalid" — an omitted integer arrives over
	// JSON-RPC as zero, so zero has to be usable for omission to be
	// expressible. A negative is still a caller mistake.
	if _, err := idx.ReadRange("notes.txt", -1, 1); err == nil {
		t.Fatalf("expected an error for a negative start line")
	}
}

func TestReadRangeWorksOnNonSymbolContent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	content := "key: value\nother: 1\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	got, err := idx.ReadRange("config.yaml", 1, 2)
	if err != nil {
		t.Fatalf("read range on non-symbol content: %v", err)
	}
	if got != "key: value\nother: 1" {
		t.Fatalf("expected the raw config lines, got %q", got)
	}
}

func TestCreateFileWritesNewFile(t *testing.T) {
	root := t.TempDir()

	idx := NewIndex(root, nil)
	if err := idx.CreateFile("new/nested/file.go", "package nested\n"); err != nil {
		t.Fatalf("create file: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "new", "nested", "file.go"))
	if err != nil {
		t.Fatalf("read back created file: %v", err)
	}
	if string(data) != "package nested\n" {
		t.Fatalf("expected written content, got %q", string(data))
	}
}

func TestCreateFileRefusesToOverwriteExisting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.go")
	if err := os.WriteFile(path, []byte("package original\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	if err := idx.CreateFile("existing.go", "package overwritten\n"); err == nil {
		t.Fatalf("expected an error when creating a file that already exists")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if string(data) != "package original\n" {
		t.Fatalf("expected original content to survive a refused overwrite, got %q", string(data))
	}
}

func TestCreateFileAddsTrailingNewlineIfMissing(t *testing.T) {
	root := t.TempDir()

	idx := NewIndex(root, nil)
	if err := idx.CreateFile("noeol.txt", "no trailing newline"); err != nil {
		t.Fatalf("create file: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "noeol.txt"))
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	if string(data) != "no trailing newline\n" {
		t.Fatalf("expected a trailing newline to be added, got %q", string(data))
	}
}

func TestDeleteFileRemovesFileAndReturnsLineCount(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "doomed.go")
	if err := os.WriteFile(path, []byte("package doomed\n\nfunc X() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	lines, err := idx.DeleteFile("doomed.go")
	if err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if lines != 3 {
		t.Fatalf("expected 3 removed lines, got %d", lines)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to actually be removed from disk")
	}
}

func TestDeleteFileEvictsCacheEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cached.go")
	if err := os.WriteFile(path, []byte("package cached\n\nfunc CachedFunc() string { return \"ok\" }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewIndex(root, nil)
	if _, err := idx.Outline("cached.go"); err != nil {
		t.Fatalf("outline: %v", err)
	}
	if _, ok := idx.cache[path]; !ok {
		t.Fatalf("expected a cache entry to exist after Outline")
	}

	if _, err := idx.DeleteFile("cached.go"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if _, ok := idx.cache[path]; ok {
		t.Fatalf("expected the cache entry to be evicted after delete")
	}
}
func writeAt(t *testing.T, root string, name string, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestReadRangeWithNoBoundsReadsTheWholeFile(t *testing.T) {
	// The case this exists for: go.mod, a Makefile and every JSON/YAML/TOML
	// config have no symbols to address, so without this each one is a `cat`.
	root := t.TempDir()
	content := "module probe\n\ngo 1.24\n\nrequire example.com/x v1.2.3\n"
	writeAt(t, root, "go.mod", content)

	got, err := NewIndex(root, nil).ReadRange("go.mod", 0, 0)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if !strings.Contains(got, "module probe") || !strings.Contains(got, "example.com/x") {
		t.Fatalf("expected the whole file, got:\n%s", got)
	}
}

func TestReadRangeWithOnlyAStartReadsToEndOfFile(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "notes.txt", "one\ntwo\nthree\nfour\n")

	got, err := NewIndex(root, nil).ReadRange("notes.txt", 3, 0)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if strings.Contains(got, "two") {
		t.Fatalf("expected to start at line 3, got:\n%s", got)
	}
	if !strings.Contains(got, "three") || !strings.Contains(got, "four") {
		t.Fatalf("expected everything from line 3 on, got:\n%s", got)
	}
}

func TestReadRangeWithOnlyAnEndStartsAtLineOne(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "notes.txt", "one\ntwo\nthree\n")

	got, err := NewIndex(root, nil).ReadRange("notes.txt", 0, 2)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("expected lines 1-2, got:\n%s", got)
	}
	if strings.Contains(got, "three") {
		t.Fatalf("expected the end bound honoured, got:\n%s", got)
	}
}

func TestReadRangeClampsAnEnormousFile(t *testing.T) {
	// One huge file must not blow the caller's context, and the clamp has to
	// say what to do instead rather than silently returning less.
	root := t.TempDir()
	var builder strings.Builder
	for i := 0; i < 5000; i++ {
		builder.WriteString("a line of text that is reasonably long\n")
	}
	writeAt(t, root, "big.txt", builder.String())

	got, err := NewIndex(root, nil).ReadRange("big.txt", 0, 0)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(got) > maxReadRangeBytes*2 {
		t.Fatalf("expected the read clamped, got %d bytes", len(got))
	}
	if !strings.Contains(got, "narrower line range") {
		t.Fatalf("expected the clamp to say what to do instead, got %d bytes", len(got))
	}
}

func TestReadRangeWholeFileLeavesSmallFilesIntact(t *testing.T) {
	// The clamp must not touch the files this feature is actually for.
	root := t.TempDir()
	content := "{\n  \"name\": \"probe\"\n}\n"
	writeAt(t, root, "package.json", content)

	got, err := NewIndex(root, nil).ReadRange("package.json", 0, 0)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(content) {
		t.Fatalf("expected the file verbatim, got:\n%s", got)
	}
}

func TestReadRangeOnAnEmptyFileIsEmptyNotAnError(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, "empty.txt", "")

	got, err := NewIndex(root, nil).ReadRange("empty.txt", 0, 0)
	if err != nil {
		t.Fatalf("expected an empty file to read empty, got %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("expected no content, got %q", got)
	}
}
