package internalapi

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/julianbei/jade/internal/commands"
	"github.com/julianbei/jade/internal/protocol"
)

// RunCommand invokes one command from the repo's declared registry.
//
// check() covers build, typecheck and tests, and nothing else. Every
// project-specific command — vet, lint, codegen, migrate — fell off that
// cliff straight back to raw bash, taking the pass/fail verdict, the decisive
// summary and any telemetry with it. This is the same execution path as
// check(), opened up to commands a repo declares for itself.
//
// The request carries a name, never a shell string. See RunCommandRequest.
func (s *Server) RunCommand(req protocol.RunCommandRequest) (protocol.RunCommandResponse, error) {
	registry, err := commands.Load(s.workspace.Root())
	if err != nil {
		return protocol.RunCommandResponse{}, err
	}

	// An empty name lists rather than errors. An agent that does not yet know
	// this repo's vocabulary should be able to ask with the tool it already
	// has, instead of guessing a name to trigger the teachable error or
	// falling back to cat-ing the registry file.
	if req.Name == "" {
		available := declaredList(registry)
		return protocol.RunCommandResponse{
			Status:    "listed",
			Available: available,
			Summary:   listSummary(available),
		}, nil
	}

	command, ok := registry.Lookup(req.Name)
	if !ok {
		return protocol.RunCommandResponse{}, commands.UnknownCommandError(req.Name, registry.Names())
	}

	// The job kind carries the command name so history, events and job_status
	// all say which command ran rather than an opaque "command".
	jobID := s.jobs.Start("command:" + req.Name)
	s.jobs.RunCommandWithTimeout(jobID, s.workspace.Root(), checkTimeout(req.TimeoutSeconds), shell(), "-c", command.Run)

	if !req.Wait {
		return protocol.RunCommandResponse{
			Name:    req.Name,
			Run:     command.Run,
			JobID:   jobID,
			Outcome: protocol.OutcomeRunning,
			Status:  "running",
		}, nil
	}

	output, finished := s.jobs.Wait(jobID, checkTimeout(req.TimeoutSeconds))
	if !finished {
		return protocol.RunCommandResponse{
			Name:    req.Name,
			Run:     command.Run,
			JobID:   jobID,
			Outcome: protocol.OutcomeTimedOut,
			Status:  "running",
			Summary: fmt.Sprintf("%s did not finish within %s — poll job_status %s", req.Name, checkTimeout(req.TimeoutSeconds), jobID),
		}, nil
	}

	outcome := finishedOutcome(output)
	passed := outcome == protocol.OutcomePassed
	s.workspace.RecordRun("command "+req.Name, string(outcome))
	return protocol.RunCommandResponse{
		Name:    req.Name,
		Run:     command.Run,
		JobID:   jobID,
		Outcome: outcome,
		Status:  output.Status,
		Passed:  passed,
		Summary: verdictSummary(passed, output.Summary, output.Raw),
	}, nil
}

// DeclareCommand adds, replaces or removes a command in the repo registry.
//
// Kept separate from RunCommand because it is the privileged operation: this
// is the one place a shell string is accepted, it is written to a file in the
// repo, and it happens once rather than on every invocation. That separation
// is what makes the registry reviewable instead of being a shell passthrough.
func (s *Server) DeclareCommand(req protocol.DeclareCommandRequest) (protocol.DeclareCommandResponse, error) {
	registry, err := commands.Load(s.workspace.Root())
	if err != nil {
		return protocol.DeclareCommandResponse{}, err
	}

	if req.Remove {
		removed, err := registry.Remove(req.Name)
		if err != nil {
			return protocol.DeclareCommandResponse{}, err
		}
		return protocol.DeclareCommandResponse{
			Name:      req.Name,
			Path:      commands.RelPath,
			Removed:   removed,
			Available: declaredList(registry),
		}, nil
	}

	replaced, err := registry.Declare(req.Name, commands.Command{
		Run:         req.Run,
		Description: req.Description,
		Kind:        strings.TrimSpace(strings.ToLower(req.Kind)),
	})
	if err != nil {
		return protocol.DeclareCommandResponse{}, err
	}

	// The registry file is a workspace change like any other, so it bumps the
	// revision. Leaving it out would let an edit preconditioned on a stale
	// revision sail through after the command surface had changed underneath
	// it.
	s.workspace.BumpRevision(commands.RelPath)

	declared, _ := registry.Lookup(req.Name)
	return protocol.DeclareCommandResponse{
		Name:      req.Name,
		Run:       declared.Run,
		Path:      commands.RelPath,
		Replaced:  replaced,
		Available: declaredList(registry),
	}, nil
}

