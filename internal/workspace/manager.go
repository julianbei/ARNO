package workspace

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/pathguard"
	"github.com/julianbei/jade/internal/protocol"
)

// Manager tracks in-memory scaffold state. The concrete implementation will
// persist revisions and map to git worktrees in later phases.
type Manager struct {
	mu       sync.RWMutex
	root     string
	bus      *events.Bus
	revision int
	changed  map[string]struct{}
	ckptSeq  int
	ckpts    map[string]checkpointSnapshot
}

type checkpointSnapshot struct {
	id       string
	note     string
	revision int
	changed  map[string]struct{}
	files    map[string][]byte
	// head is git's HEAD when the checkpoint was taken, "" without git or
	// before the first commit. Revert refuses when it has moved.
	head string
}

// Checkpoint captures workspace state for later restore.
type Checkpoint struct {
	ID       string
	Note     string
	Revision string
	Paths    []string
	// Head is the git commit the checkpoint was taken at, or "".
	Head string
}

// Freshness reports whether caller-visible workspace state drifted relative to
// an index snapshot commit.
type Freshness struct {
	IndexedCommit string
	HeadCommit    string
	Drifted       bool
	ChangedPaths  []string
	DirtyPaths    []string
	Unknown       string
}

func NewManager(root string, bus *events.Bus) *Manager {
	return &Manager{
		root:     root,
		bus:      bus,
		revision: 1,
		changed:  make(map[string]struct{}),
		ckptSeq:  0,
		ckpts:    make(map[string]checkpointSnapshot),
	}
}

// Root returns the workspace's root directory, for callers (such as the
// validation job runner) that need to execute commands against it.
func (m *Manager) Root() string {
	return m.root
}

func (m *Manager) Revision() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return revisionString(m.revision)
}

func (m *Manager) BumpRevision(changedPaths ...string) (oldRevision, newRevision string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldRevision = revisionString(m.revision)
	m.revision++
	newRevision = revisionString(m.revision)

	for _, p := range changedPaths {
		if p == "" {
			continue
		}
		m.changed[filepath.Clean(p)] = struct{}{}
	}

	if m.bus != nil {
		m.bus.Publish(events.Event{
			Type:   "REVISION_BUMPED",
			Entity: "workspace",
			Payload: map[string]string{
				"old_revision": oldRevision,
				"new_revision": newRevision,
			},
		})
	}

	return oldRevision, newRevision
}

// Changes returns every path this workspace considers touched: paths jade
// itself mediated an edit for (normalized back to real file paths, since a
// replace_symbol edit is tracked under a "path::Name@line" key), unioned
// with whatever git actually sees as dirty. Without the git union, an edit
// made outside jade (e.g. through a host's own file tools) would be
// invisible here even though the workspace genuinely changed.
func (m *Manager) Changes() []string {
	m.mu.RLock()
	tracked := make([]string, 0, len(m.changed))
	for key := range m.changed {
		tracked = append(tracked, pathFromChangedKey(key))
	}
	m.mu.RUnlock()

	dirty, err := m.DirtyPaths()
	if err != nil {
		sort.Strings(tracked)
		return dedupe(tracked)
	}

	combined := append(tracked, dirty...)
	sort.Strings(combined)
	return dedupe(combined)
}

// ChangesResponse assembles the full changes() payload — revision, tracked
// paths, and a numstat-style diff summary — in one place so transports don't
// duplicate this assembly.
func (m *Manager) ChangesResponse() protocol.ChangesResponse {
	files := m.mergedChangedFiles()
	added, removed := 0, 0
	for _, f := range files {
		added += f.Added
		removed += f.Removed
	}
	return protocol.ChangesResponse{
		Revision:     m.Revision(),
		Files:        files,
		TotalAdded:   added,
		TotalRemoved: removed,
		Summary:      fmt.Sprintf("%d files, +%d -%d", len(files), added, removed),
	}
}

// mergedChangedFiles folds jade's own edit ledger into git's diff summary.
//
// The two disagree in exactly the cases that matter: a file jade edited and
// then reverted has no git delta, and when git is unavailable DiffSummary is
// empty while jade still knows every path it wrote. Reporting "no changes"
// there would be a lie told by the one component that does have the answer,
// so a path jade touched is always listed — with zero counts when git sees
// nothing, which is itself the honest report.
//
// Folding rather than returning a second Paths list is deliberate: two
// overlapping path lists in one response serialized the same filenames twice
// and left callers to reconcile them.
func (m *Manager) mergedChangedFiles() []protocol.ChangedFile {
	files := m.DiffSummary()

	known := make(map[string]bool, len(files))
	for _, f := range files {
		known[f.Path] = true
	}

	for _, path := range m.Changes() {
		if known[path] || m.isDirectory(path) {
			continue
		}
		files = append(files, protocol.ChangedFile{Path: path})
		known[path] = true
	}

	sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })
	return files
}

