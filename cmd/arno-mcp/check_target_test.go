package main

import (
	"strings"
	"testing"
)

func TestCheckListsProjectsAndTakesATarget(t *testing.T) {
	server, root := newTestMCPServer(t)
	writeWorkspaceFile(t, root, "api/go.mod", "module example.com/api\n\ngo 1.22\n")
	writeWorkspaceFile(t, root, "api/main.go", "package main\n\nfunc main() {}\n")
	writeWorkspaceFile(t, root, "docs/readme.md", "# docs\n")

	out, err := callText(t, server, "arno.check", map[string]interface{}{"kind": "build", "dryRun": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "api/ (go.mod)") || !strings.Contains(out, "target") {
		t.Fatalf("with no root command, check should list the projects and point to target, got:\n%s", out)
	}

	out, err = callText(t, server, "arno.check", map[string]interface{}{"kind": "build", "dryRun": true, "target": "api"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "go build") || !strings.Contains(out, "api") {
		t.Fatalf("a target should discover the project's own command, got:\n%s", out)
	}

	if _, err := callText(t, server, "arno.check", map[string]interface{}{"kind": "build", "dryRun": true, "target": "../elsewhere"}); err == nil {
		t.Fatal("a target outside the workspace must be refused")
	}
}