// checkDeclared runs every declared command of kind, in name order, as one
// chain: the first to fail fails the check and the ones after it do not run.
// ran is false when the repository declares none of that kind.
func (s *Server) checkDeclared(req protocol.CheckRequest, kind string) (protocol.CheckResponse, bool) {
	registry, err := commands.Load(s.workspace.Root())
	if err != nil {
		return protocol.CheckResponse{Kind: kind, Outcome: protocol.OutcomeUnavailable, Status: "no command", Summary: err.Error()}, true
	}
	var names, runs []string
	for _, entry := range registry.All() {
		if entry.Kind == kind {
			names = append(names, entry.Name)
			runs = append(runs, "("+entry.Run+")")
		}
	}
	if len(names) == 0 {
		return protocol.CheckResponse{}, false
	}

	command := "declared " + strings.Join(names, ", ")
	if req.DryRun {
		return protocol.CheckResponse{Kind: kind, Status: "dry run", Command: command}, true
	}
	jobID := s.jobs.Start("check:" + kind)
	s.jobs.RunCommandWithTimeout(jobID, s.workspace.Root(), checkTimeout(req.TimeoutSeconds), shell(), "-c", strings.Join(runs, " && "))
	if !req.Wait {
		return protocol.CheckResponse{JobID: jobID, Kind: kind, Outcome: protocol.OutcomeRunning, Status: "running", Command: command}, true
	}
	output, finished := s.jobs.Wait(jobID, checkTimeout(req.TimeoutSeconds))
	if !finished {
		return protocol.CheckResponse{
			JobID: jobID, Kind: kind, Outcome: protocol.OutcomeTimedOut, Status: "running", Command: command,
			Summary: fmt.Sprintf("%s did not finish within %s and is still running — poll job_status %s", kind, checkTimeout(req.TimeoutSeconds), jobID),
		}, true
	}
	outcome := finishedOutcome(output)
	passed := outcome == protocol.OutcomePassed
	s.workspace.RecordRun("check "+kind, string(outcome))
	return protocol.CheckResponse{
		JobID: jobID, Kind: kind, Outcome: outcome, Status: output.Status, Passed: passed,
		Summary: verdictSummary(passed, output.Summary, output.Raw),
		Command: command,
	}, true
}

func declaredList(registry *commands.Registry) []protocol.DeclaredCommand {
	all := registry.All()
	out := make([]protocol.DeclaredCommand, 0, len(all))
	for _, entry := range all {
		out = append(out, protocol.DeclaredCommand{
			Name:        entry.Name,
			Run:         entry.Run,
			Description: entry.Description,
			Kind:        entry.Kind,
		})
	}
	return out
}

func listSummary(available []protocol.DeclaredCommand) string {
	if len(available) == 0 {
		return fmt.Sprintf("no commands declared — declare one to create %s", commands.RelPath)
	}
	return fmt.Sprintf("%d declared commands", len(available))
}

// shell picks the interpreter for a declared command. A shell is used rather
// than splitting the string into argv because real project commands contain
// pipes, redirects and `&&`, and a registry that could not express them would
// send the caller straight back to bash — the exact failure this closes.
//
// /bin/sh is resolved by absolute path when available so the command does not
// depend on whatever PATH the MCP server happened to inherit.
func shell() string {
	if path, err := exec.LookPath("sh"); err == nil {
		return path
	}
	return "/bin/sh"
}
