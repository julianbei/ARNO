package jobs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/project"
)

// manifestDiscoveryTimeout bounds the ecosystem probes (`npm run`,
// `cargo metadata`) so discovery can never block an edit indefinitely —
// the same discipline as 2.2's command timeout and 4.3's gopls timeout.
const manifestDiscoveryTimeout = 30 * time.Second

// makefileTargetCandidates lists, in preference order, the Makefile target
// names that plausibly satisfy each validation kind.
var makefileTargetCandidates = map[string][]string{
	"typecheck": {"vet", "lint"},
	"tests":     {"test", "test-unit"},
	"build":     {"build"},
}

// npmScriptCandidates lists, in preference order, the npm script names that
// plausibly satisfy each validation kind.
var npmScriptCandidates = map[string][]string{
	"typecheck": {"typecheck", "type-check", "tsc"},
	"tests":     {"test", "test:unit", "tests"},
	"build":     {"build"},
	"lint":      {"lint"},
}

// cargoArgsByKind holds the standard cargo invocation per kind. Unlike the
// npm path, these are not individually verified — see cargoArgsFor.
var cargoArgsByKind = map[string][]string{
	"typecheck": {"check", "--workspace", "--all-targets"},
	"tests":     {"test", "--workspace"},
	"build":     {"build", "--workspace"},
}

// ecosystemCommands maps a manifest file to the command that validates a
// project built around it. Order within each list is preference order.
//
// These are manifest-verified only, like the cargo path: the manifest proves
// the ecosystem, and the tool's own subcommands are built in rather than
// project-declared, so there is nothing further worth probing. A project that
// wants something else declares a Makefile target or a jade command, both of
// which are consulted first.
var ecosystemCommands = []struct {
	manifest string
	name     string
	byKind   map[string][]string
}{
	{
		manifest: "pom.xml",
		name:     "mvn",
		byKind: map[string][]string{
			"build":     {"-q", "compile"},
			"typecheck": {"-q", "compile"},
			"tests":     {"-q", "test"},
		},
	},
	{
		manifest: "build.sbt",
		name:     "sbt",
		byKind: map[string][]string{
			"build":     {"compile"},
			"typecheck": {"compile"},
			"tests":     {"test"},
		},
	},
	{
		manifest: "build.gradle",
		name:     "gradle",
		byKind: map[string][]string{
			"build":     {"build", "-x", "test"},
			"typecheck": {"compileJava"},
			"tests":     {"test"},
		},
	},
	{
		manifest: "build.gradle.kts",
		name:     "gradle",
		byKind: map[string][]string{
			"build":     {"build", "-x", "test"},
			"typecheck": {"compileKotlin"},
			"tests":     {"test"},
		},
	},
	{
		manifest: "pyproject.toml",
		name:     "python3",
		byKind: map[string][]string{
			// Python has no build step to speak of, and compileall is the
			// closest honest equivalent: it reports syntax errors across the
			// tree without pretending to be a compiler.
			"build":     {"-m", "compileall", "-q", "."},
			"typecheck": {"-m", "mypy", "."},
			"tests":     {"-m", "pytest"},
		},
	},
	{
		manifest: "setup.py",
		name:     "python3",
		byKind: map[string][]string{
			"build":     {"-m", "compileall", "-q", "."},
			"typecheck": {"-m", "mypy", "."},
			"tests":     {"-m", "pytest"},
		},
	},
	{
		manifest: "Gemfile",
		name:     "bundle",
		byKind: map[string][]string{
			"tests": {"exec", "rake", "test"},
		},
	},
}

