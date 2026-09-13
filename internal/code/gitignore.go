package code

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/julianbei/jade/internal/project"
)

// ignoreRefresh bounds how stale the ignore list may be. One walk asks for
// it once per file, so it must not run git per file; a file created and
// ignored a moment ago is the only thing a short cache can miss.
const ignoreRefresh = 2 * time.Second

// ignoreCache holds the paths the repository's ignore rules exclude. The zero
// value is ready to use.
type ignoreCache struct {
	mu      sync.Mutex
	loaded  time.Time
	ignored map[string]bool
	// config is the workspace's .jade/project.json, for its generated paths.
	config *project.Config
}

// gitIgnored reports a path that git ignores and that is not tracked.
//
// Walks skipped a fixed list of directory names (node_modules, dist, build,
// ...) and nothing else, so ky's gitignored `distribution/` build output
// answered every grep twice — once for the source, once compiled: one search
// returned 123 matches and 19.7 KB, and the workspace tree 4.7 KB. ripgrep
// honours .gitignore by default, and agents search expecting that.
func (i *Index) gitIgnored(path string) bool {
	rel, err := filepath.Rel(i.root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	ignored := i.ignore.snapshot(i.root)
	if i.ignore.generated(filepath.ToSlash(rel)) {
		return true
	}
	if len(ignored) == 0 {
		return false
	}
	prefix := ""
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		prefix += segment
		if ignored[prefix] || ignored[prefix+"/"] {
			return true
		}
		prefix += "/"
	}
	return false
}

func (c *ignoreCache) snapshot(root string) map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ignored != nil && time.Since(c.loaded) < ignoreRefresh {
		return c.ignored
	}
	c.ignored = listIgnored(root)
	c.config, _ = project.Load(root)
	c.loaded = time.Now()
	return c.ignored
}

// generated reports a path the project config marks generated. Call after
// snapshot, which loads the config.
func (c *ignoreCache) generated(rel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config.IsGenerated(rel)
}

// listIgnored asks git for untracked ignored paths, whole directories
// collapsed to one entry. Outside a git repository, or without git, nothing is
// ignored: the fixed skip list still applies.
func listIgnored(root string) map[string]bool {
	ignored := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ignored
	}
	for _, entry := range bytes.Split(out, []byte{0}) {
		if len(entry) > 0 {
			ignored[string(entry)] = true
		}
	}
	return ignored
}
