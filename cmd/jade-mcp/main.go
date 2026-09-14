package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/languages"
	"github.com/julianbei/jade/internal/lsp"
	"github.com/julianbei/jade/internal/protocol"
	"github.com/julianbei/jade/internal/render"
	"github.com/julianbei/jade/internal/telemetry"
	"github.com/julianbei/jade/internal/transport/internalapi"
	"github.com/julianbei/jade/internal/workspace"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpTextContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`

	// outcome carries an in-band failure the response reported about itself —
	// a not_found or ambiguous symbol resolution, say. Unexported so it never
	// reaches the wire: it exists only to get from jsonResult, which sees the
	// typed response, to handleToolCall, which does the recording.
	outcome telemetry.Outcome
}

type mcpServer struct {
	api       *internalapi.Server
	telemetry *telemetry.Recorder
	// profile is the --tools choice: which tools tools/list advertises.
	profile string
	// instructions is sent on initialize: the server's own plus what the
	// repository's .jade/project.json declares. Empty sends the server's own.
	instructions string
	// notify sends a notification to the client; nil before the loop starts.
	notify func(method string, params interface{})
	// background and jobEvents announce jobs started with wait: false when
	// they complete.
	background backgroundJobs
	jobEvents  <-chan events.Event
}

// version is stamped at build time via -ldflags (see the Makefile). It is
// "dev" for a plain `go build` or `go run`, which is itself useful
// information: it says the server was not built through the release path.
var version = "dev"

// resolveVersion reports what this binary is, preferring the ldflags stamp and
// falling back to the module version the toolchain recorded.
//
// The fallback exists because `go install module@version` — the first command
// in the README — cannot pass ldflags, so a correctly installed v0.0.1 used to
// introduce itself as "dev". The version is not unknown in that case, merely
// somewhere else: the module system already wrote it into the build info.
//
// A local `go build` still says "dev", because there its Main.Version really is
// "(devel)" and claiming a release number would be the actual lie.
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return info.Main.Version
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := runInit(os.Args[2:], os.Stdout); err != nil {
			fatalf("%v", err)
		}
		return
	}
	// Answered before anything else is constructed, so `--version` works even
	// when the workspace root is wrong or missing — which is exactly when
	// someone is trying to find out what they are running.
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" || arg == "version" {
			fmt.Fprintf(os.Stdout, "jade-mcp %s\n", resolveVersion())
			return
		}
	}

	ctx := context.Background()

	resolved, err := resolveWorkspaceRoot(os.Args[1:], os.Getenv, os.Getwd)
	if err != nil {
		fatalf("%v", err)
	}

	// Startup reporting goes to stderr, never stdout: stdout carries the
	// JSON-RPC stream and a stray line there breaks the protocol framing.
	fmt.Fprintf(os.Stderr, "jade-mcp %s · workspace %s (from %s)\n", resolveVersion(), resolved.Path, resolved.Source)
	if resolved.Warning != "" {
		fmt.Fprintf(os.Stderr, "jade-mcp warning: %s\n", resolved.Warning)
	}
	root := resolved.Path
	profile, err := toolsFlag(os.Args[1:])
	if err != nil {
		fatalf("%v", err)
	}

	bus := events.NewBus()
	wm := workspace.NewManager(root, bus)
	ci := code.NewIndex(root, bus)

	// Language servers are started lazily on first use and shut down when
	// jade exits. Leaking them is not hypothetical: jdtls and metals hold
	// hundreds of megabytes each, and an agent harness may restart jade
	// often.
	servers := lsp.NewManager(root)
	defer servers.Close()
	ci.UseLanguageServers(servers)
	ds := diagnostics.NewService(root)
	// Edits in every language with a server get that server's diagnostics
	// in the response, and every response names what checked it.
	ds.UseLanguageServers(ci.LanguageServerDiagnostics)
	jr := jobs.NewRunner(bus)
	es := edit.NewService(wm, ci, ds, jr)
	lr := languages.NewRegistry()

	lr.Register("typescript", languages.NewNoopAdapter("typescript"))
	lr.Register("go", languages.NewNoopAdapter("go"))
	lr.Register("rust", languages.NewNoopAdapter("rust"))

	api := internalapi.NewServer(wm, ci, es, ds, jr, lr, bus)
	if err := api.Start(ctx); err != nil {
		fatalf("failed to start internal api: %v", err)
	}

	s := &mcpServer{api: api, telemetry: telemetry.New(root), profile: profile, instructions: projectInstructions(root) + capabilityBrief(ci), jobEvents: bus.Subscribe(256)}
	if err := s.loop(os.Stdin, os.Stdout); err != nil {
		fatalf("mcp loop failed: %v", err)
	}
}

func (s *mcpServer) loop(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()
	locked := &lockedWriter{w: writer}
	s.notify = func(method string, params interface{}) {
		_ = locked.write(rpcNotification{JSONRPC: "2.0", Method: method, Params: params})
	}
	// Started here, after notify exists, so the announcer never reads it
	// before it is set.
	if s.jobEvents != nil {
		s.announceBackgroundJobs(s.jobEvents)
	}

	for {
		payload, err := readMessage(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var req rpcRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			continue
		}

		if len(req.ID) == 0 {
			_ = s.handleNotification(req)
			continue
		}

		resp := s.handleRequest(req)
		if err := locked.write(resp); err != nil {
			return err
		}
	}
}

func (s *mcpServer) handleNotification(req rpcRequest) error {
	switch req.Method {
	case "notifications/initialized":
		return nil
	default:
		return nil
	}
}

func (s *mcpServer) handleRequest(req rpcRequest) rpcResponse {
	id := decodeID(req.ID)

	switch req.Method {
	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{
						"listChanged": true,
					},
				},
				"serverInfo": map[string]interface{}{
					"name":        "jade",
					"version":     resolveVersion(),
					"description": "Agent IDE runtime for inspect, modify, validate, and state workflows.",
				},
				"instructions": s.instructionsText(),
			},
		}
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{}}
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{"tools": listedTools(s.profile)}}
	case "tools/call":
		result, err := s.handleToolCall(req.Params)
		if err != nil {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      id,
				Result: mcpToolResult{
					Content: []mcpTextContent{{Type: "text", Text: err.Error()}},
					IsError: true,
				},
			}
		}
		return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error: &rpcError{
				Code:    -32601,
				Message: "method not found",
			},
		}
	}
}

// handleToolCall is the single point every tool call passes through, which is
// why the measurement lives here rather than in each handler: a per-handler
// approach would drift the moment a tool was added, and the tool most worth
// measuring is always the newest one.
func (s *mcpServer) handleToolCall(raw json.RawMessage) (mcpToolResult, error) {
	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
		Meta      struct {
			ProgressToken interface{} `json:"progressToken"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return mcpToolResult{}, fmt.Errorf("invalid tools/call payload: %w", err)
	}

	args := req.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}

	// Resolved before anything is measured or written: a rejected name must
	// leave no trace, and both spellings must count as one tool.
	name, err := canonicalToolName(req.Name)
	if err != nil {
		s.telemetry.RecordRejected(req.Name, telemetry.Classify(err))
		return mcpToolResult{}, err
	}
	req.Name = name

	if err := checkArguments(req.Name, args); err != nil {
		return mcpToolResult{}, err
	}

	started := time.Now()
	stopProgress := s.reportProgress(req.Meta.ProgressToken, req.Name)
	result, err := s.dispatchToolCall(req.Name, args)
	stopProgress()
	if err == nil {
		s.rememberBackgroundJobs(args, result)
	}

	// A returned error always wins: it is the stronger signal, and a handler
	// that errors never produced a typed response to read an outcome from.
	outcome := telemetry.Classify(err)
	if err == nil && result.outcome != "" {
		outcome = result.outcome
	}
	s.telemetry.RecordCall(req.Name, telemetry.TargetOf(args), time.Since(started), resultBytes(result), outcome)
	return result, err
}

