package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/languages"
	"github.com/julianbei/jade/internal/protocol"
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
}

type mcpServer struct {
	api *internalapi.Server
}

func main() {
	ctx := context.Background()
	root := os.Getenv("JADE_WORKSPACE_ROOT")
	if strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatalf("failed to resolve working directory: %v", err)
		}
		root = cwd
	}

	bus := events.NewBus()
	wm := workspace.NewManager(root, bus)
	ci := code.NewIndex(root, bus)
	ds := diagnostics.NewService()
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

	s := &mcpServer{api: api}
	if err := s.loop(os.Stdin, os.Stdout); err != nil {
		fatalf("mcp loop failed: %v", err)
	}
}

func (s *mcpServer) loop(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()

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
		if err := writeMessage(writer, resp); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
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
					"version":     "0.1.0",
					"description": "Agent IDE runtime for inspect, modify, validate, and state workflows.",
				},
				"instructions": "Use JADE to inspect code, make targeted edits, validate work, and manage revisions/checkpoints in the active workspace.",
			},
		}
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{}}
	case "tools/list":
		return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]interface{}{"tools": tools()}}
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

func (s *mcpServer) handleToolCall(raw json.RawMessage) (mcpToolResult, error) {
	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return mcpToolResult{}, fmt.Errorf("invalid tools/call payload: %w", err)
	}

	args := req.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}

	switch req.Name {
	case "jade.outline":
		path := stringArg(args, "path")
		res, err := s.api.Outline(protocol.OutlineRequest{Path: path})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.read_symbol":
		path := stringArg(args, "path")
		symbolID := stringArg(args, "symbolId")
		symbolName := stringArg(args, "symbolName")
		maxLines := intArg(args, "maxLines")
		res, err := s.api.ReadSymbol(protocol.ReadSymbolRequest{
			Path:       path,
			SymbolID:   symbolID,
			SymbolName: symbolName,
			MaxLines:   maxLines,
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.replace_symbol":
		symbolID := stringArg(args, "symbolId")
		newCode := stringArg(args, "newCode")
		res, err := s.api.ReplaceSymbol(protocol.ReplaceSymbolRequest{SymbolID: symbolID, NewCode: newCode})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.replace_range":
		path := stringArg(args, "path")
		expected := stringArg(args, "expectedRevision")
		startLine := intArg(args, "startLine")
		endLine := intArg(args, "endLine")
		newCode := stringArg(args, "newCode")
		res, err := s.api.ReplaceRange(protocol.ReplaceRangeRequest{
			Path:             path,
			ExpectedRevision: expected,
			StartLine:        startLine,
			EndLine:          endLine,
			NewCode:          newCode,
		})
		if err != nil {
			return mcpToolResult{}, err
		}
		return jsonResult(res)
	case "jade.changes":
		res := s.api.Changes()
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
		res, err := s.api.JobOutput(id)
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
		return mcpToolResult{}, fmt.Errorf("unknown tool: %s", req.Name)
	}
}

func tools() []mcpTool {
	return []mcpTool{
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
			Name:        "jade.read_symbol",
			Description: "Read a symbol body by ID or name and return exact, ambiguous, or not_found resolution metadata.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":       map[string]interface{}{"type": "string", "description": "Repository-relative or workspace-relative file path."},
					"symbolId":   map[string]interface{}{"type": "string", "description": "Exact symbol ID if already known."},
					"symbolName": map[string]interface{}{"type": "string", "description": "Symbol name to resolve when ID is unknown."},
					"maxLines":   map[string]interface{}{"type": "integer", "description": "Maximum number of lines to return from the symbol body."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "jade.replace_symbol",
			Description: "Replace the source for an identified symbol using its stable symbol ID.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"symbolId": map[string]interface{}{"type": "string", "description": "Symbol ID to replace."},
					"newCode":  map[string]interface{}{"type": "string", "description": "Replacement source code."},
				},
				"required": []string{"symbolId", "newCode"},
			},
		},
		{
			Name:        "jade.replace_range",
			Description: "Replace a line range in a file only if the expected revision matches the current workspace revision.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":             map[string]interface{}{"type": "string", "description": "File path to edit."},
					"expectedRevision": map[string]interface{}{"type": "string", "description": "Revision expected before editing."},
					"startLine":        map[string]interface{}{"type": "integer", "description": "Inclusive start line."},
					"endLine":          map[string]interface{}{"type": "integer", "description": "Inclusive end line."},
					"newCode":          map[string]interface{}{"type": "string", "description": "Replacement code to insert in the target range."},
				},
				"required": []string{"path", "expectedRevision", "startLine", "endLine", "newCode"},
			},
		},
		{
			Name:        "jade.changes",
			Description: "Return the current workspace revision and a list of changed paths.",
			InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			Name:        "jade.checkpoint",
			Description: "Create a named checkpoint of the current workspace state.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"note": map[string]interface{}{"type": "string", "description": "Optional checkpoint note."},
				},
			},
		},
		{
			Name:        "jade.revert",
			Description: "Revert the workspace to a previously created checkpoint ID.",
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
			Description: "Fetch the raw output for a background validation job after a summary was already returned.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{"type": "string", "description": "Background job ID."},
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
	contentLength := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key == "content-length" {
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid content-length: %w", err)
			}
			contentLength = n
		}
	}

	if contentLength <= 0 {
		return nil, fmt.Errorf("missing content-length")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeMessage(w *bufio.Writer, v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
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

func jsonResult(v interface{}) (mcpToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpToolResult{Content: []mcpTextContent{{Type: "text", Text: string(data)}}}, nil
}

func fatalf(format string, args ...interface{}) {
	log.Printf(format, args...)
	os.Exit(1)
}
