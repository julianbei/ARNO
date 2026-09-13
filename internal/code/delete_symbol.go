package code

import (
	"fmt"
	"os"
	"strings"
)

// DeleteSymbolSource removes a symbol's declaration entirely.
//
// jade had create_file, delete_file, replace_symbol, replace_range and
// replace_text but no way to delete a declaration, so removing one meant
// locating it by hand and scanning for its closing brace — which is exactly
// the `python3` heredoc that prompted this task. Tree-sitter already knows
// the symbol's exact range, so the brace matching never needed hand-rolling.
//
// It also removes the blank-line gap the declaration left behind. Deleting
// lines 10-14 out of a file where line 15 is blank and line 9 is blank leaves
// a double blank line, which `gofmt` then rewrites — turning a delete into a
// spurious two-line diff somewhere the caller never touched.
func (i *Index) DeleteSymbolSource(path string, symbolID string) (Symbol, []string, error) {
	symbol, _, _, err := i.locateSymbolPosition(path, symbolID)
	if err != nil {
		return Symbol{}, nil, err
	}

	absolute, err := i.resolvePath(path)
	if err != nil {
		return Symbol{}, nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return Symbol{}, nil, err
	}

	lines := splitLines(string(data))
	if symbol.From < 1 || symbol.To > len(lines) || symbol.From > symbol.To {
		return Symbol{}, nil, fmt.Errorf("symbol %s has invalid range", symbolID)
	}

	from, to := expandToSurroundingBlank(lines, symbol.From, symbol.To)
	removed := append([]string(nil), lines[from-1:to]...)

	remaining := append(append([]string(nil), lines[:from-1]...), lines[to:]...)
	updated := strings.Join(remaining, "\n")
	if len(remaining) > 0 && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}

	if err := writeFile(absolute, []byte(updated)); err != nil {
		return Symbol{}, nil, err
	}

	// The symbol cache keys on mtime and size, so the write self-invalidates.
	return symbol, removed, nil
}

// expandToSurroundingBlank widens a deletion range by one trailing blank
// line, or one leading blank line when the symbol ends the file, so the
// delete does not leave a doubled or dangling blank. It never consumes a
// blank on both sides: that would glue the neighbouring declarations
// together, which is a worse edit than leaving one extra blank line.
func expandToSurroundingBlank(lines []string, from int, to int) (int, int) {
	if to < len(lines) && strings.TrimSpace(lines[to]) == "" {
		return from, to + 1
	}
	if from > 1 && strings.TrimSpace(lines[from-2]) == "" {
		return from - 1, to
	}
	return from, to
}
