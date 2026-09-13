package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/project"
)

// runInit writes a draft .jade/project.json for the workspace from what
// discovery finds, for a person to review and commit. It never overwrites an
// existing config: that file is the repository's statement, not Jade's.
func runInit(args []string, stdout io.Writer) error {
	resolved, err := resolveWorkspaceRoot(args, os.Getenv, os.Getwd)
	if err != nil {
		return err
	}
	target := filepath.Join(resolved.Path, project.RelPath)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s already exists in %s; edit it, or delete it to draft a new one", project.Source, resolved.Path)
	}

	config := project.Config{Areas: []project.Area{jobs.DraftProjectArea(resolved.Path)}}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s for %s\n\n%s\n\nReview it and commit it. Empty fields fall back to discovery; add an area for each part of a polyglot repository.\n", project.Source, resolved.Path, data)
	return nil
}

// projectInstructions is the server's opening instructions plus what the
// repository's .jade/project.json declares, so an agent learns the areas and
// the notes before its first call.
func projectInstructions(root string) string {
	config, err := project.Load(root)
	if err != nil {
		return serverInstructions + " Warning: " + err.Error() + "; checks and tests will fail until it is fixed."
	}
	if config == nil {
		return serverInstructions
	}
	text := serverInstructions + " This repository's " + project.Source + " declares " + config.Summary() + "; check and run_tests use its commands."
	if notes := strings.TrimSpace(config.Notes); notes != "" {
		text += " Notes: " + notes
	}
	return text
}

// instructionsText is what initialize sends.
func (s *mcpServer) instructionsText() string {
	if s.instructions != "" {
		return s.instructions
	}
	return serverInstructions
}
