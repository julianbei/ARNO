package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/toolchain"
)

func writeGoFile(t *testing.T, dir string, name string, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// goplsInstalled reports whether the real LSP path can be exercised here.
// gopls is absent in most CI/sandbox environments, so the approximate path
// and the graceful-degradation contract are what these tests can prove.
func goplsInstalled() bool {
	_, ok := toolchain.Gopls()
	return ok
}

func TestReferencesFallsBackToApproximateWhenGoplsMissing(t *testing.T) {
	if goplsInstalled() {
		t.Skip("gopls installed — the fallback path cannot be forced here")
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", `package fixture

func Target() int {
	return 1
}
`)
	writeGoFile(t, dir, "caller.go", `package fixture

func Caller() int {
	return Target()
}
`)

	idx := NewIndex(dir, nil)
	response, err := idx.References("target.go", "target.go::Target@3")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if response.Source != "approximate" {
		t.Fatalf("expected the approximate source without gopls, got %q", response.Source)
	}
	if len(response.References) != 1 || response.References[0].Path != "caller.go" {
		t.Fatalf("expected caller.go to reference Target, got %+v", response.References)
	}
	// The summary must carry the caveat: a name-matched edge is a weaker
	// claim than a compiler-resolved one and must not read as authoritative.
	if !strings.Contains(response.Summary, "approximate") {
		t.Fatalf("expected the summary to disclose approximation, got %q", response.Summary)
	}
}

// TestReferencesUsesGoplsWhenAvailable exercises the path that could not be
// proven at all until gopls was installed: a real compiler-resolved lookup
// returning real line and column numbers, which the approximate graph cannot
// produce.
func TestReferencesUsesGoplsWhenAvailable(t *testing.T) {
	if !goplsInstalled() {
		t.Skip("gopls not available in this environment")
	}

	dir := t.TempDir()
	writeGoFile(t, dir, "go.mod", "module fixture\n\ngo 1.21\n")
	writeGoFile(t, dir, "target.go", `package fixture

func Target() int {
	return 1
}
`)
	writeGoFile(t, dir, "caller.go", `package fixture

func Caller() int {
	return Target()
}
`)

	idx := NewIndex(dir, nil)
	response, err := idx.References("target.go", "target.go::Target@3")
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if response.Source != "lsp" {
		t.Fatalf("expected gopls to answer, got source %q (%s)", response.Source, response.Summary)
	}

	// gopls reports the declaration plus the call site; the approximate
	// path could never report either with a real line number.
	var sawCallSite bool
	for _, ref := range response.References {
		if ref.Path == "caller.go" && ref.Line == 4 && ref.Column > 0 {
			sawCallSite = true
		}
	}
	if !sawCallSite {
		t.Fatalf("expected the call site at caller.go:4 with a real column, got %+v", response.References)
	}
}

func TestReferencesRejectsUnknownSymbol(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() {}\n")

	idx := NewIndex(dir, nil)
	if _, err := idx.References("target.go", "target.go::Nope@1"); err == nil {
		t.Fatalf("expected an error for a symbol that does not exist")
	}
}

func TestLocateSymbolPositionFindsNameColumn(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() {}\n")

	idx := NewIndex(dir, nil)
	symbol, line, column, err := idx.locateSymbolPosition("target.go", "target.go::Target@3")
	if err != nil {
		t.Fatalf("locateSymbolPosition: %v", err)
	}
	if symbol.Name != "Target" {
		t.Fatalf("expected Target, got %q", symbol.Name)
	}
	if line != 3 {
		t.Fatalf("expected line 3, got %d", line)
	}
	// "func Target" — T sits at 1-based column 6, which is what gopls expects.
	if column != 6 {
		t.Fatalf("expected column 6, got %d", column)
	}
}

func TestParseGoplsReferencesHandlesBothPositionForms(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir, nil)

	output := filepath.Join(dir, "caller.go") + ":4:9-15\n" +
		filepath.Join(dir, "pkg", "other.go") + ":12:2\n" +
		"gopls: some unrelated chatter\n"

	refs := idx.parseGoplsReferences(output)
	if len(refs) != 2 {
		t.Fatalf("expected 2 parsed references, got %d: %+v", len(refs), refs)
	}
	if refs[0].Path != "caller.go" || refs[0].Line != 4 || refs[0].Column != 9 {
		t.Fatalf("unexpected first reference: %+v", refs[0])
	}
	if refs[1].Path != "pkg/other.go" || refs[1].Line != 12 || refs[1].Column != 2 {
		t.Fatalf("unexpected second reference: %+v", refs[1])
	}
}

func TestGoplsReferencesReportsUnavailableDistinctFromEmpty(t *testing.T) {
	if goplsInstalled() {
		t.Skip("gopls installed — unavailability cannot be forced here")
	}
	idx := NewIndex(t.TempDir(), nil)
	refs, ran := idx.goplsReferences("target.go", 3, 6)
	if ran {
		t.Fatalf("expected ran=false when gopls is absent")
	}
	if refs != nil {
		t.Fatalf("expected no references when gopls never ran, got %+v", refs)
	}
}

func TestApproximateReferencesDeduplicatesCallers(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "target.go", "package fixture\n\nfunc Target() int { return 1 }\n")
	writeGoFile(t, dir, "caller.go", `package fixture

func Caller() int {
	// Two call sites in one function collapse to a single referencing symbol.
	return Target() + Target()
}
`)

	idx := NewIndex(dir, nil)
	symbol, _, _, err := idx.locateSymbolPosition("target.go", "target.go::Target@3")
	if err != nil {
		t.Fatalf("locateSymbolPosition: %v", err)
	}
	refs := idx.approximateReferences("target.go::Target@3", symbol)
	if len(refs) != 1 {
		t.Fatalf("expected duplicate call sites to collapse to 1 reference, got %d: %+v", len(refs), refs)
	}
}

func TestRelativePathNormalizesAbsoluteGoplsOutput(t *testing.T) {
	dir := t.TempDir()
	idx := NewIndex(dir, nil)

	if got := idx.relativePath(filepath.Join(dir, "pkg", "file.go")); got != "pkg/file.go" {
		t.Fatalf("expected pkg/file.go, got %q", got)
	}
	if got := idx.relativePath("already/relative.go"); got != "already/relative.go" {
		t.Fatalf("expected the relative path unchanged, got %q", got)
	}
}
