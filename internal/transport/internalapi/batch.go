package internalapi

import (
	"fmt"
	"os"
	"strings"

	"github.com/julianbei/jade/internal/protocol"
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
func (s *Server) FindBatch(queries []string, kind string, limit int, maxLines int) (protocol.FindBatchResponse, error) {
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
		response, err := s.index.FindSymbols(name, kind, limit, maxLines)
		if err != nil {
			return protocol.FindBatchResponse{}, fmt.Errorf("%s: %w", name, err)
		}
		responses = append(responses, response)
	}
	return protocol.FindBatchResponse{Responses: responses}, nil
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
		read, err := s.index.ReadRangeInfo(request.Path, request.StartLine, request.EndLine)
		switch {
		case os.IsNotExist(err):
			// The OS error carries the absolute workspace path, which is noise
			// in a response that already names the requested path.
			result.Error = "file not found"
		case err != nil:
			result.Error = err.Error()
		default:
			result.StartLine = read.Start
			result.EndLine = read.End
			result.TotalLines = read.Total
			result.Clamped = read.ClampedEnd
			result.Source = read.Source
		}
		results = append(results, result)
	}
	return protocol.ReadRangesResponse{Revision: s.workspace.Revision(), Results: results}, nil
}
