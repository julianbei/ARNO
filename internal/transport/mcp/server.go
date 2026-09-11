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
	symbols, err := s.index.Outline(req.Path)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

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

	freshness := s.workspace.Freshness(req.IndexedCommit)
	return protocol.InspectResponse{
		Revision: s.workspace.Revision(),
		Outline:  outline,
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
	symbol, source, err := s.index.ReadSymbol(req.Path, req.SymbolID, req.MaxLines)
	if err != nil {
		return protocol.InspectResponse{}, err
	}

	freshness := s.workspace.Freshness(req.IndexedCommit)
	return protocol.InspectResponse{
		Revision: s.workspace.Revision(),
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
		Source: source,
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

func (s *Server) ReplaceSymbol(req protocol.ReplaceSymbolRequest) (protocol.EditResponse, error) {
	if req.SymbolID == "" {
		return protocol.EditResponse{}, fmt.Errorf("symbol id is required")
	}
	return s.edit.ReplaceSymbol(req.SymbolID, req.NewCode), nil
}

func (s *Server) ReplaceRange(req protocol.ReplaceRangeRequest) (protocol.EditResponse, error) {
	response, ok := s.edit.ReplaceRange(req.Path, req.ExpectedRevision, req.StartLine, req.EndLine, req.NewCode)
	if !ok {
		return protocol.EditResponse{}, fmt.Errorf("edit rejected: stale revision")
	}
	return response, nil
}

func (s *Server) Changes() protocol.ChangesResponse {
	return protocol.ChangesResponse{
		Revision: s.workspace.Revision(),
		Paths:    s.workspace.Changes(),
	}
}

func (s *Server) JobStatus(id string) (protocol.JobStatusResponse, error) {
	status, summary, ok := s.jobs.Status(id)
	if !ok {
		return protocol.JobStatusResponse{}, fmt.Errorf("job not found: %s", id)
	}
	return protocol.JobStatusResponse{
		ID:      id,
		Status:  status,
		Summary: summary,
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