var makeTargetPattern = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)\s*:`)

// npmScriptLine matches the indented script names `npm run` prints.
var npmScriptLine = regexp.MustCompile(`(?m)^ {2}([A-Za-z0-9:_-]+)$`)

// discoverCommand resolves the real command to run for kind in dir,
// detecting the project's ecosystem rather than assuming Go. The governing
// principle is to prefer a command whose existence is actually verified over
// one that's merely assumed. Order is Makefile
// target (verified via `make -n`) → npm script (verified via bare
// `npm run`) → cargo (manifest-verified only) → the Go default. Returns
// ok=false only when kind itself is unrecognized everywhere.
func discoverCommand(dir string, kind string) (name string, args []string, ok bool) {
	// The repository's own .jade/project.json wins over every guess.
	if config, err := project.Load(dir); err == nil && config != nil {
		if command, found := config.Command(kind); found {
			return "sh", []string{"-c", command}, true
		}
	}

	// A repository-specific Makefile target wins over any ecosystem
	// default regardless of language — a project that wrapped its own
	// build/test in a target did so for a reason (4.2's rationale).
	if target, found := makefileTargetFor(dir, kind); found {
		return "make", []string{target}, true
	}

	if fileExists(filepath.Join(dir, "package.json")) {
		if script, found := npmScriptFor(dir, kind); found {
			return "npm", []string{"run", script}, true
		}
		// A TypeScript project with no typecheck script still has a compiler:
		// ky's check typecheck answered "unavailable" with tsc installed.
		tsc := filepath.Join("node_modules", ".bin", "tsc")
		if kind == "typecheck" && fileExists(filepath.Join(dir, "tsconfig.json")) && fileExists(filepath.Join(dir, tsc)) {
			return tsc, []string{"--noEmit", "-p", "tsconfig.json"}, true
		}
	}

	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		if cargoArgs, found := cargoArgsFor(dir, kind); found {
			return "cargo", cargoArgs, true
		}
	}

	for _, ecosystem := range ecosystemCommands {
		if !fileExists(filepath.Join(dir, ecosystem.manifest)) {
			continue
		}
		if ecosystemArgs, found := ecosystem.byKind[kind]; found {
			name := ecosystem.name
			if name == "python3" {
				name = pythonInterpreter(dir)
			}
			return name, ecosystemArgs, true
		}
	}

	// The Go default applies to Go projects, not to everything left over.
	// Falling through to `go build ./...` in a Python or Java repository is
	// not a degraded answer, it is a wrong one: it fails for a reason that
	// has nothing to do with the code, and sends the reader after the wrong
	// problem. Saying no command was found is the useful answer.
	if !fileExists(filepath.Join(dir, "go.mod")) {
		return "", nil, false
	}

	commandArgs, found := goCommandsByKind[kind]
	if !found {
		return "", nil, false
	}
	return "go", commandArgs, true
}

// DescribeValidationCommand names the command a check of kind would run in
// dir, without running it. ok is false when no command fits.
//
// A green result on the wrong target is worse than none: in a Go module with
// several Node packages and no Makefile, a caller could not predict what
// "check build" would execute, and so did not trust it. Discovery is
// read-only apart from `make -n`, which prints commands without running them.
func DescribeValidationCommand(dir string, kind string) (string, bool) {
	// Rendered from the same plan RunValidationCommand runs, so what check
	// says it ran and what ran cannot drift apart.
	plan, ok, err := PlanValidation(dir, kind)
	if err != nil {
		return err.Error(), true
	}
	if !ok {
		return "", false
	}
	if plan.Source == project.Source && len(plan.Args) > 1 {
		return plan.Args[1] + " · from " + project.Source, true
	}
	return plan.String(), true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// npmScriptFor runs bare `npm run` and parses the indented script names npm
// itself prints, then picks the first candidate for kind that actually
// exists. Asking npm is deliberate: it verifies what npm considers runnable
// rather than reading package.json's "scripts" object directly or assuming a
// conventional name works.
func npmScriptFor(dir string, kind string) (string, bool) {
	candidates, ok := npmScriptCandidates[kind]
	if !ok {
		return "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), manifestDiscoveryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", "run")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return "", false
	}

	scripts := make(map[string]bool)
	for _, match := range npmScriptLine.FindAllStringSubmatch(string(output), -1) {
		scripts[match[1]] = true
	}

	for _, candidate := range candidates {
		if scripts[candidate] {
			return candidate, true
		}
	}
	return "", false
}

// cargoArgsFor proves cargo itself works (via `cargo metadata`) and then
// returns the standard invocation for kind. It deliberately does NOT
// verify individual subcommands. `cargo check`, `cargo build` and
// `cargo test` are built in rather than project-declared, so proving the
// toolchain works is the whole of what can be usefully checked.
func cargoArgsFor(dir string, kind string) ([]string, bool) {
	args, ok := cargoArgsByKind[kind]
	if !ok {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), manifestDiscoveryTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cargo", "metadata", "--no-deps", "--format-version", "1")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	return cargoPackageScope(dir, args), true
}

// cargoPackageScope narrows --workspace to -p <package> when dir is a member
// crate rather than the workspace root.
//
// cargo resolves the workspace from any member directory, so check with
// target crates/ignore ran `cargo build --workspace` over all of ripgrep: the
// target changed the directory and nothing else.
func cargoPackageScope(dir string, args []string) []string {
	manifest, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if err != nil || strings.Contains(string(manifest), "[workspace]") {
		return args
	}
	name := cargoPackageName(string(manifest))
	if name == "" || !insideCargoWorkspace(dir) {
		return args
	}
	scoped := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if arg == "--workspace" {
			scoped = append(scoped, "-p", name)
			continue
		}
		scoped = append(scoped, arg)
	}
	return scoped
}

// insideCargoWorkspace reports whether a directory above dir holds a
// workspace manifest, which makes dir's crate a member of it. A standalone
// crate keeps --workspace, which for it means the same thing.
func insideCargoWorkspace(dir string) bool {
	for parent := filepath.Dir(dir); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if manifest, err := os.ReadFile(filepath.Join(parent, "Cargo.toml")); err == nil && strings.Contains(string(manifest), "[workspace]") {
			return true
		}
	}
	return false
}

// makefileTargetFor finds the first candidate target for kind that both
// appears in the Makefile and is verified runnable.
func makefileTargetFor(dir string, kind string) (string, bool) {
	candidates, ok := makefileTargetCandidates[kind]
	if !ok {
		return "", false
	}

	data, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return "", false
	}

	targets := parseMakefileTargets(string(data))
	for _, candidate := range candidates {
		if targets[candidate] && verifyMakeTarget(dir, candidate) {
			return candidate, true
		}
	}
	return "", false
}

// parseMakefileTargets extracts declared target names from a Makefile's
// content. This only proves a target is *declared* — verifyMakeTarget
// proves it's actually invocable.
func parseMakefileTargets(content string) map[string]bool {
	targets := make(map[string]bool)
	for _, match := range makeTargetPattern.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if name == ".PHONY" {
			continue
		}
		targets[name] = true
	}
	return targets
}

// verifyMakeTarget proves target is actually invocable via `make -n`
// (a dry run that prints the target's commands without executing them)
// rather than trusting a parsed name alone — the same "prove it, don't
// assume it" principle applied to npm scripts above.
func verifyMakeTarget(dir string, target string) bool {
	cmd := exec.Command("make", "-n", target)
	cmd.Dir = dir
	return cmd.Run() == nil
}
