package diagnostics

import (
	"bufio"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
)

// Service normalizes parser, LSP, and lint diagnostics.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Immediate(scope string) []protocol.Diagnostic {
	return nil
}

// DecisiveSummary extracts the smallest useful failure signal from raw output.
func DecisiveSummary(output string) string {
	lines := decisiveLines(output)
	if len(lines) == 0 {
		trimmed := strings.TrimSpace(output)
		if trimmed == "" {
			return "no output"
		}
		return firstNLines(trimmed, 3)
	}

	return strings.Join(lines, " | ")
}

func decisiveLines(output string) []string {
	markers := []string{
		"FAIL",
		"ERROR",
		"panic:",
		"fatal:",
		"assert",
		"exception",
	}

	out := make([]string, 0, 3)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		for _, marker := range markers {
			if strings.Contains(lower, strings.ToLower(marker)) {
				out = append(out, line)
				break
			}
		}
		if len(out) == 3 {
			break
		}
	}

	return out
}

func firstNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(s))
	lines := make([]string, 0, n)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) == n {
			break
		}
	}
	return strings.Join(lines, " | ")
}
