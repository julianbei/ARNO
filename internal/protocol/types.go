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
