package main

import (
	"github.com/julianbei/arno/internal/compat"

	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/project"
)

// maxBriefLanguages bounds the languages the opening instructions name.
const maxBriefLanguages = 5

// capabilityBrief is the capability report's short form for the opening
// instructions: the workspace's main languages, how each is read and whether
// its server is there. An agent learns a missing server before its first
// failed call; capabilities has the rest. Discovery of build and test commands
// is left out, since it can run npm and would slow every session's start.
func capabilityBrief(index *code.Index) string {
	tree, err := index.WorkspaceTree(20000)
	if err != nil {
		return ""
	}
	counts := map[string]int{}
	grammar := map[string]bool{}
	for _, entry := range tree.Entries {
		if entry.IsDir {
			continue
		}
		if language, parsed := code.LanguageOf(entry.Path); language != "" {
			counts[language]++
			grammar[language] = parsed
		}
	}
	if len(counts) == 0 {
		return ""
	}
	languages := make([]string, 0, len(counts))
	for language := range counts {
		languages = append(languages, language)
	}
	sort.Slice(languages, func(a, b int) bool {
		if counts[languages[a]] != counts[languages[b]] {
			return counts[languages[a]] > counts[languages[b]]
		}
		return languages[a] < languages[b]
	})
	if len(languages) > maxBriefLanguages {
		languages = languages[:maxBriefLanguages]
	}

	parts := make([]string, 0, len(languages))
	for _, language := range languages {
		reading := "grammar"
		if !grammar[language] {
			reading = "text scan only"
		}
		status := index.LanguageServerStatus(language)
		var server string
		switch status.State {
		case "not supported":
			server = "no server known"
		case "not installed":
			server = status.Detail + " not installed, references approximate"
		case "failed":
			server = "server failed"
		default:
			server = "server " + status.Detail
		}
		parts = append(parts, fmt.Sprintf("%s (%s, %s)", language, reading, server))
	}
	return " This workspace: " + strings.Join(parts, "; ") + ". Call capabilities for build and test commands and details."
}

// runInit writes a draft .arno/project.json for the workspace from what
// discovery finds, for a person to review and commit. It never overwrites an
// existing config: that file is the repository's statement, not ARNO's.
func runInit(args []string, stdout io.Writer) error {
	resolved, err := resolveWorkspaceRoot(args, compat.Getenv, os.Getwd)
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
// repository's .arno/project.json declares, so an agent learns the areas and
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
