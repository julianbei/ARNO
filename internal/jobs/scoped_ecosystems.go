package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RunScopedTests runs tests narrowed per scope with the project's own runner:
// go test for Go, the JavaScript runner the package depends on, pytest, or
// cargo test.
//
// Scoped runs used to be Go-only. In a TypeScript repository run_tests with a
// file ran `go test` and answered "go mod init ... FAIL" seven times across
// three benchmark tasks, so the agent fell back to the whole `npm run test` —
// lint, build and browser suites — whose unrelated failures cost it dozens of
// turns.
func (r *Runner) RunScopedTests(id string, dir string, scope TestScope, changedFiles []string) {
	if fileExists(filepath.Join(dir, "go.mod")) {
		r.RunScopedGoTests(id, dir, scope, changedFiles)
		return
	}
	if scope.Kind == "" || scope.Kind == "all" {
		r.RunValidationCommand(id, dir, "tests")
		return
	}
	name, args, err := scopedTestCommand(dir, scope, changedFiles)
	if err != nil {
		r.CompleteWithOutput(id, err.Error())
		return
	}
	if name == "" {
		r.CompleteWithOutput(id, "no changed test files to run")
		return
	}
	r.RunCommand(id, dir, name, args...)
}

// scopedTestCommand builds the scoped test command for a non-Go project. An
// empty name with no error means there is nothing to run.
func scopedTestCommand(dir string, scope TestScope, changedFiles []string) (string, []string, error) {
	var files []string
	testName := ""
	switch scope.Kind {
	case "file":
		if scope.File == "" {
			return "", nil, fmt.Errorf("file scope requires a file path")
		}
		files = []string{scope.File}
	case "test":
		if scope.Test == "" {
			return "", nil, fmt.Errorf("test scope requires a test name")
		}
		testName = scope.Test
		if scope.File != "" {
			files = []string{scope.File}
		}
	case "changed":
		files = changedTestFiles(changedFiles)
		if len(files) == 0 {
			return "", nil, nil
		}
	default:
		return "", nil, fmt.Errorf("unknown test scope: %q", scope.Kind)
	}

	switch {
	case fileExists(filepath.Join(dir, "package.json")):
		return nodeTestCommand(dir, files, testName)
	case fileExists(filepath.Join(dir, "Cargo.toml")):
		return "cargo", cargoTestArgs(dir, files, testName), nil
	case isPythonProject(dir):
		args := append([]string{"-m", "pytest"}, files...)
		if testName != "" {
			args = append(args, "-k", testName)
		}
		return pythonInterpreter(dir), args, nil
	}
	return "", nil, fmt.Errorf("no validation command configured for scoped tests: no go.mod, package.json, Cargo.toml or Python manifest at the workspace root — declare one with declare_command")
}

// nodeTestCommand picks the test runner the package depends on, run from
// node_modules/.bin so nothing is downloaded, and falls back to node's own.
func nodeTestCommand(dir string, files []string, testName string) (string, []string, error) {
	dependencies := packageDependencies(dir)
	for _, runner := range []string{"vitest", "jest", "ava", "mocha"} {
		if !dependencies[runner] {
			continue
		}
		binary := filepath.Join("node_modules", ".bin", runner)
		if !fileExists(filepath.Join(dir, binary)) {
			return "", nil, fmt.Errorf("no validation command configured: %s is a dependency but %s is missing — run npm install", runner, binary)
		}
		args := []string{}
		if runner == "vitest" {
			args = append(args, "run")
		}
		args = append(args, files...)
		if testName != "" {
			switch runner {
			case "vitest", "jest":
				args = append(args, "-t", testName)
			case "ava":
				// ava matches the whole title; the others match a part of
				// it, and agents pass part of a title. Six ky runs answered
				// "Couldn't find any matching tests" for a title's prefix.
				if !strings.Contains(testName, "*") {
					testName = "*" + testName + "*"
				}
				args = append(args, "--match", testName)
			case "mocha":
				args = append(args, "--grep", testName)
			}
		}
		return binary, args, nil
	}
	args := append([]string{"--test"}, files...)
	if testName != "" {
		args = append(args, "--test-name-pattern", testName)
	}
	return "node", args, nil
}

func packageDependencies(dir string) map[string]bool {
	dependencies := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return dependencies
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return dependencies
	}
	for name := range manifest.Dependencies {
		dependencies[name] = true
	}
	for name := range manifest.DevDependencies {
		dependencies[name] = true
	}
	return dependencies
}

// cargoTestArgs targets tests/<name>.rs as its own test binary, unless
// Cargo.toml declares [[test]] targets, where a file under tests/ is usually a
// module of one of them and --test <file> would name no target.
func cargoTestArgs(dir string, files []string, testName string) []string {
	args := []string{"test"}
	manifest, _ := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if !strings.Contains(string(manifest), "[[test]]") {
		for _, file := range files {
			slashed := filepath.ToSlash(file)
			if strings.HasPrefix(slashed, "tests/") && strings.Count(slashed, "/") == 1 && strings.HasSuffix(slashed, ".rs") {
				args = append(args, "--test", strings.TrimSuffix(strings.TrimPrefix(slashed, "tests/"), ".rs"))
			}
		}
	}
	if testName != "" {
		args = append(args, testName)
	}
	return args
}

func isPythonProject(dir string) bool {
	for _, manifest := range []string{"pyproject.toml", "setup.py", "setup.cfg", "pytest.ini", "tox.ini", "requirements.txt"} {
		if fileExists(filepath.Join(dir, manifest)) {
			return true
		}
	}
	return false
}

// pythonInterpreter prefers the repository's own virtual environment, where
// the project and its test dependencies are installed, over the system
// python3, which usually has neither.
func pythonInterpreter(dir string) string {
	for _, candidate := range []string{filepath.Join(".venv", "bin", "python"), filepath.Join("venv", "bin", "python")} {
		if fileExists(filepath.Join(dir, candidate)) {
			return candidate
		}
	}
	return "python3"
}

// changedTestFiles keeps the changed files that are tests by the naming
// conventions of JavaScript, Python and Rust projects.
func changedTestFiles(files []string) []string {
	tests := make([]string, 0, len(files))
	for _, file := range files {
		base := strings.ToLower(filepath.Base(file))
		slashed := "/" + filepath.ToSlash(file)
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
			strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
			strings.Contains(slashed, "/test/") || strings.Contains(slashed, "/tests/") || strings.Contains(slashed, "/__tests__/") {
			tests = append(tests, file)
		}
	}
	return tests
}
