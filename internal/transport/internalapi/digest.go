package internalapi

import (
	"fmt"
	"os"
	"strings"

	"github.com/julianbei/jade/internal/pathguard"
	"github.com/julianbei/jade/internal/writes"
)

// fileDigest is path's content digest for a read's header, or "" when the
// path is not a readable workspace file (a dependency path, say).
func (s *Server) fileDigest(path string) string {
	absolute, err := pathguard.Resolve(s.workspace.Root(), path)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return ""
	}
	return writes.Digest(data)
}

// checkDigest refuses an edit whose file changed since the read it was based
// on — changed by Jade, the user, a formatter, a build or another agent.
// Revisions count only Jade's own edits, so an expectedRevision passed after
// someone else rewrote the file; the digest is of the file itself.
func (s *Server) checkDigest(path string, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	absolute, err := pathguard.Resolve(s.workspace.Root(), path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(absolute)
	if os.IsNotExist(err) {
		return fmt.Errorf("edit rejected: %s no longer exists; it was read with digest %s", path, expected)
	}
	if err != nil {
		return err
	}
	if current := writes.Digest(data); current != expected {
		return fmt.Errorf("edit rejected: %s changed since the read that returned digest %s (now %s) — read it again before editing", path, expected, current)
	}
	return nil
}
