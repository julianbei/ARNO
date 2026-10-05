package edit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireRustfmt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rustfmt"); err != nil {
		t.Skip("rustfmt is not installed")
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Editing a file that declares modules must format that file and no other.
// `rustfmt lib.rs` also rewrites every file lib.rs declares, so an edit to
// one file reformatted fourteen others in a fleet session's Rust workspace.
func TestRustfmtFormatsOnlyTheEditedFileNotItsModules(t *testing.T) {
	requireRustfmt(t)
	dir := t.TempDir()
	writeIn(t, dir, "Cargo.toml", "[package]\nname = \"p\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	const childSource = "pub fn f(){let x=1;let _=x;}\n"
	writeIn(t, dir, "src/lib.rs", "mod a;\npub fn g(){a::f();}\n")
	writeIn(t, dir, "src/a.rs", childSource)

	formatted := formatFiles(dir, []string{"src/lib.rs"})

	if len(formatted) != 1 || formatted[0] != "src/lib.rs" {
		t.Errorf("only the edited file should be reported formatted, got %v", formatted)
	}
	if got := readString(t, filepath.Join(dir, "src/lib.rs")); !strings.Contains(got, "pub fn g() {") {
		t.Errorf("the edited file was not formatted:\n%s", got)
	}
	if got := readString(t, filepath.Join(dir, "src/a.rs")); got != childSource {
		t.Errorf("a module the edit never touched was reformatted:\n%s", got)
	}
}

// rustfmt assumes edition 2015 when run on a file, where async is a syntax
// error: the file was refused and silently never formatted.
func TestRustfmtUsesTheEditionTheProjectDeclares(t *testing.T) {
	requireRustfmt(t)
	dir := t.TempDir()
	writeIn(t, dir, "Cargo.toml", "[package]\nname = \"p\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	writeIn(t, dir, "src/lib.rs", "pub async fn h(){}\n")

	if formatted := formatFiles(dir, []string{"src/lib.rs"}); len(formatted) != 1 {
		t.Fatalf("an async file in a 2021 project should format, got %v", formatted)
	}
	if got := readString(t, filepath.Join(dir, "src/lib.rs")); got != "pub async fn h() {}\n" {
		t.Errorf("not formatted: %q", got)
	}
}

// The project's own rustfmt.toml decides the layout, including one nearer the
// file than the workspace's.
func TestRustfmtHonorsTheNearestRustfmtToml(t *testing.T) {
	requireRustfmt(t)
	dir := t.TempDir()
	writeIn(t, dir, "Cargo.toml", "[package]\nname = \"p\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	writeIn(t, dir, "rustfmt.toml", "hard_tabs = true\n")
	writeIn(t, dir, "src/lib.rs", "fn k(){\nlet z=1;\n}\n")

	formatFiles(dir, []string{"src/lib.rs"})
	if got := readString(t, filepath.Join(dir, "src/lib.rs")); !strings.Contains(got, "\n\tlet z = 1;") {
		t.Errorf("the project's hard_tabs was not honored:\n%q", got)
	}

	writeIn(t, dir, "src/sub/rustfmt.toml", "tab_spaces = 2\n")
	writeIn(t, dir, "src/sub/m.rs", "fn k(){\nlet z=1;\n}\n")
	formatFiles(dir, []string{"src/sub/m.rs"})
	if got := readString(t, filepath.Join(dir, "src/sub/m.rs")); !strings.Contains(got, "\n  let z = 1;") {
		t.Errorf("the config nearest the file should win:\n%q", got)
	}
}

func TestRustEditionResolution(t *testing.T) {
	dir := t.TempDir()

	if got := rustEdition(dir, dir); got != defaultRustEdition {
		t.Errorf("no manifest: got %q, want the default %q", got, defaultRustEdition)
	}

	writeIn(t, dir, "Cargo.toml", "[package]\nname = \"p\"\nedition = \"2018\" # old\n")
	if got := rustEdition(dir, dir); got != "2018" {
		t.Errorf("Cargo.toml edition: got %q", got)
	}

	// The project's rustfmt.toml sets one: a flag would override it.
	writeIn(t, dir, "rustfmt.toml", "edition = \"2024\"\n")
	if got := rustEdition(dir, dir); got != "" {
		t.Errorf("rustfmt.toml sets the edition, no flag should be passed, got %q", got)
	}
}

// A member crate that inherits its edition takes the workspace's.
func TestRustEditionIsInheritedFromTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "Cargo.toml", "[workspace]\nmembers = [\"m\"]\n[workspace.package]\nedition = \"2024\"\n")
	writeIn(t, dir, "m/Cargo.toml", "[package]\nname = \"m\"\nedition.workspace = true\n")

	if got := rustEdition(dir, filepath.Join(dir, "m", "src")); got != "2024" {
		t.Errorf("workspace edition: got %q, want 2024", got)
	}
}

// A project that keeps its formatter settings in ruff.toml has chosen ruff.
func TestRuffTomlWithAFormatSectionCountsAsDeclared(t *testing.T) {
	dir := t.TempDir()
	fakeFormatter(t, filepath.Join(dir, ".venv", "bin", "ruff"))

	writeIn(t, dir, "ruff.toml", "line-length = 100\n")
	if chosen, ok := formatterFor(dir, "a.py"); ok {
		t.Fatalf("ruff.toml without a [format] section is a linter config, got %+v", chosen)
	}

	writeIn(t, dir, "ruff.toml", "line-length = 100\n\n[format]\nquote-style = \"single\"\n")
	if chosen, ok := formatterFor(dir, "a.py"); !ok || chosen.name != "ruff" {
		t.Fatalf("expected ruff, got %+v", chosen)
	}
}

// A formatter that prints nothing must not empty the file it was given.
func TestStdinFormatterNeverWritesAnEmptyResult(t *testing.T) {
	dir := t.TempDir()
	writeIn(t, dir, "a.txt", "keep me\n")
	silent := filepath.Join(dir, "silent")
	if err := os.WriteFile(silent, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	changed := runFormatter(dir, formatter{name: "silent", binary: silent, viaStdin: true}, "a.txt")

	if changed {
		t.Error("an empty result is not a formatting change")
	}
	if got := readString(t, filepath.Join(dir, "a.txt")); got != "keep me\n" {
		t.Errorf("the file was emptied: %q", got)
	}
}
