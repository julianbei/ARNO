package protocol

// TargetType identifies what a request points to.
type TargetType string

const (
	TargetWorkspace TargetType = "workspace"
	TargetFile      TargetType = "file"
	TargetSymbol    TargetType = "symbol"
	TargetRange     TargetType = "range"
)

// Target is the generic locator used by inspect, modify, validate, and state APIs.
type Target struct {
	Type     TargetType
	Path     string
	SymbolID string
	Revision string
	Start    int
	End      int
}

// OutlineRequest asks for declaration-level structure for a file.
type OutlineRequest struct {
	Path          string
	IndexedCommit string
}

// ReadSymbolRequest asks for a bounded symbol body.
type ReadSymbolRequest struct {
	Path          string
	SymbolID      string
	SymbolName    string
	MaxLines      int
	IndexedCommit string
}

// SymbolResolutionStatus communicates how symbol selection was resolved.
type SymbolResolutionStatus string

const (
	ResolutionExact     SymbolResolutionStatus = "exact"
	ResolutionAmbiguous SymbolResolutionStatus = "ambiguous"
	ResolutionNotFound  SymbolResolutionStatus = "not_found"
)

// SymbolResolution captures selection outcome and candidates.
type SymbolResolution struct {
	Status       SymbolResolutionStatus
	Query        string
	SelectedID   string
	CandidateIDs []string
}

// ReplaceSymbolRequest replaces a symbol implementation.
type ReplaceSymbolRequest struct {
	SymbolID string
	NewCode  string
}

// ReplaceRangeRequest replaces arbitrary source lines at an expected revision.
type ReplaceRangeRequest struct {
	Path             string
	ExpectedRevision string
	StartLine        int
	EndLine          int
	NewCode          string
}

// CheckpointRequest creates a named snapshot of workspace state.
type CheckpointRequest struct {
	Note string
}

// RevertRequest restores workspace state to a checkpoint.
type RevertRequest struct {
	CheckpointID string
}

// DiagnosticLevel normalizes error severities from language tools.
type DiagnosticLevel string

const (
	DiagnosticError   DiagnosticLevel = "error"
	DiagnosticWarning DiagnosticLevel = "warning"
	DiagnosticInfo    DiagnosticLevel = "info"
)

// Diagnostic is a normalized validation message.
type Diagnostic struct {
	Level   DiagnosticLevel
	Path    string
	Line    int
	Column  int
	Message string
}

// OutlineItem is a declaration-level view used for progressive disclosure.
type OutlineItem struct {
	ID   string
	Kind string
	Name string
	Path string
	From int
	To   int
}

// OutlineSections groups outline information by semantic category.
type OutlineSections struct {
	Imports   []string
	Types     []OutlineItem
	Classes   []OutlineItem
	Functions []OutlineItem
	Methods   []OutlineItem
	Other     []OutlineItem
}

// Freshness describes whether index data may be stale relative to workspace state.
type Freshness struct {
	IndexedCommit string
	HeadCommit    string
	Drifted       bool
	ChangedPaths  []string
	DirtyPaths    []string
	Unknown       string
}

// InspectResponse returns a scoped code view plus freshness metadata.
type InspectResponse struct {
	Revision  string
	Outline   []OutlineItem
	Sections  OutlineSections
	Source    string
	Freshness Freshness
	Resolve   SymbolResolution
}

// Event is a normalized asynchronous status signal.
type Event struct {
	Type    string
	Entity  string
	Payload map[string]string
}

// EventRecord contains one asynchronous event in the event stream.
type EventRecord struct {
	Cursor int64
	Event  Event
}

// EventsResponse returns a page of events and the latest cursor.
type EventsResponse struct {
	Cursor int64
	Events []EventRecord
}

// ChangesResponse returns tracked changed paths and active workspace revision.
type ChangesResponse struct {
	Revision string
	Paths    []string
}

// CheckpointResponse describes a created or restored workspace checkpoint.
type CheckpointResponse struct {
	ID       string
	Note     string
	Revision string
	Paths    []string
}

// JobStatusResponse returns asynchronous validation job status.
type JobStatusResponse struct {
	ID      string
	Status  string
	Summary string
}

// EditResponse is the baseline shape for mutation feedback.
type EditResponse struct {
	OldRevision  string
	NewRevision  string
	Changed      []string
	AddedLines   int
	RemovedLines int
	Diagnostics  []Diagnostic
	Jobs         []string
}
