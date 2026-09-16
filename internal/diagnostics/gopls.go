package diagnostics

import (
	"bufio"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/toolchain"
)

// goplsCheckTimeout bounds how long a single gopls check may run so it
// never blocks the synchronous diagnostics path indefinitely — the same
// "prove it, don't wait forever" discipline as the job runner's
// process-group timeout.
const goplsCheckTimeout = 5 * time.Second

// goplsDiagnosticLine matches gopls check's output format:
// "path/to/file.go:12:3: message" or "path/to/file.go:12:3-8: message".
var goplsDiagnosticLine = regexp.MustCompile(`^([^:]+):(\d+):(\d+)(?:-\d+)?:\s(.*)$`)

// goplsCheck runs `gopls check <relPath>` (a single-file diagnostics
// subcommand, not the full LSP protocol) to surface real type-level
// diagnostics beyond what a syntax-only parse can catch. The returned bool
// reports whether gopls actually ran and produced a real answer — false
// means "unavailable", not "clean", since those are not the same thing
// (gopls not being installed, or timing out, is a different situation than
// gopls running and finding zero problems) and callers must not conflate
// them into a false "no diagnostics" result.
func goplsCheck(root string, relPath string) ([]protocol.Diagnostic, bool) {
	binary, ok := toolchain.Gopls()
	if !ok {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), goplsCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "check", relPath)
	cmd.Dir = root
	output, _ := cmd.CombinedOutput()
	// gopls check exits non-zero when it finds diagnostics — that is
	// success for our purposes, not a failed invocation, so the exit error
	// itself is deliberately ignored here.

	if ctx.Err() != nil {
		return nil, false
	}

	return parseGoplsCheckOutput(relPath, string(output)), true
}

// parseGoplsCheckOutput extracts diagnostics from gopls check's plain-text
// output. Lines that don't match the expected format are ignored rather
// than treated as errors, since gopls may print incidental warnings to
// stderr/stdout in a different shape.
func parseGoplsCheckOutput(relPath string, output string) []protocol.Diagnostic {
	diagnostics := make([]protocol.Diagnostic, 0)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		match := goplsDiagnosticLine.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		line, _ := strconv.Atoi(match[2])
		column, _ := strconv.Atoi(match[3])
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Level:   protocol.DiagnosticError,
			Path:    relPath,
			Line:    line,
			Column:  column,
			Message: strings.TrimSpace(match[4]),
		})
	}
	return diagnostics
}
