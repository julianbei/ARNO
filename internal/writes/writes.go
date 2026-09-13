// Package writes is the one way Jade changes a file in the workspace (release
// plan 0.0.7, "One write path"). A write is atomic — a reader or a crash never
// sees half a file — and a change to several files lands in all of them or in
// none.
package writes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Digest names a file's contents: 12 hex characters of its SHA-256. Reads
// return it, and edits accept it as a precondition that sees every change to
// the file, not only Jade's.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:6])
}

// Change is one file's new contents, or its removal.
type Change struct {
	Path   string
	Data   []byte
	Remove bool
}

// observers maps a workspace root to the function told of every write or
// removal under it, before it happens.
var observers sync.Map

// Observe registers fn to be told the path of every write or removal under
// root before it happens. The workspace uses it to record what a file held
// before the first change since a checkpoint, which no caller could
// otherwise supply. A later registration for the same root replaces it.
func Observe(root string, fn func(path string)) {
	observers.Store(filepath.Clean(root), fn)
}

func notify(path string) {
	observers.Range(func(key, value any) bool {
		root := key.(string)
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			value.(func(string))(path)
		}
		return true
	})
}

// Remove deletes path through the write path, so observers see it first.
func Remove(path string) error {
	notify(path)
	return os.Remove(path)
}

// File writes data to path atomically: a temporary file beside it, then a
// rename. An existing file keeps its permissions, and a symlink is followed,
// so the link stays a link and its target changes.
func File(path string, data []byte) error {
	notify(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".jade-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	fail := func(err error) error {
		_ = os.Remove(name)
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fail(err)
	}
	if err := temp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		return fail(err)
	}
	if err := os.Rename(name, path); err != nil {
		return fail(err)
	}
	return nil
}

// Files writes every change or none. What each path held is read first; if
// any write fails, the files already written are put back — a created file
// removed — and the error says which write failed and whether the restore
// held. A rename across four files that lands in three compiles nowhere.
func Files(changes []Change) error {
	type previous struct {
		data    []byte
		existed bool
	}
	saved := make([]previous, len(changes))
	for i, change := range changes {
		data, err := os.ReadFile(change.Path)
		switch {
		case err == nil:
			saved[i] = previous{data: data, existed: true}
		case os.IsNotExist(err):
		default:
			return fmt.Errorf("%s: %w; nothing was written", change.Path, err)
		}
	}

	for i, change := range changes {
		var err error
		if change.Remove {
			if err = Remove(change.Path); os.IsNotExist(err) {
				err = nil
			}
		} else {
			err = File(change.Path, change.Data)
		}
		if err == nil {
			continue
		}
		var failed []string
		for j := 0; j < i; j++ {
			var restoreErr error
			if saved[j].existed {
				restoreErr = File(changes[j].Path, saved[j].data)
			} else {
				restoreErr = os.Remove(changes[j].Path)
			}
			if restoreErr != nil {
				failed = append(failed, changes[j].Path)
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("writing %s failed (%v), and restoring the files already written failed for %s", change.Path, err, strings.Join(failed, ", "))
		}
		return fmt.Errorf("writing %s failed (%v); the %d files already written were restored", change.Path, err, i)
	}
	return nil
}
