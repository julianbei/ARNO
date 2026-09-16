package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/julianbei/arno/internal/events"
)

func TestProjectEnvActivatesVirtualEnvAndNodeBinaries(t *testing.T) {
	bare := t.TempDir()
	if env := projectEnv(bare); env != nil {
		t.Errorf("a project with neither must inherit the environment, got %d entries", len(env))
	}

	dir := t.TempDir()
	writeFile(t, dir, ".venv/bin/python", "#!/bin/sh\n", 0o755)
	writeFile(t, dir, "node_modules/.bin/tsc", "#!/bin/sh\n", 0o755)
	env := projectEnv(dir)

	last := map[string]string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		last[key] = value
	}
	absolute, _ := filepath.Abs(dir)
	parts := strings.Split(last["PATH"], string(os.PathListSeparator))
	if len(parts) < 2 || parts[0] != filepath.Join(absolute, ".venv", "bin") || parts[1] != filepath.Join(absolute, "node_modules", ".bin") {
		t.Errorf("PATH must start with the venv and node binaries, got %q", last["PATH"])
	}
	if last["VIRTUAL_ENV"] != filepath.Join(absolute, ".venv") {
		t.Errorf("VIRTUAL_ENV: got %q", last["VIRTUAL_ENV"])
	}
}

// A command that calls bare `python`, as a Makefile target does, gets the
// repository's interpreter.
func TestCommandsRunWithTheProjectVirtualEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".venv/bin/python", "#!/bin/sh\necho venv-python\n", 0o755)

	r := NewRunner(events.NewBus())
	id := r.Start("tests")
	r.RunCommand(id, dir, "sh", "-c", "python")
	output, finished := r.Wait(id, 10*time.Second)
	if !finished || !strings.Contains(output.Raw, "venv-python") {
		t.Errorf("expected the venv's python, got %+v", output)
	}
}
