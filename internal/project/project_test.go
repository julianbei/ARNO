package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, RelPath), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoad(t *testing.T) {
	if config, err := Load(t.TempDir()); config != nil || err != nil {
		t.Errorf("a missing config is nil without error, got %v %v", config, err)
	}
	for name, content := range map[string]string{
		"not json":      `{"areas": [`,
		"unknown field": `{"areas": [], "tset": "go test"}`,
		"escaping path": `{"areas": [{"path": "../other", "test": "make"}]}`,
		"absolute path": `{"areas": [{"path": "/etc", "test": "make"}]}`,
	} {
		if _, err := Load(writeConfig(t, content)); err == nil || !strings.Contains(err.Error(), Source) {
			t.Errorf("%s: expected an error naming %s, got %v", name, Source, err)
		}
	}
}

func TestCommandsPerArea(t *testing.T) {
	config, err := Load(writeConfig(t, `{
		"areas": [
			{"path": ".", "language": "go", "build": "go build ./...", "test": "go test ./...", "testName": "go test -run {name} ./..."},
			{"path": "web/", "language": "typescript", "build": "npm run build", "testFile": "node_modules/.bin/vitest run {file}", "testName": "node_modules/.bin/vitest run {file} -t {name}"},
			{"path": "tools/ava", "testName": "node_modules/.bin/ava {file} --match *{name}*"}
		],
		"generated": ["web/dist/", "*.pb.go"]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := config.Command("build"); got != "go build ./... && (cd 'web' && npm run build)" {
		t.Errorf("build: %q", got)
	}
	if got, ok := config.Command("typecheck"); ok {
		t.Errorf("no area declares typecheck, got %q", got)
	}
	if got, _ := config.TestFileCommand("web/src/client.test.ts"); got != "(cd 'web' && node_modules/.bin/vitest run 'src/client.test.ts')" {
		t.Errorf("test file: %q", got)
	}
	if _, ok := config.TestFileCommand("cmd/main_test.go"); ok {
		t.Error("the root area declares no testFile, so discovery decides")
	}
	if got, _ := config.TestNameCommand("", "it's"); got != `go test -run 'it'"'"'s' ./...` {
		t.Errorf("test name without file: %q", got)
	}
	if got, _ := config.TestNameCommand("web/a.test.ts", "retries"); got != "(cd 'web' && node_modules/.bin/vitest run 'a.test.ts' -t 'retries')" {
		t.Errorf("test name with file: %q", got)
	}
	if got, _ := config.TestNameCommand("tools/ava/test/x.ts", "GET request"); got != "(cd 'tools/ava' && node_modules/.bin/ava 'test/x.ts' --match '*GET request*')" {
		t.Errorf("glob name: %q", got)
	}

	for rel, want := range map[string]bool{"web/dist/app.js": true, "web/dist": true, "api/v1/user.pb.go": true, "web/src/app.ts": false} {
		if got := config.IsGenerated(rel); got != want {
			t.Errorf("generated %s: got %v", rel, got)
		}
	}
	if got := config.Summary(); got != "areas . (go), web (typescript), tools/ava" {
		t.Errorf("summary: %q", got)
	}
}
