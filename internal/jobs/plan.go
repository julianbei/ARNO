package jobs

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/project"
)

// Plan is a command decided for validation, before anything runs: what to
// run, where, and what decided it (release plan 0.0.7, "Discovery returns
// plans, core runs them"). Ecosystem discovery, .jade/project.json and
// declared commands all produce one, and Runner.RunPlan is the single place
// any of them is executed — with the runner's timeout, process group, output
// clamp and exit-status verdict.
type Plan struct {
	Kind string
	Name string
	Args []string
	Dir  string
	// Source names what decided the command: .jade/project.json, Makefile,
	// package.json, Cargo.toml, go.mod, .jade/commands.json or discovery.
	Source string
	// Timeout bounds the run; zero uses the runner's default.
	Timeout time.Duration
}

// String is the command as a person would type it.
func (p Plan) String() string {
	return strings.Join(append([]string{p.Name}, p.Args...), " ")
}

// PlanValidation decides the command for kind in dir without running it. An
// invalid project config is an error, not a reason to guess.
func PlanValidation(dir string, kind string) (Plan, bool, error) {
	if _, err := project.Load(dir); err != nil {
		return Plan{}, false, err
	}
	name, args, ok := discoverCommand(dir, kind)
	if !ok {
		return Plan{}, false, nil
	}
	return Plan{Kind: kind, Name: name, Args: args, Dir: dir, Source: planSource(dir, name, args)}, true, nil
}

// planSource names what a discovered command came from.
func planSource(dir string, name string, args []string) string {
	switch {
	case name == "sh" && len(args) > 0 && args[0] == "-c":
		return project.Source
	case name == "make":
		return "Makefile"
	case name == "npm" || strings.HasPrefix(filepath.ToSlash(name), "node_modules/"):
		return "package.json"
	case name == "cargo":
		return "Cargo.toml"
	case name == "go":
		return "go.mod"
	}
	return "discovery"
}

// RunPlan runs a plan asynchronously and completes job id with the result.
func (r *Runner) RunPlan(id string, plan Plan) {
	timeout := plan.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	r.RunCommandWithTimeout(id, plan.Dir, timeout, plan.Name, plan.Args...)
}
