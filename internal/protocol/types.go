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
	Source    string
	Freshness Freshness
}

// Event is a normalized asynchronous status signal.
type Event struct {
	Type    string
	Entity  string
	Payload map[string]string
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
