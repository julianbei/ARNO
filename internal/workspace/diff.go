package workspace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/julianbei/arno/internal/protocol"
	"github.com/julianbei/arno/internal/textutil"
)

// maxDiffBytes bounds a returned patch for the same reason 8.1 bounds job
// output: this repository's own working diff is already over 8,000 lines, so
// an unbounded diff() would be the single largest response ARNO can produce.
const maxDiffBytes = 8000

// Diff returns the actual patch text for the workspace or one target path —
// docs/scope.md §22's diff(target?), and the "expand change" step of §13's
// progressive disclosure: changes() says how much moved, diff() says what.
//
// An empty target diffs the whole working tree. Untracked files are included
// in both modes; `git diff HEAD` cannot see them, and a newly created file
// showing an empty patch would be a silent lie about what changed.
func (m *Manager) Diff(target string) (protocol.DiffResponse, error) {
	return m.DiffSince(target, "")
}

// DiffSince diffs against an arbitrary git revision instead of HEAD.
//
// An empty since is exactly Diff's behaviour. A non-empty one answers "what
// has this branch done", which the working-tree diff cannot express once any
// of the work is committed — a task that commits midway disappears from
// `git diff HEAD` while being precisely what the caller wanted to see.
//
// Untracked files are deliberately *not* folded in here the way Diff does for
// the working tree: against an explicit revision the caller asked what changed
// between two committed states, and silently adding files that are in neither
// would answer a different question.
func (m *Manager) DiffSince(target string, since string) (protocol.DiffResponse, error) {
	target = strings.TrimSpace(target)
	since = strings.TrimSpace(since)

	patch, err := m.diffPatch(target, since)
	if err != nil {
		return protocol.DiffResponse{}, err
	}

	clamped, omitted := textutil.Clamp(patch, maxDiffBytes,
		"request a specific path to see one file's full patch")

	return protocol.DiffResponse{
		Target:       target,
		Patch:        clamped,
		OmittedBytes: omitted,
		Summary:      diffSummaryLine(target, patch, omitted),
	}, nil
}

// DiffPatch returns the whole patch DiffSince would clamp, and its summary
// line, for a caller that pages it instead of cutting its middle.
func (m *Manager) DiffPatch(target string, since string) (string, string, error) {
	target = strings.TrimSpace(target)
	since = strings.TrimSpace(since)
	patch, err := m.diffPatch(target, since)
	if err != nil {
		return "", "", err
	}
	return patch, diffSummaryLine(target, patch, 0), nil
}

func (m *Manager) diffPatch(target string, since string) (string, error) {
	switch {
	case since != "":
		return m.revisionDiff(since, target)
	case target == "":
		return m.workspaceDiff()
	default:
		return m.targetDiff(target)
	}
}

func (m *Manager) workspaceDiff() (string, error) {
	tracked, err := m.gitRaw("diff", "HEAD")
	if err != nil {
		return "", err
	}

	sections := make([]string, 0, 4)
	if strings.TrimSpace(tracked) != "" {
		sections = append(sections, strings.TrimRight(tracked, "\n"))
	}

	untracked, err := m.untrackedPaths()
	if err != nil {
		// A failure to list untracked files should not lose the tracked
		// patch that was already produced.
		return strings.Join(sections, "\n"), nil
	}
	for _, path := range untracked {
		if patch := m.untrackedDiff(path); patch != "" {
			sections = append(sections, patch)
		}
	}

	return strings.Join(sections, "\n"), nil
}

// revisionDiff runs `git diff <since>` — including the working tree, so the
// answer covers committed and uncommitted work alike. A bad revision is
// reported with the spelling the caller used rather than as a raw git error,
// since the usual cause is a typo or a revision that does not exist in this
// repository.
func (m *Manager) revisionDiff(since string, target string) (string, error) {
	args := []string{"diff", since}
	if target != "" {
		args = append(args, "--", filepath.Clean(target))
	}

	patch, err := m.gitRaw(args...)
	if err != nil {
		return "", fmt.Errorf("cannot diff against %q: no such revision in this repository", since)
	}
	return strings.TrimRight(patch, "\n"), nil
}

func (m *Manager) targetDiff(target string) (string, error) {
	clean := filepath.Clean(target)

	tracked, err := m.gitRaw("diff", "HEAD", "--", clean)
	if err == nil && strings.TrimSpace(tracked) != "" {
		return strings.TrimRight(tracked, "\n"), nil
	}

	// Either the path is untracked (git diff HEAD cannot see it) or it is
	// genuinely unchanged. Distinguish the two rather than reporting an
	// empty patch for a brand-new file.
	if patch := m.untrackedDiff(clean); patch != "" {
		return patch, nil
	}
	if err != nil {
		return "", fmt.Errorf("diff failed for %s: %w", clean, err)
	}
	return "", nil
}

func (m *Manager) untrackedPaths() ([]string, error) {
	stdout, err := m.gitRaw("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0)
	for _, line := range strings.Split(stdout, "\n") {
		path := strings.TrimSpace(line)
		if path != "" {
			paths = append(paths, filepath.Clean(path))
		}
	}
	return paths, nil
}

// untrackedDiff produces a patch for a file git does not track yet, by
// diffing it against an empty file. `git diff --no-index` exits non-zero
// whenever the files differ — which is the normal case here — so its exit
// status is deliberately ignored and the output is what matters.
func (m *Manager) untrackedDiff(path string) string {
	cmd := exec.Command("git", "diff", "--no-index", "--", os.DevNull, path)
	cmd.Dir = m.root

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	_ = cmd.Run()

	return strings.TrimRight(out.String(), "\n")
}

func diffSummaryLine(target string, patch string, omitted int) string {
	scope := "workspace"
	if target != "" {
		scope = target
	}
	if strings.TrimSpace(patch) == "" {
		return fmt.Sprintf("no changes in %s", scope)
	}

	files := strings.Count(patch, "\ndiff --git ")
	if strings.HasPrefix(patch, "diff --git ") {
		files++
	}

	summary := fmt.Sprintf("%d changed file(s) in %s", files, scope)
	if omitted > 0 {
		summary += fmt.Sprintf(", %d bytes of patch omitted", omitted)
	}
	return summary
}

// FileAtHead returns a file's committed content at HEAD. The bool is false
// when the path is not in HEAD at all — a newly created file — which is a
// different situation from an empty file and must not be collapsed into one:
// the symbol delta treats "absent at HEAD" as "every symbol is new".
func (m *Manager) FileAtHead(path string) ([]byte, bool) {
	stdout, err := m.gitRaw("show", "HEAD:"+filepath.ToSlash(filepath.Clean(path)))
	if err != nil {
		return nil, false
	}
	return []byte(stdout), true
}
