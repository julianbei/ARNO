package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCargoCrateAtItsLockedVersion(t *testing.T) {
	root, cargoHome := t.TempDir(), t.TempDir()
	t.Setenv("CARGO_HOME", cargoHome)
	write(t, filepath.Join(root, "Cargo.lock"), "[[package]]\nname = \"regex-syntax\"\nversion = \"0.8.11\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\n\n[[package]]\nname = \"ripgrep\"\nversion = \"14.1.1\"\n")
	write(t, filepath.Join(cargoHome, "registry", "src", "index.crates.io-abc", "regex-syntax-0.8.2", "src", "lib.rs"), "old")
	write(t, filepath.Join(cargoHome, "registry", "src", "index.crates.io-abc", "regex-syntax-0.8.11", "src", "lib.rs"), "locked")

	source, err := Resolve(root, "regex-syntax")
	if err != nil {
		t.Fatal(err)
	}
	if source.Version != "0.8.11" || !strings.HasSuffix(source.Dir, "regex-syntax-0.8.11") || source.Ecosystem != "cargo" {
		t.Errorf("expected the locked version, got %+v", source)
	}
}

func TestResolveGoModuleWithCaseEncoding(t *testing.T) {
	root, modCache := t.TempDir(), t.TempDir()
	t.Setenv("GOMODCACHE", modCache)
	write(t, filepath.Join(root, "go.mod"), "module example.com/x\n\ngo 1.21\n\nrequire (\n\tgithub.com/spf13/pflag v1.0.9\n\tgithub.com/BurntSushi/toml v1.3.2 // indirect\n)\n")
	write(t, filepath.Join(modCache, "github.com", "spf13", "pflag@v1.0.9", "flag.go"), "package pflag")
	write(t, filepath.Join(modCache, "github.com", "!burnt!sushi", "toml@v1.3.2", "decode.go"), "package toml")

	for name, want := range map[string]string{"pflag": "pflag@v1.0.9", "github.com/BurntSushi/toml": "toml@v1.3.2"} {
		source, err := Resolve(root, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.HasSuffix(source.Dir, want) {
			t.Errorf("%s: got %s", name, source.Dir)
		}
	}
}

func TestResolveNodeAndPythonPackages(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "node_modules", "@sindresorhus", "is", "package.json"), `{"version": "7.0.1"}`)
	write(t, filepath.Join(root, ".venv", "lib", "python3.11", "site-packages", "urllib3", "__init__.py"), "")
	if err := os.MkdirAll(filepath.Join(root, ".venv", "lib", "python3.11", "site-packages", "urllib3-2.2.1.dist-info"), 0o755); err != nil {
		t.Fatal(err)
	}

	node, err := Resolve(root, "@sindresorhus/is")
	if err != nil || node.Version != "7.0.1" || node.Ecosystem != "npm" {
		t.Errorf("node: %+v %v", node, err)
	}
	python, err := Resolve(root, "urllib3")
	if err != nil || python.Version != "2.2.1" || python.Ecosystem != "python" {
		t.Errorf("python: %+v %v", python, err)
	}
}

func TestResolveRefusesPathsAndReportsWhereItLooked(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../etc", "/etc/passwd", ""} {
		if _, err := Resolve(root, name); err == nil {
			t.Errorf("%q must be refused", name)
		}
	}
	write(t, filepath.Join(root, "Cargo.lock"), "[[package]]\nname = \"serde\"\nversion = \"1.0.0\"\n")
	t.Setenv("CARGO_HOME", t.TempDir())
	_, err := Resolve(root, "missing-crate")
	if err == nil || !strings.Contains(err.Error(), "Cargo.lock") {
		t.Errorf("expected the places looked, got %v", err)
	}
}
