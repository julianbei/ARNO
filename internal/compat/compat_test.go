package compat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetenvPrefersTheNewName(t *testing.T) {
	t.Setenv("ARNO_TELEMETRY", "0")
	t.Setenv("JADE_TELEMETRY", "1")
	if got := Getenv("ARNO_TELEMETRY"); got != "0" {
		t.Fatalf("expected the ARNO_ value to win, got %q", got)
	}
}

func TestGetenvFallsBackToJade(t *testing.T) {
	t.Setenv("ARNO_STATE_DIR", "")
	t.Setenv("JADE_STATE_DIR", "/tmp/state")
	if got := Getenv("ARNO_STATE_DIR"); got != "/tmp/state" {
		t.Fatalf("expected the JADE_ value as a fallback, got %q", got)
	}
}

func TestGetenvIgnoresUnrelatedNames(t *testing.T) {
	t.Setenv("PATH_TO_NOWHERE", "")
	if got := Getenv("PATH_TO_NOWHERE"); got != "" {
		t.Fatalf("expected no value for a name without the ARNO_ prefix, got %q", got)
	}
}

func TestStatePathPrefersArnoDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ArnoDir, "commands.json"), "{}")
	write(t, filepath.Join(root, LegacyDir, "commands.json"), "{}")

	want := filepath.Join(root, ArnoDir, "commands.json")
	if got := StatePath(root, "commands.json"); got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestStatePathReadsJadeDirWhenItIsTheOnlyOne(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, LegacyDir, "commands.json"), "{}")

	want := filepath.Join(root, LegacyDir, "commands.json")
	if got := StatePath(root, "commands.json"); got != want {
		t.Fatalf("expected the legacy path %s, got %s", want, got)
	}
}

// A workspace with neither directory writes the new one, so nothing recreates
// .jade/ after someone renames it.
func TestStatePathWritesArnoDirWhenNeitherExists(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, ArnoDir, "telemetry.jsonl")
	if got := StatePath(root, "telemetry.jsonl"); got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestToolName(t *testing.T) {
	cases := map[string]string{
		"jade.find":       "arno.find",
		"jade_find":       "arno.find",
		"jade.read_range": "arno.read_range",
		"arno.find":       "",
		"find":            "",
		"jade.":           "",
	}
	for name, want := range cases {
		if got := ToolName(name); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", name, got, want)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
