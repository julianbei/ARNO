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
	// Budget, in tokens, pages the body at whole lines; Continue is a handle
	// from a cut body.
	Budget   int
	Continue string
}

// ReadRangeRequest reads an arbitrary line range — the read-side escape
// hatch for content that can't be addressed by symbol (comments, config
// files, generated code with no clean symbol boundaries), mirroring
// ReplaceRangeRequest's write-side equivalent.
type ReadRangeRequest struct {
	Path          string
	StartLine     int
	EndLine       int
	IndexedCommit string
	// Budget, in tokens, pages the read at whole lines; Continue is a handle
	// from a cut read.
	Budget   int
	Continue string
}

// SymbolResolutionStatus communicates how symbol selection was resolved.
type SymbolResolutionStatus string

const (
	ResolutionExact     SymbolResolutionStatus = "exact"
	ResolutionAmbiguous SymbolResolutionStatus = "ambiguous"
	ResolutionNotFound  SymbolResolutionStatus = "not_found"
)

// SymbolCandidate is one possible match for an ambiguous symbol name, with
// enough detail to choose between them without another call.
//
// Bare IDs were not enough. Two methods named Put in the same file differ only
// by receiver, and `store.go::Put@48` versus `store.go::Put@72` tells a caller
// nothing about which is which — so they had to spend a turn fetching one to
// find out. Measured: that round trip is why jade lost a head-to-head against
// grep on a real repository (1.36x tokens, 3 calls against 2).
type SymbolCandidate struct {
	ID   string
	Kind string
	Path string
	Line int
	// Signature is the candidate's declaration line, trimmed. It is the field
	// that actually disambiguates: the receiver and parameters are exactly
	// what a caller is choosing between.
	Signature string
}

// SymbolResolution captures selection outcome and candidates.
type SymbolResolution struct {
	Status     SymbolResolutionStatus
	Query      string
	SelectedID string
	// CandidateIDs is retained alongside Candidates: it is the machine-usable
	// form, and a caller that already parses it should not have to change.
	CandidateIDs []string
	Candidates   []SymbolCandidate
}

// ReplaceSymbolRequest replaces a symbol implementation.
type ReplaceSymbolRequest struct {
	SymbolID         string
	NewCode          string
	ExpectedRevision string
}

// ReplaceRangeRequest replaces arbitrary source lines at an expected revision.
type ReplaceRangeRequest struct {
	Path             string
	ExpectedRevision string
	StartLine        int
	EndLine          int
	NewCode          string
}

// InsertRequest adds text without replacing anything. Anchor follows
// ReplaceTextRequest's rule — exactly one match or refuse — because inserting
// beside an arbitrary one of several matches silently places code somewhere
// the caller never looked. An empty Anchor appends to the end of the file.
type InsertRequest struct {
	Path             string
	ExpectedRevision string
	Anchor           string
	Position         string
	Text             string
}

// ReplaceTextRequest replaces an exact, unique string. Anchoring by text
// rather than by line number means a sequence of edits does not invalidate
// its own addresses — the reason this exists alongside ReplaceRangeRequest.
type ReplaceTextRequest struct {
	Path             string
	ExpectedRevision string
	OldText          string
	NewText          string
}

// CreateFileRequest creates a brand-new file — docs/scope.md §29's MVP editing
// operation, alongside DeleteFileRequest, that had no implementation at
// all until this task.
type CreateFileRequest struct {
	Path    string
	Content string
}

// ReplaceFileRequest overwrites an existing file's entire contents.
//
// Symmetric with CreateFileRequest: that one refuses when the file exists,
// this one refuses when it does not. Choosing the tool is what makes the
// destructive case explicit, so neither needs a force flag.
type ReplaceFileRequest struct {
	Path    string
	Content string
}

// EditOp is one step in an ApplyRequest. Op selects which fields matter:
// replace_text (OldText/NewText), replace_range (StartLine/EndLine/NewText),
// replace_symbol (SymbolID or SymbolName, NewText), delete_symbol (SymbolID
// or SymbolName), insert (Position, optional Anchor, NewText).
type EditOp struct {
	Op         string
	Path       string
	OldText    string
	NewText    string
	SymbolID   string
	SymbolName string
	StartLine  int
	EndLine    int
	Anchor     string
	Position   string
}

