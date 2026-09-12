package mcp

import (
	"context"
	"fmt"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/languages"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/workspace"
)

// Server is the MCP transport facade around internal services.
type Server struct {
	workspace   *workspace.Manager
	index       *code.Index
	edit        *edit.Service
	diagnostics *diagnostics.Service
	jobs        *jobs.Runner
	languages   *languages.Registry
	bus         *events.Bus
}

func NewServer(
	workspaceManager *workspace.Manager,
	index *code.Index,
	editService *edit.Service,
	diagnosticsService *diagnostics.Service,
	runner *jobs.Runner,
	registry *languages.Registry,
	bus *events.Bus,
) *Server {
	return &Server{
		workspace:   workspaceManager,
		index:       index,
		edit:        editService,
		diagnostics: diagnosticsService,
		jobs:        runner,
		languages:   registry,
		bus:         bus,
	}
}

func (s *Server) Start(context.Context) error {
	return nil
}

func (s *Server) Outline(req protocol.OutlineRequest) (protocol.InspectResponse, error) {
	sections, symbols, parser, err := s.index.OutlineStructured(req.Path)
	if err != nil {
		return protocol.InspectResponse{}, err
	}
	_ = parser

	outline := make([]protocol.OutlineItem, 0, len(symbols))
	for _, symbol := range symbols {
		outline = append(outline, protocol.OutlineItem{
			ID:   symbol.ID,
			Kind: symbol.Kind,
			Name: symbol.Name,
			Path: symbol.Path,
			From: symbol.From,
			To:   symbol.To,
		})
	}

	convert := func(in []code.Symbol) []protocol.OutlineItem {
		out := make([]protocol.OutlineItem, 0, len(in))
		for _, symbol := range in {
			out = append(out, protocol.OutlineItem{
				ID:   symbol.ID,
				Kind: symbol.Kind,
				Name: symbol.Name,
				Path: symbol.Path,
				From: symbol.From,
				To:   symbol.To,
			})
		}
		return out
	}

	freshness := s.workspace.Freshness(req.IndexedCommit)
	return protocol.InspectResponse{
		Revision: s.workspace.Revision(),
		Outline:  outline,
		Sections: protocol.OutlineSections{
			Imports:   sections.Imports,
			Types:     convert(sections.Types),
			Classes:   convert(sections.Classes),
			Functions: convert(sections.Functions),
			Methods:   convert(sections.Methods),
			Other:     convert(sections.Other),
		},
		Freshness: protocol.Freshness{
			IndexedCommit: freshness.IndexedCommit,
			HeadCommit:    freshness.HeadCommit,
			Drifted:       freshness.Drifted,
			ChangedPaths:  freshness.ChangedPaths,
			DirtyPaths:    freshness.DirtyPaths,
			Unknown:       freshness.Unknown,
		},
	}, nil
}

func (s *Server) ReadSymbol(req protocol.ReadSymbolRequest) (protocol.InspectResponse, error) {
	freshness := s.workspace.Freshness(req.IndexedCommit)
	base := protocol.InspectResponse{
		Revision: s.workspace.Revision(),
		Freshness: protocol.Freshness{
			IndexedCommit: freshness.IndexedCommit,
			HeadCommit:    freshness.HeadCommit,
			Drifted:       freshness.Drifted,
			ChangedPaths:  freshness.ChangedPaths,
			DirtyPaths:    freshness.DirtyPaths,
			Unknown:       freshness.Unknown,
		},
	}

	if req.SymbolID != "" {
		symbol, source, err := s.index.ReadSymbol(req.Path, req.SymbolID, req.MaxLines)
		if err != nil {
			base.Resolve = protocol.SymbolResolution{Status: protocol.ResolutionNotFound, Query: req.SymbolID}
			return base, nil
		}
		base.Outline = []protocol.OutlineItem{{
			ID:   symbol.ID,
			Kind: symbol.Kind,
			Name: symbol.Name,
			Path: symbol.Path,
			From: symbol.From,
			To:   symbol.To,
		}}
		base.Source = source
		base.Resolve = protocol.SymbolResolution{Status: protocol.ResolutionExact, Query: req.SymbolID, SelectedID: symbol.ID}
		return base, nil
	}

	if req.SymbolName == "" {
		return protocol.InspectResponse{}, fmt.Errorf("symbol id or symbol name is required")
	}

	candidates, err := s.index.SymbolsByName(req.Path, req.SymbolName)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

	if len(candidates) == 0 {
		base.Resolve = protocol.SymbolResolution{Status: protocol.ResolutionNotFound, Query: req.SymbolName}
		return base, nil
	}

	candidateIDs := make([]string, 0, len(candidates))
	outline := make([]protocol.OutlineItem, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.ID)
		outline = append(outline, protocol.OutlineItem{
			ID:   candidate.ID,
			Kind: candidate.Kind,
			Name: candidate.Name,
			Path: candidate.Path,
			From: candidate.From,
			To:   candidate.To,
		})
	}

	if len(candidates) > 1 {
		base.Outline = outline
		base.Resolve = protocol.SymbolResolution{
			Status:       protocol.ResolutionAmbiguous,
			Query:        req.SymbolName,
			CandidateIDs: candidateIDs,
		}
		return base, nil
	}

	symbol, source, err := s.index.ReadSymbol(req.Path, candidates[0].ID, req.MaxLines)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

	return protocol.InspectResponse{
		Outline: []protocol.OutlineItem{
			{
				ID:   symbol.ID,
				Kind: symbol.Kind,
				Name: symbol.Name,
				Path: symbol.Path,
				From: symbol.From,
				To:   symbol.To,
			},
		},
		Revision:  base.Revision,
		Source:    source,
		Freshness: base.Freshness,
		Resolve: protocol.SymbolResolution{
			Status:       protocol.ResolutionExact,
			Query:        req.SymbolName,
			SelectedID:   symbol.ID,
			CandidateIDs: candidateIDs,
		},
	}, nil
}

