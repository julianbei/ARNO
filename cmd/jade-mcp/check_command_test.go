package main

import (
	"os/exec"
	"strings"
	"testing"
)

// A pass on the wrong target is worse than no check, so the caller must be
// able to see the target before trusting the verdict.
func TestCheckDryRunNamesTheCommandWithoutRunningIt(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "go.mod", "module drydemo\n\ngo 1.22\n")
	// Would not compile. A dry run that actually ran the build would fail.
	writeWorkspaceFile(t, root, "main.go", "package main\n\nfunc main() { undefinedThing() }\n")

	out, err := callText(t, server, "jade.check", map[string]interface{}{"kind": "build", "dryRun": true})
	if err != nil {
		t.Fatalf("check dryRun: %v", err)
	}
	head := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(head, "would run: go build") {
		t.Fatalf("expected the command named in the first line, got:\n%s", out)
	}
	if strings.Contains(out, "undefined") || strings.Contains(out, "FAIL") {
		t.Fatalf("a dry run must not execute the build, got:\n%s", out)
	}
}

func TestCheckResultNamesTheCommandThatRan(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "go.mod", "module rundemo\n\ngo 1.22\n")
	writeWorkspaceFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	out, err := callText(t, server, "jade.check", map[string]interface{}{"kind": "build", "timeoutSeconds": 120})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	head := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(head, "pass build") || !strings.Contains(head, "ran: go build") {
		t.Fatalf("expected a pass that names its command, got:\n%s", out)
	}
}

// No manifest means no idea what to run. Saying so beats starting a job that
// can only fail for a reason unrelated to the code.
func TestCheckWithNoRecognisableProjectSaysSoUpFront(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "notes.md", "# nothing to build\n")

	out, err := callText(t, server, "jade.check", map[string]interface{}{"kind": "tests"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out, "no tests command found") || !strings.Contains(out, "declare_command") {
		t.Fatalf("expected an up-front no-command answer pointing at declare_command, got:\n%s", out)
	}
	if strings.Contains(out, "job-") {
		t.Fatalf("no job should start when there is nothing to run, got:\n%s", out)
	}
}