// resultBytes measures what the caller actually pays for: the rendered text,
// not the wire frame around it. Response size is half the reason an agent
// prefers one tool over another, so measuring the envelope instead of the
// payload would make the number useless for the comparison it exists to serve.
func resultBytes(result mcpToolResult) int {
	total := 0
	for _, content := range result.Content {
		total += len(content.Text)
	}
	return total
}

func (s *mcpServer) dispatchToolCall(name string, args map[string]interface{}) (mcpToolResult, error) {
	switch name {
	case "jade.workspace_tree":
		maxEntries := intArg(args, "maxEntries")
		res, err := s.api.WorkspaceTree(protocol.WorkspaceTreeRequest{MaxEntries: maxEntries, Budget: intArg(args, "budget"), Continue: stringArg(args, "continue")})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.outline":
		path := stringArg(args, "path")
		res, err := s.api.Outline(protocol.OutlineRequest{Path: path})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.read_range":
		if _, present := args["ranges"]; present {
			ranges, err := rangesArg(args)
			if err != nil {
				return mcpToolResult{}, err
			}
			res, err := s.api.ReadRanges(protocol.ReadRangesRequest{Ranges: ranges})
			if err != nil {
				return mcpToolResult{}, err
			}
			return jsonResult(res)
		}
		if blank(stringArg(args, "path")) && blank(stringArg(args, "continue")) {
			return mcpToolResult{}, fmt.Errorf("read_range needs path, ranges for several reads, or continue for the rest of a read")
		}
		path := stringArg(args, "path")
		startLine := intArg(args, "startLine")
		endLine := intArg(args, "endLine")
		if lines := stringArg(args, "lines"); !blank(lines) {
			var err error
			if startLine, endLine, err = parseLines(lines); err != nil {
				return mcpToolResult{}, err
			}
		}
		res, err := s.api.ReadRange(protocol.ReadRangeRequest{
			Path:      path,
			StartLine: startLine,
			EndLine:   endLine,
			Budget:    intArg(args, "budget"),
			Continue:  stringArg(args, "continue"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.history":
		res, err := s.api.History(protocol.HistoryRequest{
			Path:         stringArg(args, "path"),
			SymbolID:     stringArg(args, "symbolId"),
			SymbolName:   stringArg(args, "symbolName"),
			Limit:        intArg(args, "limit"),
			IncludePatch: boolArg(args, "includePatch"),
			Budget:       intArg(args, "budget"),
			Continue:     stringArg(args, "continue"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.context":
		res, err := s.api.Context(protocol.ContextRequest{
			Path:       stringArg(args, "path"),
			SymbolID:   stringArg(args, "symbolId"),
			SymbolName: stringArg(args, "symbolName"),
			Purpose:    stringArg(args, "purpose"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.references":
		path := stringArg(args, "path")
		symbolID := stringArg(args, "symbolId")
		symbolName := stringArg(args, "symbolName")
		res, err := s.api.References(protocol.ReferencesRequest{
			Path:       path,
			SymbolID:   symbolID,
			SymbolName: symbolName,
			Budget:     intArg(args, "budget"),
			Continue:   stringArg(args, "continue"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.rename":
		res, err := s.api.Rename(protocol.RenameRequest{
			Path:             stringArg(args, "path"),
			SymbolID:         stringArg(args, "symbolId"),
			SymbolName:       stringArg(args, "symbolName"),
			NewName:          stringArg(args, "newName"),
			ExpectedRevision: stringArg(args, "expectedRevision"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.find":
		if queries := stringsArg(args, "queries"); len(queries) > 0 {
			res, err := s.api.FindBatch(queries, stringArg(args, "kind"), intArg(args, "limit"), intArg(args, "maxLines"), stringArg(args, "dependency"))
			if err != nil {
				return mcpToolResult{}, err
			}
			return jsonResult(res)
		}
		if blank(stringArg(args, "query")) && blank(stringArg(args, "continue")) {
			return mcpToolResult{}, fmt.Errorf("find needs query (one name), queries (several names) or continue (the next page)")
		}
		res, err := s.api.Find(protocol.FindRequest{
			Dependency: stringArg(args, "dependency"),
			Query:      stringArg(args, "query"),
			Kind:       stringArg(args, "kind"),
			Limit:      intArg(args, "limit"),
			MaxLines:   intArg(args, "maxLines"),
			Budget:     intArg(args, "budget"),
			Continue:   stringArg(args, "continue"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.retrieve":
		query := stringArg(args, "query")
		maxTokens := intArg(args, "maxTokens")
		res, err := s.api.Retrieve(protocol.RetrievalRequest{Query: query, MaxTokens: maxTokens})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.replace_symbol":
		symbolID := stringArg(args, "symbolId")
		newCode := stringArg(args, "newCode")
		expected := stringArg(args, "expectedRevision")
		res, err := s.api.ReplaceSymbol(protocol.ReplaceSymbolRequest{SymbolID: symbolID, NewCode: newCode, ExpectedRevision: expected})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.insert":
		res, err := s.api.Insert(protocol.InsertRequest{
			Path:             stringArg(args, "path"),
			ExpectedRevision: stringArg(args, "expectedRevision"),
			ExpectedDigest:   stringArg(args, "expectedDigest"),
			Anchor:           stringArg(args, "anchor"),
			Position:         stringArg(args, "position"),
			Text:             stringArg(args, "text"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.replace_text":
		res, err := s.api.ReplaceText(protocol.ReplaceTextRequest{
			Path:             stringArg(args, "path"),
			ExpectedRevision: stringArg(args, "expectedRevision"),
			ExpectedDigest:   stringArg(args, "expectedDigest"),
			OldText:          stringArg(args, "oldText"),
			NewText:          stringArg(args, "newText"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.create_file":
		path := stringArg(args, "path")
		content := stringArg(args, "content")
		res, err := s.api.CreateFile(protocol.CreateFileRequest{Path: path, Content: content})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.replace_file":
		res, err := s.api.ReplaceFile(protocol.ReplaceFileRequest{
			Path:    stringArg(args, "path"),
			Content: stringArg(args, "content"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.apply":
		edits, err := editOpsArg(args, "edits")
		if err != nil {
			return mcpToolResult{}, err
		}
		res, err := s.api.Apply(protocol.ApplyRequest{
			Edits:            edits,
			ExpectedRevision: stringArg(args, "expectedRevision"),
			Format:           boolArgDefault(args, "format", true),
			Check:            stringArg(args, "check"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.delete_symbol":
		res, err := s.api.DeleteSymbol(protocol.DeleteSymbolRequest{
			Path:             stringArg(args, "path"),
			SymbolID:         stringArg(args, "symbolId"),
			SymbolName:       stringArg(args, "symbolName"),
			ExpectedRevision: stringArg(args, "expectedRevision"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.delete_file":
		path := stringArg(args, "path")
		res, err := s.api.DeleteFile(protocol.DeleteFileRequest{Path: path})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.check":
		res, err := s.api.Check(protocol.CheckRequest{
			Kind:           stringArg(args, "kind"),
			Wait:           boolArgDefault(args, "wait", true),
			TimeoutSeconds: intArg(args, "timeoutSeconds"),
			DryRun:         boolArg(args, "dryRun"),
			Target:         stringArg(args, "target"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.telemetry":
		res, err := s.api.Telemetry(protocol.TelemetryRequest{Reset: boolArg(args, "reset"), Catalog: catalogNames()})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.grep":
		if queries := stringsArg(args, "queries"); len(queries) > 0 {
			res, err := s.api.GrepBatch(queries, protocol.GrepRequest{
				Dependency: stringArg(args, "dependency"),
				Regex:      boolArg(args, "regex"),
				IgnoreCase: boolArg(args, "ignoreCase"),
				Glob:       stringArg(args, "glob"),
				Exclude:    stringArg(args, "exclude"),
				Context:    intArg(args, "context"),
				Limit:      intArg(args, "limit"),
				Budget:     intArg(args, "budget"),
			})
			if err != nil {
				return mcpToolResult{}, err
			}
			return jsonResult(res)
		}
		if blank(stringArg(args, "query")) && blank(stringArg(args, "continue")) {
			return mcpToolResult{}, fmt.Errorf("grep needs query (one pattern), queries (several patterns) or continue (the next page)")
		}
		res, err := s.api.Grep(protocol.GrepRequest{
			Dependency: stringArg(args, "dependency"),
			Query:      stringArg(args, "query"),
			Regex:      boolArg(args, "regex"),
			IgnoreCase: boolArg(args, "ignoreCase"),
			Glob:       stringArg(args, "glob"),
			Exclude:    stringArg(args, "exclude"),
			Context:    intArg(args, "context"),
			Limit:      intArg(args, "limit"),
			Budget:     intArg(args, "budget"),
			Continue:   stringArg(args, "continue"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.run_command":
		res, err := s.api.RunCommand(protocol.RunCommandRequest{
			Name:           stringArg(args, "name"),
			Wait:           boolArgDefault(args, "wait", true),
			TimeoutSeconds: intArg(args, "timeoutSeconds"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.declare_command":
		res, err := s.api.DeclareCommand(protocol.DeclareCommandRequest{
			Name:        stringArg(args, "name"),
			Run:         stringArg(args, "run"),
			Description: stringArg(args, "description"),
			Kind:        stringArg(args, "kind"),
			Remove:      boolArg(args, "remove"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.run_tests":
		scope := stringArg(args, "scope")
		file := stringArg(args, "file")
		testName := stringArg(args, "test")
		res := s.api.RunTests(protocol.RunTestsRequest{
			Wait:           boolArgDefault(args, "wait", true),
			TimeoutSeconds: intArg(args, "timeoutSeconds"),
			Scope:          scope,
			File:           file,
			Test:           testName,
		})
		return jsonResult(res)
	case "jade.capabilities":
		res, err := s.api.Capabilities()
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.changes":
		res := s.api.Changes()
		return jsonResult(res)
	case "jade.diff":
		res, err := s.api.Diff(protocol.DiffRequest{
			Target:   stringArg(args, "target"),
			Since:    stringArg(args, "since"),
			Budget:   intArg(args, "budget"),
			Continue: stringArg(args, "continue"),
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.checkpoint":
		note := stringArg(args, "note")
		res := s.api.Checkpoint(protocol.CheckpointRequest{Note: note})
		return jsonResult(res)
	case "jade.revert":
		checkpointID := stringArg(args, "checkpointId")
		res, err := s.api.Revert(protocol.RevertRequest{CheckpointID: checkpointID})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.job_status":
		id := stringArg(args, "id")
		res, err := s.api.JobStatus(id)
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.job_output":
		id := stringArg(args, "id")
		res, err := s.api.JobOutputPage(id, intArg(args, "budget"), stringArg(args, "continue"))
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.events":
		after := int64Arg(args, "after")
		limit := intArg(args, "limit")
		res := s.api.Events(after, limit)
		return jsonResult(res)
	default:
		return mcpToolResult{}, fmt.Errorf("unknown tool: %s", name)
	}
}

// catalogNames is the catalog as names, for the telemetry report's
// never-called list.
func catalogNames() []string {
	catalog := tools()
	names := make([]string, 0, len(catalog))
	for _, tool := range catalog {
		names = append(names, tool.Name)
	}
	return names
}

// deprecatedTools are served with a notice before they are removed
// (docs/tool-contract.md). Empty since 0.0.8 removed search, search_nudge,
// repository_map, read_symbol and replace_range, deprecated in 0.0.7.
var deprecatedTools = map[string]string{}

// tools is the served catalog, a deprecated tool saying so before anything
// else in its description.
func tools() []mcpTool {
	catalog := catalogTools()
	for i := range catalog {
		if reason, ok := deprecatedTools[catalog[i].Name]; ok {
			catalog[i].Description = "Deprecated, removed before 0.1.0: " + reason + ". " + catalog[i].Description
		}
	}
	return catalog
}

func catalogTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "jade.capabilities",
			Description: "What Jade can do in this workspace, in one call: per language, whether a grammar or a text scan reads it, which language server runs or is missing, and which formatter applies; whether git is there; the build, typecheck and test commands check would run; declared commands. Call it first in an unfamiliar repository instead of learning from failed calls.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "jade.workspace_tree",
			Description: "Return a plain, bounded directory/file structure listing for orientation (not relevance-ranked).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"maxEntries": map[string]interface{}{"type": "integer", "description": "Maximum number of entries to return (default 500). Prefer budget."},
					"budget":     map[string]interface{}{"type": "integer", "description": "Size of the listing in tokens. Cut at whole entries; the rest is behind continue=<handle>."},
					"continue":   map[string]interface{}{"type": "string", "description": "Handle from a cut listing: its next page."},
				},
			},
		},
		{
			Name:        "jade.outline",
			Description: "Return the declaration outline for a file, along with freshness and structured section buckets.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "Repository-relative or workspace-relative file path."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.read_range",
			Description: "Read a file verbatim, whole or by line range — the replacement for `cat` and `sed -n`. Omit both line numbers to read the whole file, which is how to read go.mod, a Makefile, or any JSON/YAML/TOML config that has no symbols to address. An end line past the end of the file reads to the end. A dependency's source reads as dep:<name>/<path>, read-only. Several ranges, in one file or many, go in one call: {\"ranges\": [{\"path\": \"a.go\", \"lines\": \"280-400\"}, {\"path\": \"b.go\", \"lines\": \"700-760\"}]}.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":     map[string]interface{}{"type": "string", "description": "Repository-relative or workspace-relative file path."},
					"lines":    map[string]interface{}{"type": "string", "description": "Line range: \"280-400\", \"280-\" to the end, or \"280\". Omit to read the whole file."},
					"budget":   map[string]interface{}{"type": "integer", "description": "Size of the read in tokens (default 5000). Cut at whole lines; the rest is behind continue=<handle>."},
					"continue": map[string]interface{}{"type": "string", "description": "Handle from a cut read: the rest of it."},
					"ranges": map[string]interface{}{
						"type":        "array",
						"description": "Several reads in one call, instead of path. A range that fails reports its error without failing the others.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"path":  map[string]interface{}{"type": "string"},
								"lines": map[string]interface{}{"type": "string", "description": "\"280-400\", \"280-\" or \"280\"."},
							},
							"required": []string{"path"},
						},
					},
				},
				"required": []string{},
			},
		},
		{
			Name:        "jade.history",
			Description: "List the commits that touched one symbol's lines, via git log -L — 'why does this code exist' without reading a whole file's history. Commits only by default; set includePatch for the diff hunks.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":         map[string]interface{}{"type": "string", "description": "File containing the symbol's declaration."},
					"symbolId":     map[string]interface{}{"type": "string", "description": "Exact symbol ID if already known."},
					"symbolName":   map[string]interface{}{"type": "string", "description": "Symbol name to resolve when ID is unknown."},
					"limit":        map[string]interface{}{"type": "integer", "description": "Maximum commits to return (default 10)."},
					"includePatch": map[string]interface{}{"type": "boolean", "description": "Include diff hunks for each commit."},
					"budget":       map[string]interface{}{"type": "integer", "description": "Size of the patch page in tokens (default 1500). Cut at whole lines; the rest is behind continue=<handle>."},
					"continue":     map[string]interface{}{"type": "string", "description": "Handle from a cut patch: its next page."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.context",
			Description: "Assemble everything needed to act on one symbol in a single call: implementation, related types, direct callers, tests exercising it, current diagnostics, and whether it changed since HEAD. Replaces find + references + test search + diagnostics round trips.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":       map[string]interface{}{"type": "string", "description": "File containing the symbol's declaration."},
					"symbolId":   map[string]interface{}{"type": "string", "description": "Exact symbol ID if already known."},
					"symbolName": map[string]interface{}{"type": "string", "description": "Symbol name to resolve when ID is unknown."},
					"purpose":    map[string]interface{}{"type": "string", "description": "What the context is for: modify (default), understand, debug, test. Narrows which sections are returned."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.references",
			Description: "Find where a symbol is referenced. Uses gopls when available (Source=lsp, compiler-resolved); otherwise falls back to the approximate name-matched call graph (Source=approximate).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":       map[string]interface{}{"type": "string", "description": "File containing the symbol's declaration."},
					"symbolId":   map[string]interface{}{"type": "string", "description": "Exact symbol ID if already known."},
					"symbolName": map[string]interface{}{"type": "string", "description": "Symbol name to resolve when ID is unknown."},
					"budget":     map[string]interface{}{"type": "integer", "description": "Size of the answer in tokens. Cut at whole references; the rest is behind continue=<handle>."},
					"continue":   map[string]interface{}{"type": "string", "description": "Handle from a cut answer: its next page."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.rename",
			Description: "Rename a symbol repository-wide via gopls, rewriting every call site deterministically. Refuses rather than guessing when gopls is unavailable or the language is unsupported — it never renames from approximate name matches.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":             map[string]interface{}{"type": "string", "description": "File containing the symbol's declaration."},
					"symbolId":         map[string]interface{}{"type": "string", "description": "Exact symbol ID if already known."},
					"symbolName":       map[string]interface{}{"type": "string", "description": "Symbol name to resolve when ID is unknown."},
					"newName":          map[string]interface{}{"type": "string", "description": "New identifier for the symbol."},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Revision expected before editing."},
				},
				"required": []string{"path", "newName"},
			},
		},
		{
			Name:        "jade.find",
			Description: "Locate declarations by name AND return their source in one call — the fused search-and-read that replaces `grep -n 'func X' -A 30`. Exact name matches win over substring ones. Use this instead of outline and read_range when you have not located the symbol yet. Pass queries to find several names in one call.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":      map[string]interface{}{"type": "string", "description": "Symbol name, exact or partial."},
					"queries":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Several symbol names in one call, instead of query. Each is answered as query would be."},
					"kind":       map[string]interface{}{"type": "string", "description": "Narrow by kind. func/function, type/struct/class/interface, method, const, var — spellings within a family are equivalent. Empty matches any."},
					"limit":      map[string]interface{}{"type": "integer", "description": "Maximum declarations to return (default 5). Prefer budget."},
					"budget":     map[string]interface{}{"type": "integer", "description": "Size of the answer in tokens. Cut at whole declarations; the rest is behind continue=<handle>."},
					"continue":   map[string]interface{}{"type": "string", "description": "Handle from a cut answer: its next page."},
					"maxLines":   map[string]interface{}{"type": "integer", "description": "Maximum lines of each body (default 40). Prefer budget."},
					"dependency": map[string]interface{}{"type": "string", "description": "Look in this dependency's source instead of the workspace, read-only: a crate, Go module, npm or Python package name."},
				},
				"required": []string{},
			},
		},
		{
			Name:        "jade.retrieve",
			Description: "Select the most relevant files and symbols within a token budget for a task query.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":     map[string]interface{}{"type": "string", "description": "Task description or symbol name to retrieve."},
					"maxTokens": map[string]interface{}{"type": "integer", "description": "Maximum budget to keep retrieval under."},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "jade.replace_symbol",
			Description: "Replace a whole declaration. symbolId takes either the full path::Name@line ID or just path::Name when that name is unique in the file — no lookup call needed first. An ambiguous name is refused with the candidates listed.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"symbolId":         map[string]interface{}{"type": "string", "description": "path::Name@line, or path::Name when unique in the file."},
					"newCode":          map[string]interface{}{"type": "string", "description": "Replacement source code."},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Revision expected before editing. Reject if the workspace has moved on."},
				},
				"required": []string{"symbolId", "newCode"},
			},
		},
		{
			Name:        "jade.insert",
			Description: "Add text to a file without replacing anything — a new function, a new section, an extra case. Use this for additive work instead of rewriting a surrounding symbol. With no anchor it appends to the end of the file; with one it places the text before or after that anchor, refusing if the anchor is absent or matches more than once. Several additions or edits at once belong in apply.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":             map[string]interface{}{"type": "string", "description": "File to add to."},
					"text":             map[string]interface{}{"type": "string", "description": "Text to insert."},
					"anchor":           map[string]interface{}{"type": "string", "description": "Optional. Exact, unique text to place the insertion beside. Omit to append to the end of the file."},
					"position":         map[string]interface{}{"type": "string", "description": "Optional. \"before\" or \"after\" the anchor. Defaults to after."},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Optional. Revision expected before editing; the edit is rejected if the workspace has moved on. Omit for no precondition."},
					"expectedDigest":   map[string]interface{}{"type": "string", "description": "Optional. The digest from the read this edit is based on; the edit is refused if the file changed since, by anyone."},
				},
				"required": []string{"path", "text"},
			},
		},
		{
			Name:        "jade.replace_text",
			Description: "Replace an exact, unique string in a file. An anchor string does not move when the lines around it do, which is why follow-up edits address text rather than line numbers. Refuses when the anchor is absent or matches more than once — extend it with surrounding context to disambiguate. For several sites, use apply: atomic, one validation, no diagnostics from half-done intermediate states.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":             map[string]interface{}{"type": "string", "description": "File path to edit."},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Revision expected before editing."},
					"expectedDigest":   map[string]interface{}{"type": "string", "description": "Optional. The digest from the read this edit is based on; the edit is refused if the file changed since, by anyone."},
					"oldText":          map[string]interface{}{"type": "string", "description": "Exact text to replace. Must appear exactly once."},
					"newText":          map[string]interface{}{"type": "string", "description": "Replacement text."},
				},
				// See replace_range: empty means "no precondition", so requiring it
				// would be a contract the server does not enforce.
				"required": []string{"path", "oldText", "newText"},
			},
		},
		{
			Name:        "jade.create_file",
			Description: "Create a brand-new file. Refuses to overwrite an existing one — use replace_text, apply or replace_file to modify existing content.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string", "description": "File path to create."},
					"content": map[string]interface{}{"type": "string", "description": "File content."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "jade.apply",
			Description: "Apply several edits as one atomic unit: all land or none do. Ops: replace_text, replace_range, replace_symbol, delete_symbol, insert. Anchors are validated before anything is written, touched files are formatted, and one validation runs at the end instead of one per edit. Prefer this over several single edits when changing more than one site.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"edits": map[string]interface{}{
						"type":        "array",
						"description": "Edits to apply in order.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"op":             map[string]interface{}{"type": "string", "description": "replace_text, replace_range, replace_symbol, delete_symbol or insert."},
								"path":           map[string]interface{}{"type": "string", "description": "File to edit."},
								"oldText":        map[string]interface{}{"type": "string", "description": "replace_text: exact text to replace, must be unique."},
								"newText":        map[string]interface{}{"type": "string", "description": "Replacement or inserted text."},
								"expectedDigest": map[string]interface{}{"type": "string", "description": "Optional. Refuse the whole apply if this file changed since the read that returned this digest."},
								"symbolId":       map[string]interface{}{"type": "string", "description": "replace_symbol/delete_symbol: exact symbol ID."},
								"symbolName":     map[string]interface{}{"type": "string", "description": "replace_symbol/delete_symbol: symbol name."},
								"startLine":      map[string]interface{}{"type": "integer", "description": "replace_range: inclusive start."},
								"endLine":        map[string]interface{}{"type": "integer", "description": "replace_range: inclusive end."},
								"anchor":         map[string]interface{}{"type": "string", "description": "insert: unique text to insert relative to."},
								"position":       map[string]interface{}{"type": "string", "description": "insert: end (default), start, before or after."},
							},
							"required": []string{"op", "path"},
						},
					},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Revision expected before editing."},
					"format":           map[string]interface{}{"type": "boolean", "description": "Format touched files afterwards (default true)."},
					"check":            map[string]interface{}{"type": "string", "description": "Run one validation after all edits: build, typecheck, tests, or impact — only the tests covering the edited files and the callers of the declarations the edits touched."},
				},
				"required": []string{"edits"},
			},
		},
		{
			Name:        "jade.delete_symbol",
			Description: "Delete one declaration entirely. The range comes from jade's parse, so the caller never has to find the closing brace; the blank line the declaration left behind is removed too.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":             map[string]interface{}{"type": "string", "description": "File containing the declaration."},
					"symbolId":         map[string]interface{}{"type": "string", "description": "Exact symbol ID if already known."},
					"symbolName":       map[string]interface{}{"type": "string", "description": "Symbol name to resolve when ID is unknown."},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Revision expected before editing."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.delete_file",
			Description: "Delete a file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "File path to delete."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.check",
			Description: "Run a validation command on demand and wait for the verdict: kind build (default), typecheck or tests. Uses the repository's own Makefile target, npm script or cargo command when present. Waits by default and returns pass/fail directly. Every result names the command that ran; dryRun names it without running anything. In a repository with several projects, pass target to check one; with no command at the root, the answer lists the projects.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"kind":           map[string]interface{}{"type": "string", "description": "build (default), typecheck, tests, lint or codegen. lint and codegen run the declared commands of that kind; lint with none declared runs typecheck."},
					"wait":           map[string]interface{}{"type": "boolean", "description": "Wait for the result (default true). False returns a job ID to poll."},
					"timeoutSeconds": map[string]interface{}{"type": "integer", "description": "Bound on the wait (default 90, max 300)."},
					"dryRun":         map[string]interface{}{"type": "boolean", "description": "Name the command that would run, without running it."},
					"target":         map[string]interface{}{"type": "string", "description": "Project directory inside the workspace to check, e.g. services/api. Omit for the workspace root."},
				},
			},
		},
		{
			Name:        "jade.replace_file",
			Description: "Overwrite an existing file's entire contents — for rewriting a document rather than amending it, where there is no anchor to edit against. Refuses to create a new file; use create_file for that. Prefer replace_text or apply when only part of the file changes.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string", "description": "Existing file to overwrite."},
					"content": map[string]interface{}{"type": "string", "description": "The file's complete new contents."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "jade.telemetry",
			Description: "Report how jade's own tools have been used in this workspace: calls, response bytes and timing per tool, plus the failure classes that most often end with a caller falling back to the shell. Records no arguments, no response bodies and no error text.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reset": map[string]interface{}{"type": "boolean", "description": "Clear the log instead of summarizing it."},
				},
			},
		},
		{
			Name:        "jade.grep",
			Description: "Literal or regex text search across the workspace, returning path:line matches with optional trailing context — the replacement for `grep -rn`. Use this for anything that is not a declaration name: struct fields, string literals, error messages, config keys, or any search needing a path filter. Use find instead when you want a declaration and its body. Pass queries to search several patterns in one call.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":      map[string]interface{}{"type": "string", "description": "Text to find."},
					"queries":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Several patterns in one call, instead of query. Each is answered as query would be, with the same filters."},
					"regex":      map[string]interface{}{"type": "boolean", "description": "Treat query as a regular expression. grep-style \\| alternation and \\( \\) groups work as in grep."},
					"ignoreCase": map[string]interface{}{"type": "boolean", "description": "Case-insensitive match."},
					"glob":       map[string]interface{}{"type": "string", "description": "Restrict by path, e.g. *.go or internal/code/*."},
					"exclude":    map[string]interface{}{"type": "string", "description": "Skip paths containing this substring, e.g. testdata."},
					"context":    map[string]interface{}{"type": "integer", "description": "Trailing lines to show per match, like grep -A (max 40)."},
					"limit":      map[string]interface{}{"type": "integer", "description": "Maximum matches returned (default 40). The true total is always reported. Prefer budget."},
					"budget":     map[string]interface{}{"type": "integer", "description": "Size of the answer in tokens. Cut at whole matches; the rest is behind continue=<handle>."},
					"continue":   map[string]interface{}{"type": "string", "description": "Handle from a cut answer: its next page. Other arguments except budget are ignored."},
					"dependency": map[string]interface{}{"type": "string", "description": "Search this dependency's source instead of the workspace, read-only, at the version the project locks: a crate, Go module, npm or Python package name. Matches read as dep:<name>/<path>, which read_range accepts."},
				},
				"required": []string{},
			},
		},
		{
			Name:        "jade.run_command",
			Description: "Run one of the repository's declared commands by name and wait for the verdict — lint, vet, codegen, migrate, anything check() does not cover. Call with no name to list what this repo declares. Use this instead of a shell: it returns pass/fail with the decisive output, and an unknown name answers with the commands that do exist.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":           map[string]interface{}{"type": "string", "description": "Declared command to run. Omit to list the declared commands."},
					"wait":           map[string]interface{}{"type": "boolean", "description": "Wait for the result (default true). False returns a job ID to poll."},
					"timeoutSeconds": map[string]interface{}{"type": "integer", "description": "Bound on the wait (default 90, max 300)."},
				},
			},
		},
		{
			Name:        "jade.declare_command",
			Description: "Declare a named command in .jade/commands.json so it can be replayed by name later. Declare once, then invoke with run_command — do not redeclare a command that already exists just to run it.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":        map[string]interface{}{"type": "string", "description": "Command name: lowercase letters, digits, ':', '_' or '-'."},
					"run":         map[string]interface{}{"type": "string", "description": "Shell command to run from the workspace root."},
					"description": map[string]interface{}{"type": "string", "description": "Optional note on what the command is for."},
					"remove":      map[string]interface{}{"type": "boolean", "description": "Delete the named command instead of declaring it."},
					"kind":        map[string]interface{}{"type": "string", "description": "Optional. lint or codegen makes the command a validation step: check with that kind runs every declared command of it."},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "jade.run_tests",
			Description: "Run a scoped test run (all/file/test/changed) and wait for the verdict. Waits by default and returns pass/fail with the decisive summary; set wait=false for a job ID to poll instead.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"scope":          map[string]interface{}{"type": "string", "description": "One of: all, file, test, changed. Defaults to all."},
					"file":           map[string]interface{}{"type": "string", "description": "Test file to run, for scope=file (Go: its package). With scope=test, limits the name filter to this file."},
					"test":           map[string]interface{}{"type": "string", "description": "Test name, for scope=test: exact in Go, the runner's name filter elsewhere (jest/vitest -t, ava --match, pytest -k, cargo test <name>)."},
					"wait":           map[string]interface{}{"type": "boolean", "description": "Wait for the result (default true). False returns a job ID to poll."},
					"timeoutSeconds": map[string]interface{}{"type": "integer", "description": "Bound on the wait (default 90, max 300)."},
				},
			},
		},
		{
			Name:        "jade.changes",
			Description: "Return the current workspace revision and a list of changed paths.",
			InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			Name:        "jade.diff",
			Description: "Return the actual patch text for the workspace or one path — the 'what changed' companion to changes()'s 'how much changed'. Includes untracked files. Pass since to diff against another revision (HEAD~3, a branch, a SHA), which is how to see what a branch has done once part of the work is already committed. A large patch comes a page at a time, whole lines, with continue=<handle> for the rest.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target":   map[string]interface{}{"type": "string", "description": "Optional file path. Omit to diff the whole working tree."},
					"since":    map[string]interface{}{"type": "string", "description": "Optional git revision to diff against, e.g. HEAD~3, main, or a commit SHA. Omit for the working-tree diff against HEAD."},
					"budget":   map[string]interface{}{"type": "integer", "description": "Size of the patch page in tokens (default 2000). Cut at whole lines; the rest is behind continue=<handle>."},
					"continue": map[string]interface{}{"type": "string", "description": "Handle from a cut patch: its next page."},
				},
			},
		},
		{
			Name:        "jade.checkpoint",
			Description: "Mark a revertible point: snapshots the files Jade has edited this session and records git's HEAD. Not a commit, and does not touch git. Checkpoints last for the session.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"note": map[string]interface{}{"type": "string", "description": "Optional checkpoint note."},
				},
			},
		},
		{
			Name:        "jade.revert",
			Description: "Restore the files Jade changed to their state at a checkpoint — edited files restored, files deleted since recreated, files created since removed — as one new revision, all or nothing. Only files Jade touched are restored, and git is never moved. Refuses if a commit has landed since the checkpoint, because restoring would overwrite committed work — use git to move past a commit.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"checkpointId": map[string]interface{}{"type": "string", "description": "Checkpoint ID to restore."},
				},
				"required": []string{"checkpointId"},
			},
		},
		{
			Name:        "jade.job_status",
			Description: "Fetch the status and decisive summary for a background validation job.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{"type": "string", "description": "Background job ID."},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "jade.job_output",
			Description: "Fetch the raw output for a background validation job after a summary was already returned. A long output comes a page at a time, whole lines, with continue=<handle> for the rest.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id":       map[string]interface{}{"type": "string", "description": "Background job ID."},
					"budget":   map[string]interface{}{"type": "integer", "description": "Size of the output page in tokens (default 2000). Cut at whole lines."},
					"continue": map[string]interface{}{"type": "string", "description": "Handle from a cut output: its next page."},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "jade.events",
			Description: "Poll the event stream starting after a cursor position.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"after": map[string]interface{}{"type": "integer", "description": "Cursor to start after."},
					"limit": map[string]interface{}{"type": "integer", "description": "Maximum number of events to return."},
				},
			},
		},
	}
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if err == io.EOF && len(line) == 0 {
				return nil, io.EOF
			}
			if err != io.EOF {
				return nil, err
			}
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}
		return []byte(trimmed), nil
	}
}

func writeMessage(w *bufio.Writer, v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

func decodeID(raw json.RawMessage) interface{} {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var asNumber float64
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		if asNumber == float64(int64(asNumber)) {
			return int64(asNumber)
		}
		return asNumber
	}
	return nil
}

func stringArg(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func intArg(args map[string]interface{}, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// editOpsArg decodes the edits array for jade.apply. It round-trips through
// JSON rather than unpacking each field by hand: the op struct has ten
// fields across five op shapes, and hand-unpacking would silently drop a
// field whenever a new op is added.
func editOpsArg(args map[string]interface{}, key string) ([]protocol.EditOp, error) {
	raw, present := args[key]
	if !present {
		return nil, fmt.Errorf("%s is required", key)
	}

	// insert on its own takes text; an edit in apply takes newText. Callers
	// carry one spelling into the other — building 0.0.4, a whole apply
	// batch was refused for it — so an edit accepts text as well.
	if list, ok := raw.([]interface{}); ok {
		for _, item := range list {
			op, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if _, has := op["newText"]; has {
				continue
			}
			if text, has := op["text"]; has {
				op["newText"] = text
			}
		}
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", key, err)
	}

	var ops []protocol.EditOp
	if err := json.Unmarshal(encoded, &ops); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("%s must contain at least one edit", key)
	}
	return ops, nil
}

// boolArgDefault reads a bool argument that defaults to true when absent —
// distinct from boolArg, whose zero value is false.
func boolArgDefault(args map[string]interface{}, key string, fallback bool) bool {
	if _, present := args[key]; !present {
		return fallback
	}
	return boolArg(args, key)
}

func boolArg(args map[string]interface{}, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

func int64Arg(args map[string]interface{}, key string) int64 {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

// jadeJSONOutput forces the legacy JSON wire format. Plain text is the
// default because the consumer is a language model, but a consumer that
// genuinely parses responses can set JADE_JSON=1 to opt out.
func jadeJSONOutput() bool {
	value := strings.TrimSpace(os.Getenv("JADE_JSON"))
	return value == "1" || strings.EqualFold(value, "true")
}

// jsonResult encodes a tool response for the wire. Despite the name it now
// renders plain text by default, falling back to JSON for any type without a
// renderer so an unrendered response degrades rather than losing content.
// jsonResult is the one place every typed response passes through on its way
// to the wire, which is why the in-band outcome is read here: it is the last
// point at which the concrete type still exists.
func jsonResult(v interface{}) (mcpToolResult, error) {
	outcome, _ := telemetry.ClassifyResponse(v)

	if !jadeJSONOutput() {
		if text, ok := render.Text(v); ok {
			return mcpToolResult{
				Content: []mcpTextContent{{Type: "text", Text: text}},
				outcome: outcome,
			}, nil
		}
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpToolResult{
		Content: []mcpTextContent{{Type: "text", Text: string(data)}},
		outcome: outcome,
	}, nil
}

func fatalf(format string, args ...interface{}) {
	log.Printf(format, args...)
	os.Exit(1)
}
