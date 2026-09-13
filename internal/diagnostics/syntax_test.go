package diagnostics

import (
	"strings"
	"testing"
)

func TestBrokenJSONReportsLineAndColumn(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", "{\n  \"name\": \"x\",\n  \"version\": 1,,\n}\n")

	result := NewService(root).Check("package.json")
	if result.Checker != "json" {
		t.Fatalf("expected the json checker named, got %+v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Line != 3 {
		t.Fatalf("expected one error on line 3, got %+v", result.Diagnostics)
	}
	if !strings.Contains(result.Diagnostics[0].Message, "invalid JSON") {
		t.Fatalf("message should say what failed: %q", result.Diagnostics[0].Message)
	}
}

func TestValidJSONIsCheckedAndClean(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.json", `{"a": [1, 2, {"b": null}]}`)

	result := NewService(root).Check("a.json")
	if result.Checker != "json" || len(result.Diagnostics) != 0 {
		t.Fatalf("expected checked and clean, got %+v", result)
	}
}

// A first value that parses must not hide garbage after it.
func TestTrailingGarbageAfterAValidJSONValueIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.json", "{}\n{\n")
	if result := NewService(root).Check("a.json"); len(result.Diagnostics) != 1 {
		t.Fatalf("expected an error for the unterminated second value, got %+v", result)
	}
}

// tsconfig.json allows comments; a strict parse would report errors tsc does
// not.
func TestJSONWithCommentsFilesAreNotReportedAsBroken(t *testing.T) {
	root := t.TempDir()
	write(t, root, "tsconfig.json", "{\n  // strict mode\n  \"compilerOptions\": {},\n}\n")
	if result := NewService(root).Check("tsconfig.json"); len(result.Diagnostics) != 0 {
		t.Fatalf("tsconfig.json comments reported as errors: %+v", result)
	}
}

func TestBrokenYAMLReportsTheLine(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ci.yml", "name: ci\non:\n  push:\n jobs: [\n")

	result := NewService(root).Check("ci.yml")
	if result.Checker != "yaml" || len(result.Diagnostics) != 1 {
		t.Fatalf("expected one yaml error, got %+v", result)
	}
	if result.Diagnostics[0].Line == 0 {
		t.Fatalf("expected a line number, got %+v", result.Diagnostics[0])
	}
}

// Kubernetes manifests put several documents in one file; an error in the
// second must still be found.
func TestYAMLErrorInALaterDocumentIsFound(t *testing.T) {
	root := t.TempDir()
	write(t, root, "deploy.yaml", "kind: A\n---\nkind: B\n  bad: [\n")
	if result := NewService(root).Check("deploy.yaml"); len(result.Diagnostics) != 1 {
		t.Fatalf("expected the second document's error, got %+v", result)
	}
}

func TestValidMultiDocumentYAMLIsClean(t *testing.T) {
	root := t.TempDir()
	write(t, root, "deploy.yaml", "kind: A\n---\nkind: B\nitems: [1, 2]\n")
	result := NewService(root).Check("deploy.yaml")
	if result.Checker != "yaml" || len(result.Diagnostics) != 0 {
		t.Fatalf("expected checked and clean, got %+v", result)
	}
}
