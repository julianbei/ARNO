package internalapi

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/commands"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/lsp"
	"github.com/julianbei/jade/internal/project"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/toolchain"
)

// goplsInstalled reports the gopls command the Go references provider runs.
func goplsInstalled() bool {
	_, ok := toolchain.Gopls()
	return ok
}

// Capabilities reports what Jade can do in this workspace in one answer: per
// language, how its structure is read, which language server would run or is
// missing, and which formatter applies; whether git is there; the commands
// check would run; declared commands. An agent in a container with no language
// servers used to learn that from failed calls, one tool at a time (release
// plan, 0.0.6).
func (s *Server) Capabilities() (protocol.CapabilitiesResponse, error) {
	root := s.workspace.Root()
	tree, err := s.index.WorkspaceTree(maxBudgetEntries)
	if err != nil {
		return protocol.CapabilitiesResponse{}, err
	}

	type seen struct {
		files   int
		sample  string
		grammar bool
	}
	languages := map[string]*seen{}
	for _, entry := range tree.Entries {
		if entry.IsDir {
			continue
		}
		language, grammar := code.LanguageOf(entry.Path)
		if language == "" {
			continue
		}
		found := languages[language]
		if found == nil {
			found = &seen{sample: entry.Path, grammar: grammar}
			languages[language] = found
		}
		found.files++
	}

	response := protocol.CapabilitiesResponse{Truncated: tree.Truncated}
	for language, found := range languages {
		capability := protocol.LanguageCapability{Language: language, Files: found.files}
		if found.grammar {
			capability.Structure = protocol.Provenance{Certainty: protocol.CertaintyStructural, Source: "tree-sitter"}
		} else {
			capability.Structure = protocol.Provenance{Certainty: protocol.CertaintyTextFallback, Source: "text scan", Completeness: protocol.CompletenessMayBeIncomplete}
		}
		status := s.index.LanguageServerStatus(language)
		capability.ServerState, capability.ServerDetail = status.State, status.Detail
		switch status.State {
		case "running", "indexing", "not started", "failed":
			if spec, known := lsp.SpecFor(language); known {
				capability.Server = spec.Command
				if resolved, installed := spec.Resolve(); installed {
					capability.Server = resolved.Command
				}
			}
		case "not installed":
			capability.MissingServer = status.Detail
		}
		// The provider that would answer references now, as the registry
		// orders them: a working language server, the gopls command for Go,
		// else the name-matched text index.
		switch {
		case capability.Server != "" && status.State != "failed":
			capability.References = protocol.Provenance{Certainty: protocol.CertaintyExact, Source: capability.Server}
		case language == "go" && goplsInstalled():
			capability.References = protocol.Provenance{Certainty: protocol.CertaintyExact, Source: "gopls"}
		default:
			capability.References = protocol.Provenance{Certainty: protocol.CertaintyApproximate, Source: "text index", Completeness: protocol.CompletenessMayBeIncomplete}
		}
		if name, ok := edit.FormatterName(root, found.sample); ok {
			capability.Formatter = name
		}
		response.Languages = append(response.Languages, capability)
	}
	sort.Slice(response.Languages, func(a, b int) bool {
		if response.Languages[a].Files != response.Languages[b].Files {
			return response.Languages[a].Files > response.Languages[b].Files
		}
		return response.Languages[a].Language < response.Languages[b].Language
	})

	response.Providers = []protocol.ProviderCapability{
		{Capability: "references", Providers: code.ReferenceProviderIDs()},
		{Capability: "rename", Providers: code.RenameProviderIDs()},
		{Capability: "edit diagnostics", Providers: diagnostics.DiagnosticProviderIDs()},
	}

	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		response.Git = true
	}
	if registry, err := commands.Load(root); err == nil {
		for _, entry := range registry.All() {
			name := entry.Name
			if entry.Kind != "" {
				name += " (" + entry.Kind + ")"
			}
			response.Commands = append(response.Commands, name)
		}
	}
	for _, kind := range []string{"build", "typecheck", "tests"} {
		if command, ok := jobs.DescribeValidationCommand(root, kind); ok {
			response.Validation = append(response.Validation, protocol.ValidationCapability{Kind: kind, Command: command})
		}
	}
	if config, err := project.Load(root); err != nil {
		response.ProjectConfig = err.Error()
	} else if config != nil {
		response.ProjectConfig = project.Source + ": " + config.Summary()
	}
	return response, nil
}