// ApplyRequest performs several edits as one unit.
//
// Deliberately a declarative pipeline and not a scripting DSL: it cannot
// loop, branch, run arbitrary commands, or read one result to choose the
// next edit. An arbitrary step-graph executor would be bash with extra
// syntax — guardrails could not inspect intent, and telemetry would degrade
// from "17 anchored edits, 2 ambiguous-anchor failures" to "someone ran a
// script". An agent that needs to decide mid-sequence makes two Apply calls
// and thinks in between, which is observable.
type ApplyRequest struct {
	Edits            []EditOp
	ExpectedRevision string
	// Format runs the language formatter over touched files. Defaults on.
	Format bool
	// Check runs one validation afterwards ("build", "typecheck", "tests"),
	// instead of one per edit. Empty skips it.
	Check string
}

// ApplyResponse reports the whole unit's outcome.
type ApplyResponse struct {
	OldRevision  string
	NewRevision  string
	Applied      int
	Changed      []string
	AddedLines   int
	RemovedLines int
	Formatted    []string
	Diagnostics  []Diagnostic
	Checks       CheckReport
	CheckStatus  string
	CheckOutcome ValidationOutcome
	CheckPassed  bool
	CheckSummary string
	Summary      string
	Snippets     []Snippet `json:",omitempty"`
}

// DeleteSymbolRequest removes one declaration. The range comes from jade's
// own parse, so the caller never has to find the closing brace itself — the
// hand-rolled brace scanning that motivated this tool.
type DeleteSymbolRequest struct {
	Path             string
	SymbolID         string
	SymbolName       string
	ExpectedRevision string
}

// DeleteFileRequest removes a file.
type DeleteFileRequest struct {
	Path string
}

// RunTestsRequest starts a scoped test run — "all" (whole repo), "file"
// (the package containing one file), "test" (one test name across all
// packages), or "changed" (packages containing any currently-changed
// file). Deliberately conservative per docs/scope.md §33's own guidance, not
// call-graph-precise affected-test prediction.
type RunTestsRequest struct {
	Scope string
	File  string
	Test  string
	// Wait blocks for the result instead of returning a job ID to poll.
	// The async model is right for long runs; making a short scoped run
	// cost two calls plus a poll is why the native test command wins.
	Wait bool
	// TimeoutSeconds bounds the wait. Ignored when Wait is false.
	TimeoutSeconds int
}

// RunTestsResponse carries the scoped run's outcome. When the caller waited
// (the default), Status/Passed/Summary hold the verdict; otherwise JobID is
// how to follow up via job_status/job_output.
type RunTestsResponse struct {
	JobID string
	// Outcome is the verdict from the closed set; Passed is true exactly when
	// it is OutcomePassed. Status is the job's own state, kept for JSON
	// readers.
	Outcome ValidationOutcome
	Status  string
	Passed  bool
	Summary string
}

// CheckRequest runs a validation command on demand — the caller-triggered
// counterpart to the jobs an edit spawns automatically. Kind is "build",
// "typecheck" or "tests"; the actual command comes from the same discovery
// path (Makefile target, npm script, cargo, then the language default).
type CheckRequest struct {
	Kind string
	// Wait blocks for the result instead of returning a job ID to poll.
	// Defaults on: an agent asking "does it build" wants the answer.
	Wait bool
	// TimeoutSeconds bounds the wait. Ignored when Wait is false.
	TimeoutSeconds int
	// DryRun names the command that would run, without running it.
	DryRun bool
	// Target is a project directory inside the workspace to discover and run
	// the command in, for a repository with several projects. Empty means the
	// workspace root.
	Target string
}

// CheckResponse carries the outcome. Outcome is running when the check was
// not waited for, and timed out when the wait ran out; JobID is how to follow
// up on either. A dry run has no outcome.
type CheckResponse struct {
	JobID   string
	Kind    string
	Outcome ValidationOutcome
	Status  string
	Passed  bool
	Summary string
	// Command is what ran, or would run for a dry run, e.g. "go vet ./...".
	// Empty when no validation command fits the workspace.
	Command string
}

// TelemetryRequest asks for the recorded tool-usage summary.
type TelemetryRequest struct {
	// Reset clears the log instead of summarizing it, so a measurement run can
	// start from a known state.
	Reset bool
	// Catalog is every tool the server offers, so tools never called can be
	// named. The transport fills it; callers do not.
	Catalog []string
}

