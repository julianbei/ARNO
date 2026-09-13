// Package writes is the one way Jade changes a file in the workspace (release
// plan 0.0.7, "One write path"). A write is atomic — a reader or a crash never
// sees half a file — and a change to several files lands in all of them or in
// none.
package writes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Change is one file's new contents.
type Change struct {
	Path string
	Data []byte
}

// File writes data to path atomically: a temporary file beside it, then a
// rename. An existing file keeps its permissions, and a symlink is followed,
// so the link stays a link and its target changes.
func File(path string, data []byte) error {
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
		err := File(change.Path, change.Data)
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
