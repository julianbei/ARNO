package internalapi

import (
	"fmt"
	"os"
	"strings"

	"github.com/julianbei/arno/internal/protocol"
)

// Batch bounds. Enough for the two or three declarations or ranges a caller
// usually needs together, small enough that one call cannot become a whole
// repository dump.
const (
	maxFindQueries = 10
	maxReadRanges  = 20
)

// clampNote describes a read whose end line was reduced to the end of the
// file, or returns "" when it was not.
func clampNote(start, end, total int, clamped bool) string {
	if !clamped {
		return ""
	}
	return fmt.Sprintf("lines %d-%d of %d", start, end, total)
}

// FindBatch runs find for several names in one call, answering each in the
// order asked.
func (s *Server) FindBatch(queries []string, kind string, limit int, maxLines int, dependency string) (protocol.FindBatchResponse, error) {
	names := make([]string, 0, len(queries))
	for _, query := range queries {
		if query = strings.TrimSpace(query); query != "" {
			names = append(names, query)
		}
	}
	if len(names) == 0 {
		return protocol.FindBatchResponse{}, fmt.Errorf("queries has no names")
	}
	if len(names) > maxFindQueries {
		return protocol.FindBatchResponse{}, fmt.Errorf("queries has %d names; at most %d per call", len(names), maxFindQueries)
	}

	responses := make([]protocol.FindResponse, 0, len(names))
	for _, name := range names {
		response, err := s.Find(protocol.FindRequest{Query: name, Kind: kind, Limit: limit, MaxLines: maxLines, Dependency: dependency})
		if err != nil {
			return protocol.FindBatchResponse{}, fmt.Errorf("%s: %w", name, err)
		}
		responses = append(responses, response)
	}
	return protocol.FindBatchResponse{Responses: responses}, nil
}

// maxGrepQueries bounds a batched grep, like maxFindQueries.
const maxGrepQueries = 10

// GrepBatch runs grep for several patterns with the same filters, answering
// each in the order asked.
//
// A shell agent searches two or three alternatives in one `grep` command. A
// ARNO-only benchmark agent could only send one pattern per call, and made ten
// more search calls per task than the shell agent did — turns that each
// re-read the whole conversation.
func (s *Server) GrepBatch(queries []string, base protocol.GrepRequest) (protocol.GrepBatchResponse, error) {
	patterns := make([]string, 0, len(queries))
	for _, query := range queries {
		if strings.TrimSpace(query) != "" {
			patterns = append(patterns, query)
		}
	}
	if len(patterns) == 0 {
		return protocol.GrepBatchResponse{}, fmt.Errorf("queries has no patterns")
	}
	if len(patterns) > maxGrepQueries {
		return protocol.GrepBatchResponse{}, fmt.Errorf("queries has %d patterns; at most %d per call", len(patterns), maxGrepQueries)
	}

	responses := make([]protocol.GrepResponse, 0, len(patterns))
	for _, pattern := range patterns {
		req := base
		req.Query = pattern
		response, err := s.Grep(req)
		if err != nil {
			return protocol.GrepBatchResponse{}, fmt.Errorf("%s: %w", pattern, err)
		}
		responses = append(responses, response)
	}
	return protocol.GrepBatchResponse{Responses: responses}, nil
}

// ReadRanges reads several ranges in one call. A range that cannot be read
// reports its own error and leaves the others intact.
func (s *Server) ReadRanges(req protocol.ReadRangesRequest) (protocol.ReadRangesResponse, error) {
	if len(req.Ranges) == 0 {
		return protocol.ReadRangesResponse{}, fmt.Errorf("ranges is empty")
	}
	if len(req.Ranges) > maxReadRanges {
		return protocol.ReadRangesResponse{}, fmt.Errorf("ranges has %d entries; at most %d per call", len(req.Ranges), maxReadRanges)
	}

	results := make([]protocol.RangeResult, 0, len(req.Ranges))
	for _, request := range req.Ranges {
		result := protocol.RangeResult{Path: request.Path}
		if strings.TrimSpace(request.Path) == "" {
			result.Error = "path is required"
			results = append(results, result)
			continue
		}
		index, path, indexErr := s.indexFor(request.Path)
		if indexErr != nil {
			result.Error = indexErr.Error()
			results = append(results, result)
			continue
		}
		read, err := index.ReadRangePage(path, request.StartLine, request.EndLine, defaultReadBudget*4)
		switch {
		case os.IsNotExist(err):
			// The OS error carries the absolute workspace path, which is noise
			// in a response that already names the requested path.
			result.Error = "file not found"
		case err != nil:
			result.Error = withDependencyHint(request.Path, err).Error()
		default:
			result.StartLine = read.Start
			result.EndLine = read.End
			result.TotalLines = read.Total
			result.Clamped = read.ClampedEnd
			result.Source = read.Source
			// A range past its page gets a read_range handle for the rest
			// instead of losing its middle.
			if read.NextLine > 0 {
				result.Continue = s.continuations.put(continuation{tool: "read_range", revision: s.workspace.Revision(), budget: defaultReadBudget, path: request.Path, shown: read.NextLine, through: read.Through})
			}
		}
		results = append(results, result)
	}
	return protocol.ReadRangesResponse{Revision: s.workspace.Revision(), Results: results}, nil
}

// Inspect runs several read-only operations — find, grep, read_range,
// references, outline — in one call, answering each in the order asked. An
// op that fails reports its own error and leaves the others intact.
//
// The batched forms of find, grep and read_range each take one kind of
// question. An agent localising a change usually has several kinds at once:
// the declaration, its callers, and the lines beside it. A host that runs
// parallel calls spends one round trip on those; one that serialises them
// spends three. Microsoft's tool-architecture study over 11,700 trajectories
// measured composed operations at 41.6% fewer steps and 56.3% fewer tokens
// for the same success rate; the benchmark's core+inspect profile is where
// that claim is tested against ARNO's own per-tool batching.
// inspectOne answers one op of an inspect batch through the same code path
// its own tool uses, so the answer reads exactly as that tool's would.
