package workspace

import (
	"bufio"
	"bytes"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/julianbei/jade/internal/events"
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
}

// Checkpoint captures workspace state for later restore.
type Checkpoint struct {
	ID       string
	Note     string
	Revision string
	Paths    []string
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

func (m *Manager) Changes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]string, 0, len(m.changed))
	for p := range m.changed {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
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

	snapshot := checkpointSnapshot{
		id:       id,
		note:     note,
		revision: m.revision,
		changed:  copyChanged,
	}
	m.ckpts[id] = snapshot

	checkpoint := Checkpoint{
		ID:       snapshot.id,
		Note:     snapshot.note,
		Revision: revisionString(snapshot.revision),
		Paths:    sortedKeys(snapshot.changed),
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

func (m *Manager) RevertCheckpoint(id string) (Checkpoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot, ok := m.ckpts[id]
	if !ok {
		return Checkpoint{}, false
	}

	m.revision = snapshot.revision
	m.changed = make(map[string]struct{}, len(snapshot.changed))
	for path := range snapshot.changed {
		m.changed[path] = struct{}{}
	}

	checkpoint := Checkpoint{
		ID:       snapshot.id,
		Note:     snapshot.note,
		Revision: revisionString(snapshot.revision),
		Paths:    sortedKeys(snapshot.changed),
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

	return checkpoint, true
}

func (m *Manager) HeadCommit() (string, error) {
	return m.git("rev-parse", "HEAD")
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

func (m *Manager) Freshness(indexedCommit string) Freshness {
	f := Freshness{IndexedCommit: indexedCommit}

	head, err := m.HeadCommit()
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
