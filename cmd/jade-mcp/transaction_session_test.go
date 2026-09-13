package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var checkpointID = regexp.MustCompile(`cp-\d+`)

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=session@test", "-c", "user.name=session"}, args...)...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestTheChangeTransactionHoldsAcrossAScriptedSession is the 0.0.7 exit gate
// (release plan, Phase 5): one session through every part of the transaction,
// asserting what docs/tool-contract.md's edit contract says at each step.
//
// The plan lists "attempted revert, revert to a checkpoint before the
// commit". Revert refuses across a commit by design, so the attempt is the
// refusal, and the successful revert is to a checkpoint taken after it.
func TestTheChangeTransactionHoldsAcrossAScriptedSession(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("the session commits from the shell, which needs git")
	}
	server, root := newTestMCPServer(t)
	for name, content := range map[string]string{
		"status.txt": "broken\n", "old.txt": "remove me\n", "a.txt": "alpha\n", "b.txt": "beta\n",
	} {
		writeWorkspaceFile(t, root, name, content)
	}
	runGit(t, root, "init", "-q")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "baseline")

	call := func(tool string, args map[string]interface{}) (string, error) {
		return callText(t, server, tool, args)
	}
	must := func(tool string, args map[string]interface{}) string {
		t.Helper()
		out, err := call(tool, args)
		if err != nil {
			t.Fatalf("%s %v: %v", tool, args, err)
		}
		return out
	}
	text := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, name))
		if os.IsNotExist(err) {
			return "<missing>"
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	// Checkpoint, then create a file and delete another.
	first := checkpointID.FindString(must("jade.checkpoint", map[string]interface{}{"note": "before the session"}))
	if first == "" {
		t.Fatal("checkpoint returned no id")
	}
	must("jade.create_file", map[string]interface{}{"path": "new.txt", "content": "created\n"})
	must("jade.delete_file", map[string]interface{}{"path": "old.txt"})

	// A multi-file apply lands in both files.
	must("jade.apply", map[string]interface{}{"edits": []interface{}{
		map[string]interface{}{"op": "replace_text", "path": "a.txt", "oldText": "alpha", "newText": "ALPHA"},
		map[string]interface{}{"op": "replace_text", "path": "b.txt", "oldText": "beta", "newText": "BETA"},
	}})
	if text("a.txt") != "ALPHA\n" || text("b.txt") != "BETA\n" {
		t.Fatalf("apply should land in both files: %q %q", text("a.txt"), text("b.txt"))
	}

	// An edit from outside Jade makes an edit based on the earlier read stale.
	read := must("jade.read_range", map[string]interface{}{"path": "a.txt"})
	digest := digestHeader.FindStringSubmatch(read)
	if digest == nil {
		t.Fatalf("no digest in:\n%s", read)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("ALPHA\noutside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := call("jade.replace_text", map[string]interface{}{"path": "a.txt", "oldText": "ALPHA", "newText": "Alpha", "expectedDigest": digest[1]}); err == nil || !strings.Contains(err.Error(), "changed since the read") {
		t.Fatalf("the stale edit should be refused, got %v", err)
	}
	if text("a.txt") != "ALPHA\noutside\n" {
		t.Fatalf("a refused edit must not write: %q", text("a.txt"))
	}

	// A failing check, the fix, a passing check.
	must("jade.declare_command", map[string]interface{}{"name": "verify", "run": "grep -q fixed status.txt", "kind": "lint"})
	if out := must("jade.check", map[string]interface{}{"kind": "lint"}); !strings.HasPrefix(firstLine(out), "FAIL") {
		t.Fatalf("the check should fail before the fix:\n%s", out)
	}
	must("jade.replace_text", map[string]interface{}{"path": "status.txt", "oldText": "broken", "newText": "fixed"})
	if out := must("jade.check", map[string]interface{}{"kind": "lint"}); !strings.HasPrefix(firstLine(out), "pass") {
		t.Fatalf("the check should pass after the fix:\n%s", out)
	}

	// A commit from the shell: revert across it is refused and changes nothing.
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-qm", "session work")
	if _, err := call("jade.revert", map[string]interface{}{"checkpointId": first}); err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("revert across a commit should be refused, got %v", err)
	}
	if text("status.txt") != "fixed\n" || text("new.txt") != "created\n" || text("old.txt") != "<missing>" {
		t.Fatal("a refused revert must leave the committed files alone")
	}

	// A checkpoint after the commit restores contents and existence.
	second := checkpointID.FindString(must("jade.checkpoint", map[string]interface{}{"note": "after the commit"}))
	must("jade.replace_text", map[string]interface{}{"path": "status.txt", "oldText": "fixed", "newText": "broken again"})
	must("jade.create_file", map[string]interface{}{"path": "extra.txt", "content": "extra\n"})
	must("jade.delete_file", map[string]interface{}{"path": "b.txt"})
	must("jade.revert", map[string]interface{}{"checkpointId": second})
	if text("status.txt") != "fixed\n" || text("extra.txt") != "<missing>" || text("b.txt") != "BETA\n" {
		t.Fatalf("revert should restore status.txt, remove extra.txt and recreate b.txt: %q %q %q", text("status.txt"), text("extra.txt"), text("b.txt"))
	}

	// The record: changes lists the validation that ran.
	if changes := must("jade.changes", map[string]interface{}{}); !strings.Contains(changes, "ran: ") || !strings.Contains(changes, "check lint") {
		t.Fatalf("changes should list the checks this session ran:\n%s", changes)
	}
}
