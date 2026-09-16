package telemetry

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// excludeMarker is the comment written above the exclude pattern, so a person
// reading .git/info/exclude can tell where the line came from.
const excludeMarker = "# added by arno: local tool-usage log, never part of a change"

// excludeFromGit adds the telemetry log to the repository's local exclude
// file, so it never shows up as untracked.
//
// .git/info/exclude rather than .gitignore, deliberately: .gitignore is a
// tracked file, and editing it would itself be the unrequested change this
// exists to prevent. The exclude file is local to the clone and is never
// committed.
//
// Everything here is best-effort. No git, not a repository, a read-only .git
// — each leaves the log unexcluded, which is the pre-existing behaviour, and
// none of them may fail a tool call.
func excludeFromGit(root string) {
	git, err := exec.LookPath("git")
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Already ignored — by the repository's own .gitignore, a global excludes
	// file, or an earlier run — means there is nothing to add.
	check := exec.CommandContext(ctx, git, "check-ignore", "-q", RelPath)
	check.Dir = root
	if check.Run() == nil {
		return
	}

	// --git-path resolves the exclude file correctly for worktrees and
	// submodules, where .git is a file pointing elsewhere. --show-prefix is
	// the workspace's position inside the repository: a pattern anchored at
	// the repository root has to name the subdirectory when Arno is rooted
	// below it.
	query := exec.CommandContext(ctx, git, "rev-parse", "--git-path", "info/exclude", "--show-prefix")
	query.Dir = root
	output, err := query.Output()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return
	}
	excludePath := lines[0]
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(root, excludePath)
	}
	prefix := ""
	if len(lines) > 1 {
		prefix = lines[1]
	}
	pattern := "/" + filepath.ToSlash(filepath.Join(prefix, RelPath))

	existing, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return
		}
	}

	var addition bytes.Buffer
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		addition.WriteByte('\n')
	}
	addition.WriteString(excludeMarker + "\n" + pattern + "\n")

	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(addition.Bytes())
}
