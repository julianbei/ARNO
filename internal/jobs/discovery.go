package jobs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
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

var makeTargetPattern = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)\s*:`)

// npmScriptLine matches the indented script names `npm run` prints.
var npmScriptLine = regexp.MustCompile(`(?m)^ {2}([A-Za-z0-9:_-]+)$`)

// discoverCommand resolves the real command to run for kind in dir,
// detecting the project's ecosystem rather than assuming Go. It mirrors
// droneship's discovery philosophy: prefer a command whose existence is
// actually verified over one that's merely assumed. Order is Makefile
// target (verified via `make -n`) → npm script (verified via bare
// `npm run`) → cargo (manifest-verified only) → the Go default. Returns
// ok=false only when kind itself is unrecognized everywhere.
func discoverCommand(dir string, kind string) (name string, args []string, ok bool) {
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
	}

	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		if cargoArgs, found := cargoArgsFor(dir, kind); found {
			return "cargo", cargoArgs, true
		}
	}

	commandArgs, found := goCommandsByKind[kind]
	if !found {
		return "", nil, false
	}
	return "go", commandArgs, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// npmScriptFor runs bare `npm run` and parses the indented script names npm
// itself prints, then picks the first candidate for kind that actually
// exists. This is droneship's own mechanism, ported deliberately: it
// verifies what npm considers runnable rather than reading package.json's
// "scripts" object directly or assuming a conventional name works.
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
// verify individual subcommands: droneship's own cargo path doesn't
// either, and inventing rigor the ported source never had would be
// misleading about how much is actually being checked here.
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
	return args, true
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
// assume it" principle droneship applies to npm scripts.
func verifyMakeTarget(dir string, target string) bool {
	cmd := exec.Command("make", "-n", target)
	cmd.Dir = dir
	return cmd.Run() == nil
}
