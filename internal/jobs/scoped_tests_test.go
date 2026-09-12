package jobs

import (
	"strings"
	"testing"
)

func TestGoTestArgsForScopeAll(t *testing.T) {
	args, err := goTestArgsForScope("/repo", TestScope{Kind: "all"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[0] != "test" || args[1] != "./..." {
		t.Fatalf("expected [test ./...], got %v", args)
	}
}

func TestGoTestArgsForScopeDefaultsToAll(t *testing.T) {
	args, err := goTestArgsForScope("/repo", TestScope{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[1] != "./..." {
		t.Fatalf("expected empty Kind to default to all, got %v", args)
	}
}

func TestGoTestArgsForScopeByTestName(t *testing.T) {
	args, err := goTestArgsForScope("/repo", TestScope{Kind: "test", Test: "TestFoo"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-run") || !strings.Contains(joined, "^TestFoo$") {
		t.Fatalf("expected a -run flag anchoring the exact test name, got %v", args)
	}
}

func TestGoTestArgsForScopeByTestRequiresName(t *testing.T) {
	if _, err := goTestArgsForScope("/repo", TestScope{Kind: "test"}, nil); err == nil {
		t.Fatalf("expected an error when test scope has no test name")
	}
}

func TestGoTestArgsForScopeByFile(t *testing.T) {
	args, err := goTestArgsForScope("/repo", TestScope{Kind: "file", File: "internal/code/index.go"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 2 || args[1] != "./internal/code" {
		t.Fatalf("expected [test ./internal/code], got %v", args)
	}
}

func TestGoTestArgsForScopeByFileRequiresFile(t *testing.T) {
	if _, err := goTestArgsForScope("/repo", TestScope{Kind: "file"}, nil); err == nil {
		t.Fatalf("expected an error when file scope has no file path")
	}
}

func TestGoTestArgsForScopeChangedDedupesAndSkipsNonGoFiles(t *testing.T) {
	changed := []string{
		"internal/code/index.go",
		"internal/code/index_test.go",
		"internal/jobs/discovery.go",
		"README.md",
	}
	args, err := goTestArgsForScope("/repo", TestScope{Kind: "changed"}, changed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Count(joined, "./internal/code") != 1 {
		t.Fatalf("expected internal/code deduped to one package arg, got %v", args)
	}
	if !strings.Contains(joined, "./internal/jobs") {
		t.Fatalf("expected internal/jobs package included, got %v", args)
	}
	if strings.Contains(joined, "README") {
		t.Fatalf("expected non-Go files to be skipped entirely, got %v", args)
	}
}

func TestGoTestArgsForScopeChangedWithNoGoFilesReturnsNil(t *testing.T) {
	args, err := goTestArgsForScope("/repo", TestScope{Kind: "changed"}, []string{"README.md", "salvage_state.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args != nil {
		t.Fatalf("expected nil args when no changed file is Go, got %v", args)
	}
}

func TestGoTestArgsForScopeUnknownKindErrors(t *testing.T) {
	if _, err := goTestArgsForScope("/repo", TestScope{Kind: "mystery"}, nil); err == nil {
		t.Fatalf("expected an error for an unknown scope kind")
	}
}

func TestRunScopedGoTestsCompletesWithNoChangesMessage(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("tests")
	r.RunScopedGoTests(id, "/repo", TestScope{Kind: "changed"}, []string{"README.md"})

	status, summary := waitForCompletion(t, r, id)
	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}
	if summary == "" {
		output, _ := r.Output(id)
		if !strings.Contains(output.Raw, "no changed Go packages") {
			t.Fatalf("expected an explanatory message for zero-Go-file changes, got %q", output.Raw)
		}
	}
}

func TestRunScopedGoTestsActuallyRunsForRealScope(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("tests")
	// Deliberately targets internal/protocol, NOT internal/jobs itself:
	// scoping this test's spawned "go test" at its own running package
	// would recursively re-run this very test (which spawns another "go
	// test ./internal/jobs", which runs this test again, ...), a real bug
	// caught while writing this test the first time. internal/protocol has
	// no test files, so it's a fast, side-effect-free real subprocess run.
	// "../.." from internal/jobs resolves to the repo root when tests run
	// with their package directory as the working directory.
	r.RunScopedGoTests(id, "../..", TestScope{Kind: "file", File: "internal/protocol/types.go"}, nil)

	status, _ := waitForCompletion(t, r, id)
	if status != "completed" {
		t.Fatalf("expected status completed, got %s", status)
	}

	output, ok := r.Output(id)
	if !ok {
		t.Fatalf("expected job output to be present")
	}
	if !strings.Contains(output.Raw, "internal/protocol") {
		t.Fatalf("expected output to reference the scoped package, got %q", output.Raw)
	}
}
