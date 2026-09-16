package internalapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/pathguard"
)

func TestDependencyHintForCachePaths(t *testing.T) {
	cases := map[string]string{
		"/Users/me/.cargo/registry/src/index.crates.io-1949cf8c6b5b557f/regex-syntax-0.8.11/src/hir/mod.rs": "dep:regex-syntax/src/hir/mod.rs",
		"/Users/me/go/pkg/mod/github.com/spf13/pflag@v1.0.9/flag.go":                                        "dep:pflag/flag.go",
		"/repo/node_modules/@sindresorhus/is/distribution/index.js":                                         "dep:@sindresorhus/is/distribution/index.js",
		"/repo/.venv/lib/python3.11/site-packages/urllib3/connection.py":                                    "dep:urllib3/connection.py",
	}
	for path, want := range cases {
		err := withDependencyHint(path, fmt.Errorf("%w: %s is not inside the workspace root /repo", pathguard.ErrOutsideWorkspace, path))
		if !strings.Contains(err.Error(), want) || !errors.Is(err, pathguard.ErrOutsideWorkspace) {
			t.Errorf("%s: got %v", path, err)
		}
	}

	plain := fmt.Errorf("%w: /etc/passwd is not inside the workspace root /repo", pathguard.ErrOutsideWorkspace)
	if got := withDependencyHint("/etc/passwd", plain); got != plain {
		t.Errorf("a path that is not a dependency keeps its error, got %v", got)
	}
}