// isDirectory filters directory entries out of the changed-file merge.
//
// `git status --porcelain` collapses a wholly-untracked directory to a single
// `dir/` entry rather than listing the files inside it, so jade's changed-path
// ledger legitimately contains directories. They were harmless while that
// ledger was a separate Paths list the renderer never printed; folding it into
// Files made them visible as bogus `+0 -0 internal/bench` rows. Found live over
// MCP immediately after the fold landed.
//
// A stat failure counts as not-a-directory: a path jade recorded that has since
// been deleted is still a file it changed, and dropping it would lose a real
// edit to hide a cosmetic one.
func (m *Manager) isDirectory(path string) bool {
	info, err := os.Stat(filepath.Join(m.root, path))
	return err == nil && info.IsDir()
}

func (m *Manager) Checkpoint(note string) Checkpoint {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ckptSeq++
	id := "cp-" + itoa(m.ckptSeq)

	copyChanged := make(map[string]struct{}, len(m.changed))
	for path := range m.changed {
		copyChanged[path] = struct{}{}
	}

	// Snapshot current on-disk content for every path touched so far, so a
	// later revert can restore exactly this state rather than only resetting
	// the revision counter. Paths that no longer exist (e.g. deleted since)
	// are skipped; reverting to a checkpoint predating their existence is a
	// known limitation, not a silent failure of paths that do exist.
	files := make(map[string][]byte, len(m.changed))
	for key := range m.changed {
		path := pathFromChangedKey(key)
		if _, captured := files[path]; captured {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.root, path))
		if err != nil {
			continue
		}
		files[path] = data
	}

	snapshot := checkpointSnapshot{
		id:       id,
		note:     note,
		revision: m.revision,
		changed:  copyChanged,
		files:    files,
		head:     m.headCommit(),
	}
	m.ckpts[id] = snapshot

	checkpoint := Checkpoint{
		ID:       snapshot.id,
		Note:     snapshot.note,
		Revision: revisionString(snapshot.revision),
		Paths:    sortedKeys(snapshot.changed),
		Head:     snapshot.head,
	}

	if m.bus != nil {
		m.bus.Publish(events.Event{
			Type:   "CHECKPOINT_CREATED",
			Entity: "workspace",
			Payload: map[string]string{
				"id":       checkpoint.ID,
				"revision": checkpoint.Revision,
				"note":     checkpoint.Note,
			},
		})
	}

	return checkpoint
}

func (m *Manager) RevertCheckpoint(id string) (Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot, ok := m.ckpts[id]
	if !ok {
		return Checkpoint{}, checkpointNotFound(id)
	}
	// Checked before anything is written: a refused revert leaves files and
	// the revision counter exactly as they were.
	if err := movedPastCheckpoint(id, snapshot.head, m.headCommit()); err != nil {
		return Checkpoint{}, err
	}

	m.revision = snapshot.revision
	m.changed = make(map[string]struct{}, len(snapshot.changed))
	for path := range snapshot.changed {
		m.changed[path] = struct{}{}
	}

	// Best-effort restore: a write failure for one path (e.g. permissions)
	// should not prevent restoring the others.
	for path, data := range snapshot.files {
		absolute, err := pathguard.Resolve(m.root, path)
		if err != nil {
			continue
		}
		_ = os.WriteFile(absolute, data, 0o644)
	}

	checkpoint := Checkpoint{
		ID:       snapshot.id,
		Note:     snapshot.note,
		Revision: revisionString(snapshot.revision),
		Paths:    sortedKeys(snapshot.changed),
		Head:     snapshot.head,
	}

	if m.bus != nil {
		m.bus.Publish(events.Event{
			Type:   "CHECKPOINT_RESTORED",
			Entity: "workspace",
			Payload: map[string]string{
				"id":       checkpoint.ID,
				"revision": checkpoint.Revision,
			},
		})
	}

	return checkpoint, nil
}

// ErrNoCommits and ErrNotARepository replace raw git stderr for the two ways
// resolving HEAD legitimately fails.
//
// This matters more than it looks. Freshness puts the error text into every
// inspect and outline response, so on a repository with no commits yet — a
// brand-new one, the most common thing there is — jade was emitting four lines
// of `git rev-parse`'s "ambiguous argument 'HEAD'" diagnostic at the top of
// every single response. Localized, too: on the machine this was found on it
// came back in German, which no amount of downstream string matching could
// have coped with. Found live while verifying 11.3 against a fresh repo.
var (
	ErrNoCommits      = errors.New("repository has no commits yet")
	ErrNotARepository = errors.New("not a git repository")
)

// HeadCommit resolves HEAD, or reports in one short line why it cannot.
//
// --verify --quiet makes git exit non-zero with *empty* output instead of
// printing its multi-line diagnostic, so the distinction below is drawn from
// exit status rather than from parsing locale-dependent text.
func (m *Manager) HeadCommit() (string, error) {
	head, err := m.git("rev-parse", "--verify", "--quiet", "HEAD")
	if err == nil && strings.TrimSpace(head) != "" {
		return head, nil
	}
	if m.isRepository() {
		return "", ErrNoCommits
	}
	return "", ErrNotARepository
}

