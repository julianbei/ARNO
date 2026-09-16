package workspace

import (
	"errors"
	"fmt"
)

// ErrCheckpointNotFound reports a revert to a checkpoint ID this session never
// created. Checkpoints live in memory, so an ID from a previous session is
// also not found.
var ErrCheckpointNotFound = errors.New("checkpoint not found")

func checkpointNotFound(id string) error {
	return fmt.Errorf("%w: %s", ErrCheckpointNotFound, id)
}

// headCommit is the commit git's HEAD points at, or "" when there is no git,
// no repository, or no commit yet. Every one of those is "no commit to
// protect", which is what callers need to know.
func (m *Manager) headCommit() string {
	head, err := m.git("rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return head
}

func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// movedPastCheckpoint refuses a revert when a commit has landed since the
// checkpoint was taken.
//
// A checkpoint snapshots the files Arno had edited. Restoring that snapshot
// after a commit writes the pre-commit content back over files git now
// considers committed — silently undoing work the user deliberately recorded,
// with nothing in the response to say so. That is why agents avoided revert
// entirely once commits were being made from the shell alongside Arno's
// revisions. Refusing, and saying why, makes the rule predictable: revert
// works within the stretch of work since the last commit, and moving past a
// commit is git's job.
func movedPastCheckpoint(id string, then string, now string) error {
	if then == now {
		return nil
	}
	if then == "" {
		return fmt.Errorf("checkpoint %s was taken before the first commit, and HEAD is now %s: "+
			"reverting would overwrite committed work. Use git to undo a commit, then checkpoint again",
			id, shortCommit(now))
	}
	return fmt.Errorf("checkpoint %s was taken at commit %s, and HEAD is now %s: "+
		"reverting would overwrite work committed since. Arno does not undo commits; "+
		"use git (git revert, git reset) to move past one, then checkpoint again",
		id, shortCommit(then), shortCommit(now))
}
