package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/julianbei/jade/internal/events"
)

func runJob(t *testing.T, start func(r *Runner, id string)) JobOutput {
	t.Helper()
	r := NewRunner(events.NewBus())
	id := r.Start("tests")
	start(r, id)
	output, finished := r.Wait(id, 10*time.Second)
	if !finished {
		t.Fatal("did not finish")
	}
	return output
}

func TestProjectConfigDecidesTestAndCheckCommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/x\n", 0o644)
	writeFile(t, dir, ".jade/project.json", `{
		"areas": [
			{"path": ".", "language": "go", "test": "echo root-tests", "testName": "echo name {name}"},
			{"path": "web", "language": "typescript", "testFile": "echo web-file {file}"}
		],
		"env": {"vars": {"JADE_PROBE": "from-config"}}
	}`, 0o644)
	writeFile(t, dir, "web/src/a.test.ts", "", 0o644)

	file := runJob(t, func(r *Runner, id string) {
		r.RunScopedTests(id, dir, TestScope{Kind: "file", File: "web/src/a.test.ts"}, nil)
	})
	if strings.TrimSpace(file.Raw) != "web-file src/a.test.ts" {
		t.Errorf("file scope must use the area's testFile inside the area, got %q", file.Raw)
	}

	name := runJob(t, func(r *Runner, id string) {
		r.RunScopedTests(id, dir, TestScope{Kind: "test", Test: "retries"}, nil)
	})
	if strings.TrimSpace(name.Raw) != "name retries" {
		t.Errorf("test scope must use testName, got %q", name.Raw)
	}

	all := runJob(t, func(r *Runner, id string) { r.RunValidationCommand(id, dir, "tests") })
	if strings.TrimSpace(all.Raw) != "root-tests" {
		t.Errorf("check tests must use the config over go.mod, got %q", all.Raw)
	}

	env := runJob(t, func(r *Runner, id string) { r.RunCommand(id, dir, "sh", "-c", "echo $JADE_PROBE") })
	if strings.TrimSpace(env.Raw) != "from-config" {
		t.Errorf("configured vars must reach commands, got %q", env.Raw)
	}

	if described, _ := DescribeValidationCommand(dir, "tests"); !strings.Contains(described, "from .jade/project.json") {
		t.Errorf("the description must name the config, got %q", described)
	}
	if described, _ := DescribeValidationCommand(dir, "build"); strings.Contains(described, "project.json") {
		t.Errorf("an undeclared kind falls back to discovery, got %q", described)
	}
}

func TestInvalidProjectConfigFailsInsteadOfGuessing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/x\n", 0o644)
	writeFile(t, dir, ".jade/project.json", `{"areas": [{"path": "..", "test": "rm -rf /"}]}`, 0o644)

	output := runJob(t, func(r *Runner, id string) { r.RunValidationCommand(id, dir, "tests") })
	if !output.Failed || !strings.Contains(output.Raw, "leaves the workspace") {
		t.Errorf("expected a failed job naming the problem, got %+v", output)
	}
	scoped := runJob(t, func(r *Runner, id string) {
		r.RunScopedTests(id, dir, TestScope{Kind: "test", Test: "x"}, nil)
	})
	if !scoped.Failed {
		t.Errorf("a scoped run must not fall back to discovery on an invalid config, got %+v", scoped)
	}
}

func TestDraftProjectArea(t *testing.T) {
	goDir := t.TempDir()
	writeFile(t, goDir, "go.mod", "module example.com/x\n", 0o644)
	area := DraftProjectArea(goDir)
	if area.Language != "go" || area.Build != "go build ./..." || area.Test != "go test ./..." || area.TestName == "" {
		t.Errorf("go draft: %+v", area)
	}

	ava := t.TempDir()
	writeFile(t, ava, "package.json", `{"devDependencies": {"ava": "^6"}}`, 0o644)
	writeFile(t, ava, "tsconfig.json", `{}`, 0o644)
	writeFile(t, ava, "node_modules/.bin/ava", "#!/bin/sh\n", 0o755)
	writeFile(t, ava, "node_modules/.bin/tsc", "#!/bin/sh\n", 0o755)
	area = DraftProjectArea(ava)
	if area.Language != "typescript" || area.TestFile != "node_modules/.bin/ava {file}" || area.TestName != "node_modules/.bin/ava --match *{name}*" {
		t.Errorf("typescript draft: %+v", area)
	}
	if area.Typecheck != "node_modules/.bin/tsc --noEmit -p tsconfig.json" {
		t.Errorf("typecheck draft: %q", area.Typecheck)
	}
}
