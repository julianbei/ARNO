package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every tool that takes a path, given one that leaves the workspace, refuses
// with an error that names the root, and leaves the file outside untouched.
// The escapes: an absolute path, a `..` climb, and a symlink inside the
// workspace that points out of it.
func TestEveryPathToolRefusesToLeaveTheWorkspace(t *testing.T) {
	server, root := newTestMCPServer(t)
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.go")
	original := "package secret\n\nfunc Secret() int { return 1 }\n"
	if err := os.WriteFile(secret, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	escapes := map[string]string{
		"absolute": secret,
		"dotdot":   relativeFrom(t, root, secret),
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.go")); err == nil {
		escapes["symlink"] = "link.go"
	} else {
		t.Logf("symlinks unavailable, skipping that escape: %v", err)
	}
	// A new file inside a symlinked directory would land outside too.
	if err := os.Symlink(outsideDir, filepath.Join(root, "linkdir")); err == nil {
		escapes["symlinked dir"] = "linkdir/secret.go"
	}

	tools := map[string]func(path string) map[string]interface{}{
		"jade.outline": func(p string) map[string]interface{} { return map[string]interface{}{"path": p} },
		"jade.read_symbol": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "symbolName": "Secret"}
		},
		"jade.read_range": func(p string) map[string]interface{} { return map[string]interface{}{"path": p, "startLine": 1} },
		"jade.references": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "symbolName": "Secret"}
		},
		"jade.rename": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "symbolName": "Secret", "newName": "Leaked"}
		},
		"jade.context": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "symbolName": "Secret"}
		},
		"jade.history": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "symbolName": "Secret"}
		},
		"jade.replace_range": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "startLine": 1, "endLine": 1, "newCode": "package leaked"}
		},
		"jade.replace_text": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "oldText": "return 1", "newText": "return 2"}
		},
		"jade.insert":       func(p string) map[string]interface{} { return map[string]interface{}{"path": p, "text": "// leaked"} },
		"jade.replace_file": func(p string) map[string]interface{} { return map[string]interface{}{"path": p, "content": "leaked"} },
		"jade.delete_symbol": func(p string) map[string]interface{} {
			return map[string]interface{}{"path": p, "symbolName": "Secret"}
		},
		"jade.delete_file": func(p string) map[string]interface{} { return map[string]interface{}{"path": p} },
		"jade.replace_symbol": func(p string) map[string]interface{} {
			return map[string]interface{}{"symbolId": p + "::Secret@3", "newCode": "func Secret() int { return 2 }"}
		},
		"jade.apply": func(p string) map[string]interface{} {
			return map[string]interface{}{"edits": []interface{}{
				map[string]interface{}{"op": "replace_text", "path": p, "oldText": "return 1", "newText": "return 2"},
			}}
		},
	}

	for escape, path := range escapes {
		for tool, args := range tools {
			_, err := callText(t, server, tool, args(path))
			if err == nil {
				t.Errorf("%s via %s (%s): expected a refusal, got success", tool, escape, path)
			} else if !strings.Contains(err.Error(), root) {
				t.Errorf("%s via %s: refusal should name the workspace root, got: %v", tool, escape, err)
			}
		}

		// read_range with several ranges reports the escape per range.
		out, err := callText(t, server, "jade.read_range", map[string]interface{}{
			"ranges": []interface{}{map[string]interface{}{"path": path}},
		})
		if err == nil && !strings.Contains(out, "workspace root") {
			t.Errorf("read_range ranges via %s: expected a per-range refusal, got:\n%s", escape, out)
		}
	}

	// create_file is checked with fresh names: the outside target must not
	// exist for the refusal to be about the boundary rather than existence.
	for escape, path := range map[string]string{
		"absolute":      filepath.Join(outsideDir, "created.go"),
		"dotdot":        relativeFrom(t, root, filepath.Join(outsideDir, "created.go")),
		"symlinked dir": "linkdir/created.go",
	} {
		if escape == "symlinked dir" {
			if _, err := os.Lstat(filepath.Join(root, "linkdir")); err != nil {
				continue
			}
		}
		if _, err := callText(t, server, "jade.create_file", map[string]interface{}{"path": path, "content": "leaked"}); err == nil {
			t.Errorf("create_file via %s: expected a refusal", escape)
		}
		if _, err := os.Stat(filepath.Join(outsideDir, "created.go")); err == nil {
			t.Fatalf("create_file via %s wrote outside the workspace", escape)
		}
	}

	data, err := os.ReadFile(secret)
	if err != nil {
		t.Fatalf("the outside file is gone: %v", err)
	}
	if string(data) != original {
		t.Fatalf("the outside file was changed:\n%s", data)
	}
}

// grep and find walk the workspace. A symlinked file pointing out of it is not
// repository content, so neither reads through it.
func TestWalksDoNotReadThroughAnEscapingSymlink(t *testing.T) {
	server, root := newTestMCPServer(t)
	outside := filepath.Join(t.TempDir(), "secret.go")
	if err := os.WriteFile(outside, []byte("package secret\n\nfunc OutsideOnlyName() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeWorkspaceFile(t, root, "main.go", "package main\n\nfunc main() {}\n")

	for tool, args := range map[string]map[string]interface{}{
		"jade.grep": {"query": "OutsideOnlyName"},
		"jade.find": {"query": "OutsideOnlyName"},
	} {
		out, err := callText(t, server, tool, args)
		if err != nil {
			continue
		}
		if strings.Contains(out, "link.go") {
			t.Errorf("%s read through the escaping symlink:\n%s", tool, out)
		}
	}
}

func relativeFrom(t *testing.T, root string, target string) string {
	t.Helper()
	rel, err := filepath.Rel(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "..") {
		t.Fatalf("expected %s to be outside %s", target, root)
	}
	return rel
}
