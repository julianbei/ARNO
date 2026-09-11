package workspace

import "sync"

// Manager tracks in-memory scaffold state. The concrete implementation will
// persist revisions and map to git worktrees in later phases.
type Manager struct {
	mu       sync.RWMutex
	revision int
	changed  map[string]struct{}
}

func NewManager() *Manager {
	return &Manager{
		revision: 1,
		changed:  make(map[string]struct{}),
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
		m.changed[p] = struct{}{}
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
