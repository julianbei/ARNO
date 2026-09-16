package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/events"
)

func TestEditsWithoutALanguageServerGetASyntaxCheck(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ok.py":     "def parse(value):\n    return value.strip()\n",
		"broken.py": "def parse(value:\n    return value.strip()\n",
		"broken.ts": "export const normalize = (input: string): string => {\n\treturn input.toUpperCase(;\n};\n",
		"broken.rs": "fn main() {\n    let x = 1\n    println!(\"{}\", x);\n",
		"notes.md":  "# not code\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	index := NewIndex(root, events.NewBus())

	diagnostics, checker, unchecked, handled := index.LanguageServerDiagnostics("ok.py")
	if !handled || unchecked != "" || len(diagnostics) != 0 || !strings.HasPrefix(checker, "tree-sitter (syntax only;") {
		t.Errorf("a valid file must be checked by the syntax fallback, got %v %q %q %v", diagnostics, checker, unchecked, handled)
	}

	for _, name := range []string{"broken.py", "broken.ts", "broken.rs"} {
		diagnostics, checker, _, _ := index.LanguageServerDiagnostics(name)
		if len(diagnostics) == 0 {
			t.Errorf("%s: expected a syntax error, checker %q", name, checker)
			continue
		}
		if diagnostics[0].Line < 1 || diagnostics[0].Path != name {
			t.Errorf("%s: expected a positioned diagnostic, got %+v", name, diagnostics[0])
		}
	}

	if _, _, _, handled := index.LanguageServerDiagnostics("notes.md"); handled {
		t.Error("a file with no language must not be reported at all")
	}
}