// TelemetryToolStats aggregates one tool's recorded calls.
type TelemetryToolStats struct {
	Tool     string
	Calls    int
	Errors   int
	Bytes    int64
	AvgMS    int64
	MaxMS    int64
	Failures []TelemetryFailure
}

// TelemetryFailure is one failure class and its count.
type TelemetryFailure struct {
	Outcome string
	Count   int
}

// TelemetryResponse carries the usage summary.
//
// Fallbacks is the headline: each entry is a class of tool failure that
// plausibly ended with the caller running a shell command instead, which is
// the thing jade's whole design bet is about.
type TelemetryResponse struct {
	Tools     []TelemetryToolStats
	Fallbacks []TelemetryFailure
	// Switches, Retries and NeverCalled are the tool-confusion report: an
	// inspect tool followed by a different one on the same target, a tool
	// retried after ambiguous or not_found, and tools with no calls.
	Switches    []TelemetrySwitch
	Retries     []TelemetryRetry
	NeverCalled []string
	TotalCalls  int
	TotalErrors int
	TotalBytes  int64
	Path        string
	Cleared     bool
	Summary     string
}

// TelemetrySwitch counts one inspect tool followed by another on one target.
type TelemetrySwitch struct {
	From  string
	To    string
	Count int
}

// TelemetryRetry counts a tool called again right after a failed answer.
type TelemetryRetry struct {
	Tool  string
	After string
	Count int
}

// GrepRequest is literal or regex text search across the workspace.
//
// Distinct from SearchRequest, which ranks symbols by name similarity and
// therefore cannot answer "what reads this struct field", "where is this
// string literal", or any query with a negative filter. See Index.Grep.
type GrepRequest struct {
	Query string
	// Dependency searches that dependency's source instead of the workspace,
	// read-only; matches are reported as dep:<name>/<path>.
	Dependency string
	// Regex treats Query as a regular expression instead of a literal.
	Regex bool
	// IgnoreCase folds case on both sides.
	IgnoreCase bool
	// Glob restricts the search by path, matched against both the base name
	// and the full relative path ("*.go", "internal/code/*").
	Glob string
	// Exclude drops paths containing this substring — the `| grep -v testdata`
	// half of the command this replaces.
	Exclude string
	// Context is how many trailing lines to include per match, like `grep -A`.
	Context int
	// Limit caps returned matches. Total still reports the true count.
	Limit int
	// Budget, in tokens, pages the answer at whole matches; the rest is behind
	// a Continue handle. Zero keeps Limit's behaviour.
	Budget int
	// Continue is a handle from a cut answer: the same call's next page.
	Continue string
}

// GrepMatch is one matching line and its trailing context.
type GrepMatch struct {
	Path string
	Line int
	Text string
	// After is the trailing context lines, empty when none were requested.
	After []string
}

// GrepResponse carries the matches. Total is the true number found even when
// Limit cut the returned set: a cap must cost detail, never accuracy.
type GrepResponse struct {
	Query      string
	Matches    []GrepMatch
	Total      int
	Files      int
	Truncated  bool
	Summary    string
	Provenance Provenance
	// Continue is the handle for the next page when a budget cut the answer.
	Continue string
}

// GrepBatchResponse answers several grep patterns, in the order asked.
type GrepBatchResponse struct {
	Responses []GrepResponse
}

// RunCommandRequest invokes one command from the repo's declared registry.
//
// It carries a Name, never a shell string. That is the whole guardrail: a
// named command is enumerable for telemetry, reviewable because it was
// declared once into a file that shows up in a diff, and teachable because an
// unknown name can answer with the list of declared ones. Accepting a shell
// string here would make this bash with extra steps.
type RunCommandRequest struct {
	// Name of the declared command. Empty lists what is declared rather than
	// erroring — an agent that does not know the vocabulary should be able to
	// ask with the tool it already has.
	Name string
	// Wait blocks for the result instead of returning a job ID to poll.
	Wait bool
	// TimeoutSeconds bounds the wait. Ignored when Wait is false.
	TimeoutSeconds int
}

// DeclaredCommand is one entry of the repo command registry.
type DeclaredCommand struct {
	Name        string
	Run         string
	Description string
}

// RunCommandResponse carries a command's outcome, or — when the request named
// no command — the list of commands that are declared.
type RunCommandResponse struct {
	Name string
	// Run is the shell string that actually ran, echoed back so the caller can
	// see what a name resolved to without opening the registry file.
	Run     string
	JobID   string
	Outcome ValidationOutcome
	Status  string
	Passed  bool
	Summary string
	// Available is populated when Name was empty, and is the answer to "what
	// can I run here".
	Available []DeclaredCommand
}

