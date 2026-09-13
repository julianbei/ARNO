package jobs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/julianbei/jade/internal/events"
)

func writeFile(t *testing.T, dir string, name string, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestScopedTestCommandPerEcosystem(t *testing.T) {
	ava := t.TempDir()
	writeFile(t, ava, "package.json", `{"devDependencies": {"ava": "^6"}}`, 0o644)
	writeFile(t, ava, "node_modules/.bin/ava", "#!/bin/sh\n", 0o755)

	jest := t.TempDir()
	writeFile(t, jest, "package.json", `{"devDependencies": {"jest": "^29"}}`, 0o644)
	writeFile(t, jest, "node_modules/.bin/jest", "#!/bin/sh\n", 0o755)

	plainNode := t.TempDir()
	writeFile(t, plainNode, "package.json", `{}`, 0o644)

	venv := t.TempDir()
	writeFile(t, venv, "setup.py", "", 0o644)
	writeFile(t, venv, ".venv/bin/python", "#!/bin/sh\n", 0o755)

	cargo := t.TempDir()
	writeFile(t, cargo, "Cargo.toml", "[package]\nname = \"x\"\n", 0o644)

	cargoTargets := t.TempDir()
	writeFile(t, cargoTargets, "Cargo.toml", "[package]\nname = \"x\"\n\n[[test]]\nname = \"integration\"\npath = \"tests/tests.rs\"\n", 0o644)

	cases := []struct {
		name     string
		dir      string
		scope    TestScope
		wantName string
		wantArgs []string
	}{
		{"ava file", ava, TestScope{Kind: "file", File: "test/stream.ts"}, "node_modules/.bin/ava", []string{"test/stream.ts"}},
		{"ava test", ava, TestScope{Kind: "test", Test: "GET request"}, "node_modules/.bin/ava", []string{"--match", "*GET request*"}},
		{"jest test in file", jest, TestScope{Kind: "test", File: "a.test.ts", Test: "works"}, "node_modules/.bin/jest", []string{"a.test.ts", "-t", "works"}},
		{"node fallback", plainNode, TestScope{Kind: "file", File: "test/a.js"}, "node", []string{"--test", "test/a.js"}},
		{"pytest in venv", venv, TestScope{Kind: "test", File: "tests/test_utils.py", Test: "parse"}, ".venv/bin/python", []string{"-m", "pytest", "tests/test_utils.py", "-k", "parse"}},
		{"cargo test file", cargo, TestScope{Kind: "file", File: "tests/cli.rs"}, "cargo", []string{"test", "--test", "cli"}},
		{"cargo declared targets", cargoTargets, TestScope{Kind: "test", File: "tests/regression.rs", Test: "r3180"}, "cargo", []string{"test", "r3180"}},
	}
	for _, tc := range cases {
		name, args, err := scopedTestCommand(tc.dir, tc.scope, nil)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if name != tc.wantName || !reflect.DeepEqual(args, tc.wantArgs) {
			t.Errorf("%s: got %s %v, want %s %v", tc.name, name, args, tc.wantName, tc.wantArgs)
		}
	}
}

func TestScopedTestCommandWithoutARunner(t *testing.T) {
	missing := t.TempDir()
	writeFile(t, missing, "package.json", `{"devDependencies": {"vitest": "^2"}}`, 0o644)
	if _, _, err := scopedTestCommand(missing, TestScope{Kind: "file", File: "a.test.ts"}, nil); err == nil || !strings.Contains(err.Error(), "npm install") {
		t.Errorf("a runner that is not installed must say so, got %v", err)
	}

	bare := t.TempDir()
	_, _, err := scopedTestCommand(bare, TestScope{Kind: "file", File: "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "no validation command configured") {
		t.Errorf("no manifest must read as unavailable, got %v", err)
	}
	output := JobOutput{Raw: err.Error()}
	if got := Outcome(output, true); got != "unavailable" {
		t.Errorf("outcome: got %q", got)
	}
}

func TestChangedTestFiles(t *testing.T) {
	got := changedTestFiles([]string{"source/ky.ts", "test/main.ts", "src/a.spec.js", "tests/test_utils.py", "requests/utils.py"})
	want := []string{"test/main.ts", "src/a.spec.js", "tests/test_utils.py"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunScopedTestsRunsTheProjectRunner(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"devDependencies": {"ava": "^6"}}`, 0o644)
	writeFile(t, dir, "node_modules/.bin/ava", "#!/bin/sh\necho \"ran ava $*\"\n", 0o755)

	r := NewRunner(events.NewBus())
	id := r.Start("tests")
	r.RunScopedTests(id, dir, TestScope{Kind: "file", File: "test/main.ts"}, nil)
	output, finished := r.Wait(id, 10*time.Second)
	if !finished {
		t.Fatal("did not finish")
	}
	if !strings.Contains(output.Raw, "ran ava test/main.ts") || output.Failed {
		t.Errorf("expected the project's ava to run, got %+v", output)
	}
}
