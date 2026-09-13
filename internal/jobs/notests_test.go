package jobs

import (
	"reflect"
	"testing"
)

func TestNoTestsRan(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"cargo matched nothing", "running 0 tests\n\ntest result: ok. 0 passed; 0 failed\n\nrunning 0 tests\n", true},
		{"cargo ran one", "running 0 tests\n\nrunning 1 test\ntest regression::r3180 ... ok\n", false},
		{"go matched nothing", "ok  \tgithub.com/spf13/cobra\t0.2s [no tests to run]\n", true},
		{"go ran in one package", "ok  \tgithub.com/spf13/cobra\t0.2s [no tests to run]\nok  \tgithub.com/spf13/cobra/doc\t0.3s\n", false},
		{"ordinary pass", "ok  \tgithub.com/spf13/cobra\t0.2s\n", false},
	}
	for _, tc := range cases {
		if got := NoTestsRan(tc.output); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestCargoArgsForWorkspaceCrates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Cargo.toml", "[package]\nname = \"ripgrep\"\n\n[workspace]\nmembers = [\"crates/regex\"]\n\n[[test]]\nname = \"integration\"\npath = \"tests/tests.rs\"\n", 0o644)
	writeFile(t, root, "crates/regex/Cargo.toml", "[package]\nname = \"grep-regex\"\nversion = \"0.1.0\"\n\n[dependencies]\nname-like = \"1\"\n", 0o644)

	cases := []struct {
		name  string
		scope TestScope
		want  []string
	}{
		{"crate source file", TestScope{Kind: "test", File: "crates/regex/src/matcher.rs", Test: "whole_line"}, []string{"test", "-p", "grep-regex", "whole_line"}},
		{"name across the workspace", TestScope{Kind: "test", Test: "whole_line"}, []string{"test", "--workspace", "whole_line"}},
		{"root package test module", TestScope{Kind: "file", File: "tests/regression.rs"}, []string{"test", "-p", "ripgrep"}},
	}
	for _, tc := range cases {
		name, args, err := scopedTestCommand(root, tc.scope, nil)
		if err != nil || name != "cargo" || !reflect.DeepEqual(args, tc.want) {
			t.Errorf("%s: got %s %v %v, want %v", tc.name, name, args, err, tc.want)
		}
	}
}
