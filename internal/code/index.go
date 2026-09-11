package code

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/events"
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
}

func NewIndex(root string, bus *events.Bus) *Index {
	return &Index{root: root, bus: bus}
}

func (i *Index) Outline(path string) ([]Symbol, error) {
	absolute := i.resolvePath(path)
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

func (i *Index) OutlineStructured(path string) (OutlineSections, []Symbol, error) {
	absolute := i.resolvePath(path)
	rel, err := filepath.Rel(i.root, absolute)
	if err != nil {
		return OutlineSections{}, nil, err
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return OutlineSections{}, nil, err
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

	return sections, symbols, nil
}

func (i *Index) ReadSymbol(path string, symbolID string, maxLines int) (Symbol, string, error) {
	if maxLines <= 0 {
		maxLines = 150
	}

	absolute := i.resolvePath(path)
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
	for _, symbol := range symbols {
		if symbol.ID != symbolID {
			continue
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

	return Symbol{}, "", fmt.Errorf("symbol not found: %s", symbolID)
}

func (i *Index) SymbolsByName(path string, symbolName string) ([]Symbol, error) {
	absolute := i.resolvePath(path)
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
	if strings.EqualFold(filepath.Ext(absolute), ".go") {
		mode := strings.TrimSpace(strings.ToLower(os.Getenv(parserModeEnv)))
		if mode != "regex" {
			symbols, err := extractGoSymbolsTreeSitter(relPath, data)
			if err == nil && len(symbols) > 0 {
				return symbols, "tree-sitter"
			}
		}
		return extractSymbols(relPath, lines), "regex"
	}

	return extractSymbols(relPath, lines), "regex"
}

func (i *Index) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(i.root, path)
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
	{kind: "class", pattern: regexp.MustCompile(`^\s*type\s+([A-Z][A-Za-z0-9_]*)\s+struct\b`)},
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
		case "type":
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
