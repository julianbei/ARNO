package edit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const formatTimeout = 20 * time.Second

// formatFiles runs the language formatter over touched files and returns the
// ones that were actually reformatted.
//
// Formatting is the gap that nothing else catches: `go build`, `go vet` and
// `go test` all pass on badly formatted code, so jade reported success at
// every step while a repository's formatting steadily degraded through
// accumulated edits. docs/scope.md Rule 2 names the formatter as a deterministic
// tool jade should already be using.
//
// A missing formatter is not an error. The edits themselves succeeded, and
// failing the whole batch because `gofmt` is absent would be worse than
// leaving the file unformatted.
// formatTouched formats files after a single edit, the same way Apply does
// for a batch.
//
// Every edit path runs this, because formatting drift was invisible to
// everything else jade reports: `go build`, `go vet` and `go test` all pass
// on badly formatted code, so jade said "success" at every step while a
// repository's formatting degraded through accumulated edits. Four files in
// this repo had drifted out of gofmt shape before anyone noticed.
//
// It runs before diagnostics are computed, so reported line numbers match
// the file as it finally stands rather than the pre-format version.
func (s *Service) formatTouched(paths ...string) []string {
	return formatFiles(s.workspace.Root(), paths)
}

func formatFiles(root string, paths []string) []string {
	byFormatter := make(map[string][]string)
	for _, path := range paths {
		if formatter := formatterFor(path); formatter != "" {
			byFormatter[formatter] = append(byFormatter[formatter], path)
		}
	}

	formatted := make([]string, 0, len(paths))
	for formatter, group := range byFormatter {
		binary, ok := lookFormatter(formatter)
		if !ok {
			continue
		}
		for _, path := range group {
			if runFormatter(root, binary, formatter, path) {
				formatted = append(formatted, path)
			}
		}
	}
	return formatted
}

// formatterFor maps an extension to its canonical formatter. Only formatters
// that are safe to run unattended and produce a single canonical result are
// listed — a formatter with project-specific configuration could reformat
// far more than the agent touched.
func formatterFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "gofmt"
	case ".rs":
		return "rustfmt"
	default:
		return ""
	}
}

func lookFormatter(formatter string) (string, bool) {
	if path, err := exec.LookPath(formatter); err == nil {
		return path, true
	}
	return "", false
}

// runFormatter rewrites one file in place, reporting whether the content
// actually changed so the response names only files that really moved.
func runFormatter(root string, binary string, formatter string, path string) bool {
	absolute := filepath.Join(root, path)

	before, err := os.ReadFile(absolute)
	if err != nil {
		return false
	}

	args := []string{"-w", absolute}
	if formatter == "rustfmt" {
		args = []string{absolute}
	}

	ctx, cancel := context.WithTimeout(context.Background(), formatTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		// A formatter that refuses (syntax error mid-batch, say) leaves the
		// file as the edits left it. The edits themselves still stand.
		return false
	}

	after, err := os.ReadFile(absolute)
	if err != nil {
		return false
	}
	return string(before) != string(after)
}