// DeclareCommandRequest adds or replaces a command in the repo registry.
//
// Declaration is deliberately a separate operation from invocation: it is the
// privileged one. The shell string is written exactly once, into a file a
// human can read and a diff will show.
type DeclareCommandRequest struct {
	Name        string
	Run         string
	Description string
	// Remove deletes the named command instead of declaring it.
	Remove bool
}

// DeclareCommandResponse confirms what was written and where.
type DeclareCommandResponse struct {
	Name string
	Run  string
	// Path is the registry's location relative to the workspace root, so the
	// caller can go read or revert it.
	Path string
	// Replaced reports that an existing command was overwritten. Surfaced
	// rather than silent: clobbering a command another agent declared should
	// be visible in the response.
	Replaced bool
	// Removed reports a deletion.
	Removed   bool
	Available []DeclaredCommand
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

// ParserInfo says how a file's symbols were obtained, and — crucially —
// whether the answer can be trusted to be complete.
//
// jade ships tree-sitter grammars for Go, TypeScript, TSX and Rust. Everything
// else falls back to a line-oriented heuristic that recognises some
// declaration shapes and misses others. That fallback is fine; presenting its
// output as if it were a grammar parse is not. Found live against a Python
// repository, where outline returned the file's one class and silently omitted
// its one function — an agent reading that concludes the function does not
// exist, which is a worse outcome than being told the language is unsupported.
type ParserInfo struct {
	// Language is the detected language, or "" when the extension is unknown.
	Language string
	// Parser is "grammar" or "heuristic".
	Parser string
	// Complete is true only for a real grammar parse. When false, absence of a
	// symbol from the outline is not evidence that the symbol is absent from
	// the file.
	Complete bool
	// Note explains the limitation in the caller's terms when Complete is
	// false, and is empty otherwise.
	Note string
}

// Provenance says how sure an answer is, what produced it and whether it is
// whole, rendered by core as one line: `exact · gopls · complete`. Certainty
// and completeness are closed sets; a backend picks from them and never words
// its own confidence. See docs/tool-contract.md, "Budgets and provenance".
type Provenance struct {
	Certainty    string
	Source       string
	Completeness string
}

// Certainty values.
const (
	CertaintyExact        = "exact"
	CertaintyStructural   = "structural"
	CertaintyApproximate  = "approximate"
	CertaintyTextFallback = "text fallback"
)

// Completeness values.
const (
	CompletenessComplete        = "complete"
	CompletenessCut             = "cut"
	CompletenessMayBeIncomplete = "may be incomplete"
	CompletenessParseErrors     = "parse errors"
	CompletenessStale           = "stale"
)

// String renders the provenance line, or "" when none was recorded.
func (p Provenance) String() string {
	if p.Certainty == "" {
		return ""
	}
	line := p.Certainty
	if p.Source != "" {
		line += " · " + p.Source
	}
	if p.Completeness != "" {
		line += " · " + p.Completeness
	}
	return line
}

// ParserProvenance is the provenance of an outline obtained as parser says:
// a grammar parse is structural and complete, a text scan may miss
// declarations, and a grammar that did not parse the file points at parse
// errors.
func ParserProvenance(parser ParserInfo) Provenance {
	switch {
	case parser.Parser == "":
		return Provenance{}
	case parser.Complete:
		return Provenance{Certainty: CertaintyStructural, Source: "tree-sitter", Completeness: CompletenessComplete}
	case parser.Parser == "heuristic" && parser.Language != "" && parserNoteSaysUnparsed(parser.Note):
		return Provenance{Certainty: CertaintyTextFallback, Source: "text scan", Completeness: CompletenessParseErrors}
	default:
		return Provenance{Certainty: CertaintyTextFallback, Source: "text scan", Completeness: CompletenessMayBeIncomplete}
	}
}

// parserNoteSaysUnparsed reports ParserFor's note for a grammar that failed on
// the file, as opposed to a language with no grammar.
func parserNoteSaysUnparsed(note string) bool {
	const marker = "grammar did not parse"
	for i := 0; i+len(marker) <= len(note); i++ {
		if note[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}

// InspectResponse returns a scoped code view plus freshness metadata.
type InspectResponse struct {
	Revision  string
	Outline   []OutlineItem
	Sections  OutlineSections
	Source    string
	Freshness Freshness
	Resolve   SymbolResolution
	// Parser reports how the outline was produced. Zero-valued when the
	// response carries no outline (a plain range read, say).
	Parser ParserInfo
	// Range describes a read whose end line was past the end of the file and
	// was clamped, e.g. "lines 190-312 of 312", or a paged read's page. Empty
	// otherwise.
	Range string
	// Continue is the handle for the rest of a read its budget cut.
	Continue string
}

// RepositoryMapRequest asks JADE to rank the most relevant files and symbols
// for a query while staying under a context budget.
type RepositoryMapRequest struct {
	Query     string
	MaxTokens int
}

// RepositoryMapItem is a single file candidate in a repo-wide relevance map.
type RepositoryMapItem struct {
	Path          string
	Score         float64
	EstimatedCost int
	SymbolCount   int
}

// RepositoryMapResponse returns the included files and the omitted remainder.
type RepositoryMapResponse struct {
	Query      string
	MaxTokens  int
	UsedTokens int
	Included   []RepositoryMapItem
	Omitted    []RepositoryMapItem
	Summary    string
	Provenance Provenance
}

// WorkspaceTreeEntry is one file or directory in a plain structural listing.
type WorkspaceTreeEntry struct {
	Path  string
	IsDir bool
}

// WorkspaceTreeRequest asks for a plain directory/file structure listing —
// orientation ("what does this repo look like"), not relevance ranking.
type WorkspaceTreeRequest struct {
	MaxEntries int
	// Budget, in tokens, pages the listing at whole entries; Continue is a
	// handle from a cut listing.
	Budget   int
	Continue string
}

// WorkspaceTreeResponse returns a bounded, sorted structural listing.
type WorkspaceTreeResponse struct {
	Root      string
	Entries   []WorkspaceTreeEntry
	Truncated bool
	Summary   string
	// Page names a paged listing's entries, "entries 1-180 of 900 ·
	// continue=c4", and Continue is the handle for the rest.
	Page     string
	Continue string
}

// CapabilitiesResponse is what Jade can do in this workspace, from one call.
type CapabilitiesResponse struct {
	Languages     []LanguageCapability
	Git           bool
	Commands      []string
	Validation    []ValidationCapability
	ProjectConfig string
	// Truncated says the language counts stopped at the listing bound.
	Truncated bool
}

// LanguageCapability is one language present in the workspace.
type LanguageCapability struct {
	Language string
	Files    int
	// Structure is how outlines and find read the language, in provenance
	// words: structural · tree-sitter, or text fallback · text scan.
	Structure Provenance
	// Server is the language server that would start; MissingServer the one
	// Jade looks for when none is installed. Both empty: none is known.
	Server        string
	MissingServer string
	Formatter     string
	// ServerState tells the kinds of missing apart: "not supported" (no
	// server known), "not installed", "failed" (installed, would not start or
	// died), and the working states "not started", "running", "indexing".
	// ServerDetail is the failure or the work in progress.
	ServerState  string
	ServerDetail string
}

// ValidationCapability is a command check would run for kind.
type ValidationCapability struct {
	Kind    string
	Command string
}

// RetrievalRequest asks JADE to choose the most relevant symbols/files under a token budget.
type RetrievalRequest struct {
	Query     string
	MaxTokens int
}

// FindRequest locates declarations by name and returns their bodies in one
// call — the fused search-and-read that `grep -n "func X" -A 30` provides
// and jade previously needed two calls (outline, then read_symbol) to match.
type FindRequest struct {
	// Dependency looks in that dependency's source instead of the workspace.
	Dependency string
	Query      string
	// Kind narrows to func, type, method, class and so on. Empty matches any.
	Kind     string
	Limit    int
	MaxLines int
	// Budget, in tokens, pages the answer at whole declarations; Continue is
	// a handle from a cut answer.
	Budget   int
	Continue string
}

// FindResult is one matching declaration, with its source.
type FindResult struct {
	SymbolID  string
	Symbol    string
	Kind      string
	Path      string
	StartLine int
	EndLine   int
	Body      string
	// Truncated says the body was cut at MaxLines, so a reader knows the
	// declaration continues rather than assuming it ended there.
	Truncated bool
}

// FindResponse returns the matches. Total is the true count even when
// Results was capped by Limit.
type FindResponse struct {
	Query      string
	Results    []FindResult
	Total      int
	Summary    string
	Provenance Provenance
	// Continue is the handle for the next page when a budget cut the answer.
	Continue string
}

// SearchRequest asks JADE to rank likely symbols/files for a query.
type SearchRequest struct {
	Query string
	Mode  string
	Limit int
}

// SearchHit is one candidate returned by a hybrid search stack.
type SearchHit struct {
	Path      string
	Symbol    string
	Reason    string
	Score     float64
	Snippet   string
	Kind      string
	StartLine int
	EndLine   int
}

// SearchResponse returns exact/symbol/semantic ranked hits for a query.
type SearchResponse struct {
	Query      string
	Mode       string
	Hits       []SearchHit
	Provenance Provenance
}

// SearchNudgeRequest asks jade whether a shell search-style command (a raw
// grep/rg/ag/ack/find/fd invocation the harness already ran) warrants
// appending index hits below its own output — piggybacking a better answer
// onto the tool call an agent already chose, rather than trying to make it
// choose differently.
// jade cannot observe the tool call itself (it's an MCP server, not the
// harness); a harness integration supplies Command and the surrounding
// context after running it.
type SearchNudgeRequest struct {
	Command        string
	OutputLength   int
	FirstInSession bool
}

// SearchNudgeResponse carries the footer to append (if any). Nudged=false
// means: append nothing, the original output stands as-is.
type SearchNudgeResponse struct {
	Footer string
	Nudged bool
}

// RetrievalCandidate is a single file/symbol candidate chosen under a token budget.
type RetrievalCandidate struct {
	Path          string
	Symbol        string
	Reason        string
	Score         float64
	EstimatedCost int
	Kind          string
	StartLine     int
	EndLine       int
}

// RetrievalResponse returns budget-aware candidate paths for a task.
type RetrievalResponse struct {
	Query          string
	MaxTokens      int
	UsedTokens     int
	Candidates     []RetrievalCandidate
	Summary        string
	BudgetExceeded bool
	Provenance     Provenance
}

// RetrievalDocument captures tokenized content for one file or symbol in the retrieval index.
type RetrievalDocument struct {
	Path      string
	Symbol    string
	Kind      string
	Terms     []string
	Score     float64
	StartLine int
	EndLine   int
}

// RetrievalIndex stores the cached candidate documents used to score queries across a workspace.
type RetrievalIndex struct {
	Documents []RetrievalDocument
}

// SymbolGraphNode is a symbol in the dependency graph.
type SymbolGraphNode struct {
	ID   string
	Path string
	Name string
	Kind string
}

// Edge confidence levels for an approximate (name-matched) symbol graph.
// These describe *how the edge was resolved*, not how likely it is to be
// correct in some numeric sense — a caller can act on the distinction.
const (
	// EdgeSameFile means the callee was declared in the caller's own file.
	// The strongest approximate signal: no cross-file guessing was needed.
	EdgeSameFile = "same-file"
	// EdgeUniqueName means the callee name matched exactly one declaration
	// repository-wide. Unambiguous by name, but still a name match — it
	// cannot see shadowing, interface dispatch or method sets.
	EdgeUniqueName = "unique-name"
	// EdgeResolved means a language server resolved the edge. Nothing emits
	// this yet; it is the target state for Phase 7's per-language migration.
	EdgeResolved = "resolved"
)

// SymbolGraphEdge is a relationship between symbols, such as calls or references.
type SymbolGraphEdge struct {
	From string
	To   string
	Kind string
	// Confidence records how this edge was resolved (see Edge* constants),
	// so a caller can tell a same-file match from a repo-wide name guess
	// rather than treating every edge as equally authoritative.
	Confidence string
}

// SymbolGraph holds a lightweight call/reference graph for retrieval and impact analysis.
type SymbolGraph struct {
	Nodes []SymbolGraphNode
	Edges []SymbolGraphEdge
	// Source is "approximate" while the graph is built from tree-sitter and
	// name matching, and "lsp" once a language server produces it.
	Source string
	// Limitations spells out what this graph provably cannot see. It is
	// carried in the response rather than left to documentation so the
	// caveat travels with the data: an approximate graph must never be
	// presented as authoritative IDE-grade semantics.
	Limitations []string
}

// ReferenceLocation is one place a symbol is referenced. Line/Column are
// 1-based when they come from a language server; an approximate
// (name-matched) answer reports 0 for both rather than guessing.
type ReferenceLocation struct {
	Path   string
	Line   int
	Column int
	// Symbol names the referencing declaration. It is the approximate
	// path's substitute for a line number: that path dedupes per calling
	// symbol but cannot report where in the file the call sits, so without
	// a name two distinct callers in one file render identically. Empty for
	// language-server results, where Line and Column already distinguish
	// them.
	Symbol string
	// Confidence is empty for language-server results (the Source field
	// already says they are resolved) and carries the graph edge's
	// confidence for approximate ones, so a caller can tell a same-file
	// match from a repository-wide name guess per reference rather than
	// only per response.
	Confidence string
}

// CommitInfo is one commit that touched a symbol's lines.
type CommitInfo struct {
	SHA     string
	Author  string
	Date    string
	Subject string
}

// HistoryRequest asks which commits touched a symbol — docs/scope.md §18's
// history(symbol) instead of `git log -p` over a whole file.
type HistoryRequest struct {
	Path       string
	SymbolID   string
	SymbolName string
	Limit      int
	// IncludePatch adds the diff hunks. Off by default: the commit list
	// answers "why does this exist", and the patch is the follow-up.
	IncludePatch bool
	// Budget, in tokens, pages the patch at whole lines; Continue is a handle
	// from a cut patch.
	Budget   int
	Continue string
}

// HistoryResponse lists the commits that touched one symbol's line range.
type HistoryResponse struct {
	Path         string
	StartLine    int
	EndLine      int
	Commits      []CommitInfo
	Patch        string
	OmittedBytes int
	Summary      string
	// Continue is the handle for the rest of a patch its budget cut.
	Continue string
}

// ContextRequest asks jade to assemble everything needed to act on a symbol.
// Purpose selects which sections are worth their tokens: "modify" (default,
// widest), "understand", "debug", "test".
type ContextRequest struct {
	Path       string
	SymbolID   string
	SymbolName string
	Purpose    string
}

// ContextResponse is docs/scope.md §14's assembled working set for one symbol —
// implementation, related types, callers, tests, diagnostics and recent
// change in a single call instead of four round trips.
//
// The *Count fields report true totals; the accompanying slices are capped.
// A cap costs detail, never accuracy.
type ContextResponse struct {
	SymbolID       string
	Symbol         string
	Kind           string
	Path           string
	Purpose        string
	Implementation string
	Callers        []string
	CallerCount    int
	Tests          []string
	TestCount      int
	Types          []string
	TypeCount      int
	Diagnostics    []Diagnostic
	// RecentChange is "added", "modified" or "removed" when this symbol
	// differs from HEAD, empty when it is unchanged.
	RecentChange string
	Summary      string
	// Provenance is how the callers and tests were found: a language server's
	// exact references or the approximate name-matched graph.
	Provenance Provenance
}

// ReferencesRequest asks where a symbol is referenced.
type ReferencesRequest struct {
	Path       string
	SymbolID   string
	SymbolName string
	// Budget, in tokens, pages the answer; Continue is a handle from a cut
	// answer.
	Budget   int
	Continue string
}

// ReferencesResponse carries the reference set plus, critically, which
// engine produced it: "lsp" (compiler-resolved, exact) or "approximate"
// (name-matched call graph, with documented blind spots). A caller must
// be able to tell these apart — they are not the same claim.
type ReferencesResponse struct {
	Query      string
	Source     string
	References []ReferenceLocation
	Summary    string
	Provenance Provenance
	// Continue is the handle for the next page when a budget cut the answer.
	Continue string
}

// RenameRequest renames a symbol across the whole repository.
type RenameRequest struct {
	Path             string
	SymbolID         string
	SymbolName       string
	NewName          string
	ExpectedRevision string
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
// ChangedFile is one file's added/removed line counts in a numstat-style diff.
type ChangedFile struct {
	Path    string
	Added   int
	Removed int
}

// ChangesResponse returns tracked changed paths and active workspace revision.
// SymbolChangeKind says how a symbol changed between two versions of a file.
type SymbolChangeKind string

const (
	SymbolAdded    SymbolChangeKind = "added"
	SymbolModified SymbolChangeKind = "modified"
	SymbolRemoved  SymbolChangeKind = "removed"
)

// SymbolChange names one symbol that moved — docs/scope.md §13's
// "SessionManager.refreshSession modified", the question an agent returning
// to a file actually has, as opposed to how many lines moved.
type SymbolChange struct {
	Path   string
	Symbol string
	Kind   string
	Change SymbolChangeKind
}

type ChangesResponse struct {
	Revision string
	// Files is the single changed-file list. It carries jade's own edit
	// ledger as well as git's diff, so callers never have to reconcile two
	// overlapping path lists — see Manager.mergedChangedFiles.
	Files        []ChangedFile
	TotalAdded   int
	TotalRemoved int
	// Symbols is empty when no changed file is in a language jade can parse;
	// the file-level counts stand on their own in that case rather than the
	// whole response degrading.
	Symbols []SymbolChange
	// SymbolsOmitted is the changed-file count that caused the symbol delta to
	// be skipped, or zero when it was computed. Non-zero means "this response
	// deliberately has no Symbols", which is a different statement from "this
	// tree has no symbol changes" and must never be collapsed into it.
	SymbolsOmitted int
	Summary        string
}

// CheckpointResponse describes a created or restored workspace checkpoint.
type CheckpointResponse struct {
	ID       string
	Note     string
	Revision string
	Paths    []string
	// Head is the git commit the checkpoint was taken at, or "" without git.
	Head string
}

// JobStatusResponse returns asynchronous validation job status.
type JobStatusResponse struct {
	ID      string
	Kind    string
	Status  string
	Summary string
}

// JobOutputResponse returns raw output for explicit expansion requests.
type JobOutputResponse struct {
	ID        string
	Kind      string
	Status    string
	Summary   string
	RawOutput string
	// OmittedBytes is how many bytes the size clamp dropped from the middle
	// of RawOutput, 0 when it is complete. Present so a reader can tell a
	// short job from a clamped one rather than mistaking a truncated tail
	// for the whole story.
	OmittedBytes int
	// Lines is a paged output's page, "lines 1-212 of 900", and Continue the
	// handle for the rest.
	Lines    string
	Continue string
}

// DiffRequest asks for the actual patch text. An empty Target diffs the
// whole working tree; a path diffs just that file.
type DiffRequest struct {
	Target string
	// Since diffs against a git revision other than HEAD — "HEAD~3", a branch
	// name, a commit SHA. Empty means HEAD, the working-tree diff.
	//
	// The question it answers is "what has this branch done", which HEAD
	// cannot express once any of the work is committed: a task that commits
	// midway becomes invisible to a working-tree diff even though nothing has
	// merged.
	Since string
	// Budget, in tokens, pages the patch at whole lines; Continue is a handle
	// from a cut patch.
	Budget   int
	Continue string
}

// DiffResponse carries real hunk content — docs/scope.md §22's diff(target?),
// the "what changed" companion to ChangesResponse's "how much changed".
type DiffResponse struct {
	Target string
	Patch  string
	// OmittedBytes is how much of the patch the size clamp dropped, 0 when
	// it is complete.
	OmittedBytes int
	Summary      string
	// Continue is the handle for the rest of a patch its budget cut.
	Continue string
}

// EditResponse is the baseline shape for mutation feedback.
type EditResponse struct {
	OldRevision  string
	NewRevision  string
	Changed      []string
	AddedLines   int
	RemovedLines int
	// Formatted names files the formatter actually rewrote after the edit,
	// empty when nothing moved. Reported rather than silent: an agent should
	// know its edit was adjusted, and a formatter that keeps firing on the
	// same file is a signal the agent is writing badly-shaped code.
	Formatted   []string
	Diagnostics []Diagnostic
	// Checks names what checked the edited files, so an empty Diagnostics
	// list can be read as "nothing wrong" rather than "nothing looked".
	Checks CheckReport
	Jobs   []string
	// Snippets show the edited region as the file now reads, so the caller
	// need not read it back.
	Snippets []Snippet `json:",omitempty"`
}

// Snippet is a region of a file after an edit, with a little context.
type Snippet struct {
	Path      string
	StartLine int
	EndLine   int
	Source    string
}

// CheckReport says which checkers ran over edited files and which files went
// unchecked, with the reason.
type CheckReport struct {
	// Checked lists distinct checker names, e.g. "gopls", "pyright-langserver".
	Checked []string `json:",omitempty"`
	// Unchecked has one "path: reason" entry per file of a known language
	// that nothing could check. Files with no language are not listed.
	Unchecked []string `json:",omitempty"`
}