// isRepository checks for a .git entry rather than shelling out again, so the
// probe cannot itself fail in a way that needs explaining. .git is a directory
// in a normal clone and a file in a worktree or submodule; both count.
func (m *Manager) isRepository() bool {
	_, err := os.Stat(filepath.Join(m.root, ".git"))
	return err == nil
}

func (m *Manager) DirtyPaths() ([]string, error) {
	stdout, err := m.gitRaw("status", "--porcelain")
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		paths = append(paths, filepath.Clean(path))
	}

	sort.Strings(paths)
	return dedupe(paths), nil
}

// DiffSummary returns a numstat-style per-file added/removed line count
// summary of the workspace's changes against HEAD. It is deliberately
// cheap and never throws (a git failure degrades to an
// empty summary rather than propagating an error), and gives per-file counts
// rather than full diff content. Tracked changes come from
// "git diff --numstat HEAD"; untracked new files (which numstat omits
// entirely) are counted separately as fully-added.
func (m *Manager) DiffSummary() []protocol.ChangedFile {
	files := make(map[string]*protocol.ChangedFile)

	if stdout, err := m.gitRaw("diff", "--numstat", "HEAD"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(stdout))
		for scanner.Scan() {
			parts := strings.SplitN(scanner.Text(), "\t", 3)
			if len(parts) != 3 {
				continue
			}
			added, _ := strconv.Atoi(parts[0])
			removed, _ := strconv.Atoi(parts[1])
			path := filepath.Clean(parts[2])
			files[path] = &protocol.ChangedFile{Path: path, Added: added, Removed: removed}
		}
	}

	if stdout, err := m.gitRaw("ls-files", "--others", "--exclude-standard"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(stdout))
		for scanner.Scan() {
			path := filepath.Clean(strings.TrimSpace(scanner.Text()))
			if path == "" || path == "." {
				continue
			}
			if _, exists := files[path]; exists {
				continue
			}
			files[path] = &protocol.ChangedFile{Path: path, Added: countFileLines(filepath.Join(m.root, path))}
		}
	}

	out := make([]protocol.ChangedFile, 0, len(files))
	for _, f := range files {
		out = append(out, *f)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Path < out[b].Path })
	return out
}

// countFileLines returns a best-effort line count for a newly added file.
// Errors (e.g. a binary or unreadable file) degrade to 0 rather than failing
// the whole summary.
func countFileLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	return strings.Count(string(data), "\n") + 1
}

func (m *Manager) Freshness(indexedCommit string) Freshness {
	f := Freshness{IndexedCommit: indexedCommit}

	head, err := m.HeadCommit()
	if errors.Is(err, ErrNoCommits) {
		// A repository with no commits is a normal state, not a broken one:
		// there is nothing to be stale against. Reporting it put "freshness
		// unknown" on every read, the same noise the drift count was.
		return f
	}
	if err != nil {
		f.Unknown = err.Error()
		return f
	}
	f.HeadCommit = head

	dirty, err := m.DirtyPaths()
	if err != nil {
		f.Unknown = err.Error()
		return f
	}
	f.DirtyPaths = dirty

	if indexedCommit == "" {
		f.Drifted = true
		f.ChangedPaths = append([]string(nil), dirty...)
		return f
	}

	changed, err := m.diffNames(indexedCommit, head)
	if err != nil {
		f.Unknown = err.Error()
		return f
	}
	f.ChangedPaths = dedupe(append(changed, dirty...))
	f.Drifted = indexedCommit != head || len(dirty) > 0
	return f
}

func (m *Manager) diffNames(base string, head string) ([]string, error) {
	stdout, err := m.gitRaw("diff", "--name-only", base+"..."+head)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path == "" {
			continue
		}
		out = append(out, filepath.Clean(path))
	}

	sort.Strings(out)
	return dedupe(out), nil
}

func (m *Manager) git(args ...string) (string, error) {
	stdout, err := m.gitRaw(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout), nil
}

func (m *Manager) gitRaw(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = m.root

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", execError{message: msg}
	}

	return out.String(), nil
}

type execError struct {
	message string
}

func (e execError) Error() string {
	return e.message
}

func dedupe(in []string) []string {
	if len(in) <= 1 {
		return in
	}

	out := make([]string, 0, len(in))
	last := ""
	for i, v := range in {
		if i == 0 || v != last {
			out = append(out, v)
			last = v
		}
	}
	return out
}

// pathFromChangedKey normalizes an entry in the changed-paths set back to a
// real file path. BumpRevision is called with a plain path for replace_range
// but with a full "path::Name@line" symbol ID for replace_symbol, so a
// changed-set key isn't always a path by itself.
func pathFromChangedKey(key string) string {
	if idx := strings.LastIndex(key, "::"); idx >= 0 {
		return key[:idx]
	}
	return key
}

func sortedKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func revisionString(n int) string {
	return "r" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}

	return string(digits[i:])
}