func (s *Server) ReplaceSymbol(req protocol.ReplaceSymbolRequest) (protocol.EditResponse, error) {
	if req.SymbolID == "" {
		return protocol.EditResponse{}, fmt.Errorf("symbol id is required")
	}
	return s.edit.ReplaceSymbol(req.SymbolID, req.ExpectedRevision, req.NewCode)
}

func (s *Server) ReplaceRange(req protocol.ReplaceRangeRequest) (protocol.EditResponse, error) {
	return s.edit.ReplaceRange(req.Path, req.ExpectedRevision, req.StartLine, req.EndLine, req.NewCode)
}

func (s *Server) Changes() protocol.ChangesResponse {
	return s.workspace.ChangesResponse()
}

func (s *Server) SearchNudge(req protocol.SearchNudgeRequest) protocol.SearchNudgeResponse {
	footer, ok := s.index.SearchNudge(req.Command, req.OutputLength, req.FirstInSession)
	return protocol.SearchNudgeResponse{Footer: footer, Nudged: ok}
}

func (s *Server) Checkpoint(req protocol.CheckpointRequest) protocol.CheckpointResponse {
	checkpoint := s.workspace.Checkpoint(req.Note)
	return protocol.CheckpointResponse{
		ID:       checkpoint.ID,
		Note:     checkpoint.Note,
		Revision: checkpoint.Revision,
		Paths:    checkpoint.Paths,
	}
}

func (s *Server) Revert(req protocol.RevertRequest) (protocol.CheckpointResponse, error) {
	if req.CheckpointID == "" {
		return protocol.CheckpointResponse{}, fmt.Errorf("checkpoint id is required")
	}

	checkpoint, ok := s.workspace.RevertCheckpoint(req.CheckpointID)
	if !ok {
		return protocol.CheckpointResponse{}, fmt.Errorf("checkpoint not found: %s", req.CheckpointID)
	}

	return protocol.CheckpointResponse{
		ID:       checkpoint.ID,
		Note:     checkpoint.Note,
		Revision: checkpoint.Revision,
		Paths:    checkpoint.Paths,
	}, nil
}

func (s *Server) JobStatus(id string) (protocol.JobStatusResponse, error) {
	kind, status, summary, ok := s.jobs.Status(id)
	if !ok {
		return protocol.JobStatusResponse{}, fmt.Errorf("job not found: %s", id)
	}
	return protocol.JobStatusResponse{
		ID:      id,
		Kind:    kind,
		Status:  status,
		Summary: summary,
	}, nil
}

func (s *Server) JobOutput(id string) (protocol.JobOutputResponse, error) {
	output, ok := s.jobs.Output(id)
	if !ok {
		return protocol.JobOutputResponse{}, fmt.Errorf("job not found: %s", id)
	}

	return protocol.JobOutputResponse{
		ID:           id,
		Kind:         output.Kind,
		Status:       output.Status,
		Summary:      output.Summary,
		RawOutput:    output.Raw,
		OmittedBytes: output.OmittedBytes,
	}, nil
}

func (s *Server) Events(after int64, limit int) protocol.EventsResponse {
	if s.bus == nil {
		return protocol.EventsResponse{}
	}

	records, latest := s.bus.Events(after, limit)
	converted := make([]protocol.EventRecord, 0, len(records))
	for _, record := range records {
		converted = append(converted, protocol.EventRecord{
			Cursor: record.Cursor,
			Event: protocol.Event{
				Type:    record.Event.Type,
				Entity:  record.Event.Entity,
				Payload: record.Event.Payload,
			},
		})
	}

	return protocol.EventsResponse{Cursor: latest, Events: converted}
}
