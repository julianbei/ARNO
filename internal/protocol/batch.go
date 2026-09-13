package protocol

// FindBatchResponse answers find with several names at once: one FindResponse
// per name, in the order asked.
//
// Callers writing types against another language's structs usually need two
// or three declarations together, and sent them as parallel calls — each
// paying the per-call overhead for what is one question.
type FindBatchResponse struct {
	Responses []FindResponse
}

// ReadRangesRequest reads several ranges, possibly from several files, in one
// call.
type ReadRangesRequest struct {
	Ranges []ReadRangeRequest
}

// RangeResult is one range of a multi-range read. A range that cannot be read
// carries Error and does not fail the others: one mistyped path should not
// discard four good reads.
type RangeResult struct {
	Path       string
	StartLine  int
	EndLine    int
	TotalLines int
	// Clamped reports that the requested end line was past the end of the
	// file and was reduced to the last line.
	Clamped bool
	Source  string
	Error   string
	// Continue is a read_range handle for the rest of a range its page cut.
	Continue string
}

// ReadRangesResponse is the answer to a multi-range read.
type ReadRangesResponse struct {
	Revision string
	Results  []RangeResult
}
