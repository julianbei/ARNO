package jobs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlanValidationNamesWhatDecidedTheCommand(t *testing.T) {
	goDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.com/p\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, ok, err := PlanValidation(goDir, "build")
	if err != nil || !ok {
		t.Fatalf("a Go module should plan a build, got ok=%v err=%v", ok, err)
	}
	if plan.Name != "go" || plan.Source != "go.mod" || plan.Dir != goDir || plan.Kind != "build" {
		t.Fatalf("unexpected Go plan %+v", plan)
	}

	makeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(makeDir, "Makefile"), []byte("build:\n\techo built\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, ok, err = PlanValidation(makeDir, "build")
	if err != nil || !ok || plan.Name != "make" || plan.Source != "Makefile" {
		t.Fatalf("a Makefile target should plan make, got %+v ok=%v err=%v", plan, ok, err)
	}

	if _, ok, err := PlanValidation(t.TempDir(), "build"); ok || err != nil {
		t.Fatalf("an empty directory has nothing to plan, got ok=%v err=%v", ok, err)
	}
}

func TestRunPlanGivesTheExitStatusVerdict(t *testing.T) {
	r := NewRunner(nil)
	id := r.Start("tests")
	r.RunPlan(id, Plan{Kind: "tests", Name: "sh", Args: []string{"-c", "echo all good; exit 3"}, Dir: t.TempDir(), Timeout: 10 * time.Second})

	output, finished := r.Wait(id, 10*time.Second)
	if !finished {
		t.Fatal("the plan should finish")
	}
	if !output.Failed {
		t.Fatalf("a non-zero exit fails whatever the output says, got %+v", output)
	}
}
