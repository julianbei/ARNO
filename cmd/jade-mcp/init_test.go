package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitDraftsProjectConfigOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runInit([]string{"--root", root}, &out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".jade", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"env"`) {
		t.Errorf("a draft with no environment must not write an empty env:\n%s", data)
	}
	if !strings.Contains(string(data), `"build": "go build ./..."`) || !strings.Contains(out.String(), "Review it and commit it") {
		t.Errorf("unexpected draft:\n%s\n%s", data, out.String())
	}
	if err := runInit([]string{"--root", root}, &out); err == nil {
		t.Error("init must not overwrite an existing config")
	}
}

func TestInstructionsCarryTheProjectNotes(t *testing.T) {
	root := t.TempDir()
	if got := projectInstructions(root); got != serverInstructions {
		t.Errorf("without a config the instructions are unchanged, got %q", got)
	}
	if err := os.MkdirAll(filepath.Join(root, ".jade"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"areas": [{"path": "web", "language": "typescript"}], "notes": "Browser tests need Playwright."}`
	if err := os.WriteFile(filepath.Join(root, ".jade", "project.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	got := projectInstructions(root)
	if !strings.Contains(got, "areas web (typescript)") || !strings.Contains(got, "Browser tests need Playwright.") {
		t.Errorf("got %q", got)
	}
}
