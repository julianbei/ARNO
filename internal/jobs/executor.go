package jobs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// goCommandsByKind maps a job kind to the "go" subcommand arguments that
// actually validate a Go workspace for it.
var goCommandsByKind = map[string][]string{
	"typecheck": {"vet", "./..."},
	"tests":     {"test", "./..."},
	"build":     {"build", "./..."},
}

// RunCommand executes name/args in dir, capturing combined stdout+stderr, and
// marks job id completed with the result once the process exits. It runs
// asynchronously — callers should not block on it, and should poll Status or
// wait for a JOB_COMPLETED event instead.
// defaultCommandTimeout bounds how long a validation command may run before
// its whole process group is killed.
//
// It was 60 seconds while run_tests waits up to 300: a cold `cargo test`
// compiles for minutes, so every first Rust test run was killed and reported
// as timed out. How long a caller waits is the caller's timeout; this bound
// only stops a hung command, and a slow one stays pollable until it ends.
const defaultCommandTimeout = 10 * time.Minute

// RunCommand executes name/args in dir with the default timeout. See
// RunCommandWithTimeout for the full behavior.
func (r *Runner) RunCommand(id string, dir string, name string, args ...string) {
	r.RunCommandWithTimeout(id, dir, defaultCommandTimeout, name, args...)
}

// RunCommandWithTimeout executes name/args in dir, capturing combined
// stdout+stderr, and marks job id completed with the result once the
// process exits or timeout elapses. It runs asynchronously — callers should
// not block on it, and should poll Status or wait for a JOB_COMPLETED event
// instead.
//
// The command runs in its own process group (Setpgid) so that on timeout the
// whole group can be killed, not just the direct child. Killing only the
// direct child leaves any grandchildren it spawned (e.g. a shell's
// backgrounded subprocess) holding the output pipe open, which blocks
// completion until those grandchildren exit on their own — the timeout would
// otherwise not actually bound wall-clock time.
func (r *Runner) RunCommandWithTimeout(id string, dir string, timeout time.Duration, name string, args ...string) {
	go func() {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = projectEnv(dir)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output

		if err := cmd.Start(); err != nil {
			// A command that could not start has failed, whatever its message
			// happens to say.
			r.CompleteWithResult(id, err.Error(), true)
			return
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		var timedOut bool
		select {
		case err := <-done:
			result := strings.TrimSpace(output.String())
			if err != nil {
				if result != "" {
					result += "\n"
				}
				result += err.Error()
			}
			// err is non-nil exactly when the process exited non-zero, which is
			// the authoritative verdict. Previously it was only appended to the
			// output text ("exit status 3") and then never looked for, so the
			// status was captured and immediately thrown away.
			r.CompleteWithResult(id, result, err != nil)
			return
		case <-time.After(timeout):
			timedOut = true
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}

		result := strings.TrimSpace(output.String())
		if timedOut {
			if result != "" {
				result += "\n"
			}
			result += fmt.Sprintf("timed out after %s and was killed", timeout)
		}
		// A killed command did not pass, regardless of what it printed first.
		r.CompleteWithResult(id, result, timedOut)
	}()
}

// RunValidationCommand runs the discovered validation command for kind
// against the project rooted at dir. Named for what it does rather than
// for Go specifically: since 6.5 it dispatches per ecosystem (Makefile
// target, npm script, cargo, or the Go default).
func (r *Runner) RunValidationCommand(id string, dir string, kind string) {
	name, args, ok := discoverCommand(dir, kind)
	if !ok {
		r.CompleteWithOutput(id, fmt.Sprintf("no validation command configured for kind %q", kind))
		return
	}
	r.RunCommand(id, dir, name, args...)
}

// projectEnv is the environment a developer's shell has in dir: the
// repository's Python virtual environment activated and its local node
// binaries on PATH. Nil, when there is neither, inherits jade's own.
//
// Without it a Makefile `test: python -m pytest tests` — requests' — ran the
// system python, which has neither the project nor pytest installed, and
// failed for a reason that has nothing to do with the code.
func projectEnv(dir string) []string {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	env := os.Environ()
	prefix := []string{}
	for _, venv := range []string{".venv", "venv"} {
		bin := filepath.Join(absolute, venv, "bin")
		if info, err := os.Stat(bin); err == nil && info.IsDir() {
			prefix = append(prefix, bin)
			env = append(env, "VIRTUAL_ENV="+filepath.Join(absolute, venv))
			break
		}
	}
	nodeBin := filepath.Join(absolute, "node_modules", ".bin")
	if info, err := os.Stat(nodeBin); err == nil && info.IsDir() {
		prefix = append(prefix, nodeBin)
	}
	if len(prefix) == 0 {
		return nil
	}
	// exec keeps the last value of a repeated key, so this PATH wins.
	path := strings.Join(append(prefix, os.Getenv("PATH")), string(os.PathListSeparator))
	return append(env, "PATH="+path)
}
