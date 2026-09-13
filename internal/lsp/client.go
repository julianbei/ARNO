package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrServerGone reports that the language server exited. Callers treat it the
// same as a server that was never installed — degrade, do not fail the tool
// call — so it is a sentinel rather than a formatted string.
var ErrServerGone = errors.New("language server is not running")

// Client is one running language server process.
//
// One client serves one (workspace root, language) pair and is reused across
// calls. That reuse is the entire point: a language server's first answer is
// expensive because it indexes the workspace, and every answer afterwards is
// cheap. Spawning per call — which is what shelling out to the gopls CLI did —
// pays the expensive one every time.
type Client struct {
	name string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	writeMu sync.Mutex

	nextID atomic.Int64

	pendingMu sync.Mutex
	pending   map[string]chan Message

	// diagnostics holds the latest publishDiagnostics per URI. The server
	// pushes these unprompted, so they are recorded as they arrive and read
	// on demand rather than requested.
	diagnosticsMu sync.Mutex
	diagnostics   map[string][]Diagnostic
	// diagnosticsGeneration counts publishes per URI. Reading diagnostics
	// right after an edit needs to know whether what is stored was published
	// for the new content or is left over from before it — the stored list
	// alone cannot say.
	diagnosticsGeneration map[string]int

	// command is the binary actually launched, for naming the checker in
	// responses. The language name alone does not say whether ruby-lsp or
	// solargraph answered.
	command string

	// answerPrompt is the spec's policy for window/showMessageRequest, and
	// declined records prompts it said no to, so a caller can explain why a
	// server that needed permission never became useful.
	answerPrompt func(message string, actions []string) string
	declinedMu   sync.Mutex
	declined     []string

	// open tracks which documents the server believes are open, and at which
	// version. Getting this wrong is the classic LSP bug: a didChange for a
	// document never opened is ignored by some servers and fatal to others.
	openMu sync.Mutex
	open   map[string]int
	// openText is the content the server last received per URI. An
	// incremental-sync server needs a ranged change, and the range of "the
	// whole document" is only knowable from what the server currently holds.
	openText map[string]string

	// capabilities is what the server said it supports, recorded at
	// initialize. Asking for an unsupported feature costs a round trip and
	// returns an error that reads like a jade bug rather than an absent one.
	capabilities map[string]json.RawMessage

	// progress tracks work-done tokens the server has begun and not ended.
	// An indexing server answers semantic requests before it is ready, and
	// answers them wrongly rather than slowly: ruby-lsp returns null for a
	// class rename it gets right two seconds later. The only signal that the
	// index is ready is the `end` of its progress token.
	progressMu sync.Mutex
	// activeProgress maps each unfinished token to its title, so a caller
	// that gives up waiting can say what the server was busy with.
	activeProgress map[string]string
	lastProgress   time.Time
	// progressEnded counts completed progress tokens, so a caller can tell
	// whether any work began and finished after a given moment.
	progressEnded int

	// ready is when initialize completed, the reference point for how long a
	// server gets to start reporting progress before it is assumed to have
	// nothing to report.
	ready time.Time

	// closed means the process is gone — set by reap on exit, and by Close
	// only after the shutdown handshake has been attempted. Setting it any
	// earlier would make Close's own shutdown request fail against itself.
	closed    atomic.Bool
	closeOnce sync.Once
	exited    chan struct{}
	exitErr   error
}

// Start launches the server and completes the initialize handshake.
//
// A failure here is never fatal to jade: the caller falls back to whatever it
// did before this package existed. That is why the error is returned rather
// than logged and swallowed — the caller needs to know to degrade, not to
// stop.
func Start(ctx context.Context, spec ServerSpec, root string) (*Client, error) {
	binary, ok := spec.Locate()
	if !ok {
		return nil, fmt.Errorf("%s is not installed", spec.Command)
	}

	cmd := exec.Command(binary, spec.Args...)
	cmd.Dir = root

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// A server's stderr is chatter — progress, warnings, occasionally a stack
	// trace. It must be drained regardless: a full stderr pipe blocks the
	// server's writes and deadlocks it, which presents as a hang rather than
	// as an error.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client := &Client{
		name:                  spec.Language,
		cmd:                   cmd,
		stdin:                 stdin,
		stdout:                bufio.NewReaderSize(stdout, 64*1024),
		pending:               make(map[string]chan Message),
		diagnostics:           make(map[string][]Diagnostic),
		diagnosticsGeneration: make(map[string]int),
		command:               filepath.Base(binary),
		answerPrompt:          spec.AnswerPrompt,
		open:                  make(map[string]int),
		openText:              make(map[string]string),
		activeProgress:        make(map[string]string),
		exited:                make(chan struct{}),
	}

	go client.readLoop()
	go client.reap()

	if err := client.initialize(ctx, root, spec); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// reap records the process exit so in-flight and future calls can fail fast
// instead of waiting out their timeouts against a dead pipe.
func (c *Client) reap() {
	c.exitErr = c.cmd.Wait()
	c.closed.Store(true)
	close(c.exited)

	// Wake everything still waiting. Without this a caller blocks for the
	// full request timeout after a crash, turning one failure into a stall.
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

func (c *Client) readLoop() {
	for {
		message, err := readMessage(c.stdout)
		if err != nil {
			return
		}

		// A response carries an ID and no method. A server-to-client request
		// carries both; jade answers the few it must and ignores the rest.
		if len(message.ID) > 0 && message.Method == "" {
			c.deliver(message)
			continue
		}
		c.handleServerMessage(message)
	}
}

func (c *Client) deliver(message Message) {
	key := string(message.ID)
	c.pendingMu.Lock()
	ch, ok := c.pending[key]
	if ok {
		delete(c.pending, key)
	}
	c.pendingMu.Unlock()
	if ok {
		ch <- message
		close(ch)
	}
}

// handleServerMessage processes what the server sends unprompted.
//
// Most of it is ignorable, but two things are not: diagnostics, which are the
// only way a server reports them, and server-to-client *requests*, which
// block the server until answered. A server waiting forever on an
// unanswered `workspace/configuration` is a hang with no error anywhere.
func (c *Client) handleServerMessage(message Message) {
	switch message.Method {
	case "textDocument/publishDiagnostics":
		var params PublishDiagnosticsParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return
		}
		c.diagnosticsMu.Lock()
		c.diagnostics[params.URI] = params.Diagnostics
		c.diagnosticsGeneration[params.URI]++
		c.diagnosticsMu.Unlock()

	case "$/progress":
		var params struct {
			Token json.RawMessage `json:"token"`
			Value struct {
				Kind  string `json:"kind"`
				Title string `json:"title"`
			} `json:"value"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return
		}
		token := string(params.Token)
		c.progressMu.Lock()
		switch params.Value.Kind {
		case "begin":
			c.activeProgress[token] = params.Value.Title
		case "end":
			if _, ok := c.activeProgress[token]; ok {
				c.progressEnded++
			}
			delete(c.activeProgress, token)
		}
		c.lastProgress = time.Now()
		c.progressMu.Unlock()

	case "workspace/configuration":
		// Answer with one null per requested item: servers ask this during
		// startup and several will not proceed until they get a reply.
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(message.Params, &params)
		reply := make([]any, len(params.Items))
		c.respond(message.ID, reply)

	case "window/showMessageRequest":
		// A prompt is a server asking permission, often for a side effect in
		// the user's repository. Declining is a null result, not an error: an
		// error reads to some servers as a broken client.
		var params struct {
			Message string `json:"message"`
			Actions []struct {
				Title string `json:"title"`
			} `json:"actions"`
		}
		_ = json.Unmarshal(message.Params, &params)
		titles := make([]string, 0, len(params.Actions))
		for _, action := range params.Actions {
			titles = append(titles, action.Title)
		}
		choice := ""
		if c.answerPrompt != nil {
			choice = c.answerPrompt(params.Message, titles)
		}
		if choice == "" {
			c.declinedMu.Lock()
			c.declined = append(c.declined, params.Message)
			c.declinedMu.Unlock()
			c.respond(message.ID, nil)
			return
		}
		c.respond(message.ID, map[string]string{"title": choice})

	case "window/workDoneProgress/create", "client/registerCapability",
		"client/unregisterCapability":
		// Accepted with an empty result. These are bookkeeping jade does not
		// act on, but they are requests, so they need an answer.
		c.respond(message.ID, nil)

	default:
		// An unrecognised *request* still needs an answer or the server may
		// wait on it. An unrecognised notification carries no ID and is
		// correctly dropped here.
		if len(message.ID) > 0 {
			c.respondError(message.ID, -32601, "method not supported by jade")
		}
	}
}

func (c *Client) respond(id json.RawMessage, result any) {
	payload, err := json.Marshal(result)
	if err != nil {
		return
	}
	_ = c.send(Message{ID: id, Result: payload})
}

func (c *Client) respondError(id json.RawMessage, code int, message string) {
	_ = c.send(Message{ID: id, Error: &ResponseError{Code: code, Message: message}})
}

func (c *Client) send(message Message) error {
	if c.closed.Load() {
		return ErrServerGone
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeMessage(c.stdin, message)
}

// Call makes a request and waits for its response.
//
// Every call is bounded. A language server that stops answering must cost one
// timeout, not a hung tool call: jade's whole proposition is being cheaper
// than the shell, and a tool that sometimes hangs forever is not cheaper at
// any token count.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if c.closed.Load() {
		return ErrServerGone
	}

	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}

	id := c.nextID.Add(1)
	rawID, _ := json.Marshal(id)

	ch := make(chan Message, 1)
	c.pendingMu.Lock()
	c.pending[string(rawID)] = ch
	c.pendingMu.Unlock()

	if err := c.send(Message{ID: rawID, Method: method, Params: payload}); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, string(rawID))
		c.pendingMu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, string(rawID))
		c.pendingMu.Unlock()
		// Tell the server to stop working on it. Best-effort: a cancel that
		// does not arrive costs the server some wasted work, not correctness.
		cancelParams, _ := json.Marshal(map[string]any{"id": id})
		_ = c.send(Message{Method: "$/cancelRequest", Params: cancelParams})
		return ctx.Err()

	case message, ok := <-ch:
		if !ok {
			return ErrServerGone
		}
		if message.Error != nil {
			return message.Error
		}
		if result == nil || len(message.Result) == 0 {
			return nil
		}
		return json.Unmarshal(message.Result, result)
	}
}

// Notify sends a notification, which by definition has no reply.
func (c *Client) Notify(method string, params any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(Message{Method: method, Params: payload})
}

func (c *Client) initialize(ctx context.Context, root string, spec ServerSpec) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   pathToURI(root),
		"workspaceFolders": []map[string]any{
			{"uri": pathToURI(root), "name": filepathBase(root)},
		},
		"capabilities": map[string]any{
			// Without this a server is not allowed to report progress, and
			// progress is the only way to know its index is ready. See
			// WaitSettled.
			"window": map[string]any{"workDoneProgress": true},
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"didSave":           false,
					"willSave":          false,
					"willSaveWaitUntil": false,
				},
				"references": map[string]any{},
				"rename":     map[string]any{"prepareSupport": false},
				"publishDiagnostics": map[string]any{
					"relatedInformation": false,
				},
				"definition":     map[string]any{},
				"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
				"configuration":    true,
				// Declaring documentChanges tells the server it may answer a
				// rename with the richer shape. Both are handled, but a server
				// told otherwise will use the older one, and saying what is
				// actually supported keeps the two in step.
				"workspaceEdit": map[string]any{"documentChanges": true},
			},
		},
		"initializationOptions": spec.InitializationOptions,
	}

	var result struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := c.Call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	c.capabilities = result.Capabilities
	c.ready = time.Now()

	return c.Notify("initialized", map[string]any{})
}

// Settling bounds. The grace period is how long after initialize a server
// gets to announce indexing; ruby-lsp begins its progress token within a
// fraction of a second, and a server that says nothing in that window is
// taken at its word. Quiet absorbs back-to-back tokens, which rust-analyzer
// and jdtls both emit, so one ending is not mistaken for all of them ending.
const (
	settleGrace   = 1500 * time.Millisecond
	settleQuiet   = 300 * time.Millisecond
	SettleTimeout = 60 * time.Second
)

// WaitSettled blocks until the server has no work-done progress outstanding.
//
// An indexing server does not answer slowly, it answers wrongly: ruby-lsp
// returns null for a class rename it gets right once indexing ends, and a
// null rename reads as "cannot rename" rather than "ask again". Waiting here
// costs nothing when the server is idle, a moment on the first call after
// start, and at most SettleTimeout for a server that never ends a token —
// after which the request goes ahead and the answer is whatever it is.
func (c *Client) WaitSettled(ctx context.Context) {
	deadline := time.Now().Add(SettleTimeout)
	for {
		c.progressMu.Lock()
		active := len(c.activeProgress)
		last := c.lastProgress
		c.progressMu.Unlock()

		now := time.Now()
		if active == 0 && now.Sub(c.ready) >= settleGrace && now.Sub(last) >= settleQuiet {
			return
		}
		if now.After(deadline) || c.closed.Load() {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-c.exited:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Supports reports whether the server advertised a capability. Asking a server
// for something it does not implement wastes a round trip and produces an
// error that reads like a jade bug rather than an absent feature.
func (c *Client) Supports(capability string) bool {
	raw, ok := c.capabilities[capability]
	if !ok {
		return false
	}
	// Capabilities are either a boolean or an options object; both mean yes,
	// and only an explicit false means no.
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		return enabled
	}
	return string(raw) != "null"
}

// Diagnostics returns the latest published diagnostics for a file.
func (c *Client) Diagnostics(path string) []Diagnostic {
	c.diagnosticsMu.Lock()
	defer c.diagnosticsMu.Unlock()
	return append([]Diagnostic(nil), c.diagnostics[pathToURI(path)]...)
}

// diagnosticsQuiet is how long a file's diagnostics must stay unchanged after a
// publish before they are taken as the answer for the current content.
const diagnosticsQuiet = 400 * time.Millisecond

// DeclinedPrompt returns the first declined prompt whose message contains
// substring, case-insensitively, or "" if none was declined.
func (c *Client) DeclinedPrompt(substring string) string {
	c.declinedMu.Lock()
	defer c.declinedMu.Unlock()
	for _, prompt := range c.declined {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(substring)) {
			return prompt
		}
	}
	return ""
}

// ProgressEnded is how many progress tokens have completed. Take it before a
// change; a larger value afterwards means the server began and finished some
// work since — for a compile-on-save server, the compile.
func (c *Client) ProgressEnded() int {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	return c.progressEnded
}

// WaitForCompiledDiagnostics is WaitForDiagnostics for servers whose
// diagnostics exist only after compiling (DiagnosticsAfterCompile). It returns
// true only once a progress token that ended after endedBefore has completed
// and the file's diagnostics have settled after it.
//
// Without that condition the empty publish metals sends on save — before it
// has compiled anything — is indistinguishable from a clean compile.
func (c *Client) WaitForCompiledDiagnostics(ctx context.Context, path string, endedBefore int, timeout time.Duration) ([]Diagnostic, bool) {
	uri := pathToURI(path)
	deadline := time.Now().Add(timeout)
	for {
		compiled := c.ProgressEnded() > endedBefore && c.Busy() == ""
		if compiled {
			c.progressMu.Lock()
			lastProgress := c.lastProgress
			c.progressMu.Unlock()
			if time.Since(lastProgress) >= diagnosticsQuiet {
				c.diagnosticsMu.Lock()
				published := append([]Diagnostic(nil), c.diagnostics[uri]...)
				c.diagnosticsMu.Unlock()
				return published, true
			}
		}
		if time.Now().After(deadline) || c.closed.Load() {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-c.exited:
			return nil, false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Busy names what the server is working on — the title of an unfinished
// progress token — or returns empty when it reports nothing in progress.
func (c *Client) Busy() string {
	c.progressMu.Lock()
	defer c.progressMu.Unlock()
	titles := make([]string, 0, len(c.activeProgress))
	for _, title := range c.activeProgress {
		if title == "" {
			title = "unnamed work"
		}
		titles = append(titles, title)
	}
	if len(titles) == 0 {
		return ""
	}
	sort.Strings(titles)
	return strings.Join(titles, ", ")
}

// syncKind is the document sync the server asked for: 1 full, 2 incremental.
// Servers that omit it get full sync, the LSP default most accept.
func (c *Client) syncKind() int {
	raw, ok := c.capabilities["textDocumentSync"]
	if !ok {
		return 1
	}
	var kind int
	if err := json.Unmarshal(raw, &kind); err == nil {
		return kind
	}
	var options struct {
		Change int `json:"change"`
	}
	if err := json.Unmarshal(raw, &options); err == nil && options.Change != 0 {
		return options.Change
	}
	return 1
}

// Command is the name of the server binary this client launched.
func (c *Client) Command() string {
	return c.command
}

// DiagnosticsGeneration is how many times the server has published
// diagnostics for a file. Take it before syncing a change, then pass it to
// WaitForDiagnostics.
func (c *Client) DiagnosticsGeneration(path string) int {
	c.diagnosticsMu.Lock()
	defer c.diagnosticsMu.Unlock()
	return c.diagnosticsGeneration[pathToURI(path)]
}

// WaitForDiagnostics waits for a publish newer than generation `after` and
// returns it. The bool is false when none arrived within timeout.
//
// Servers publish after a change on their own schedule, so reading the stored
// list immediately after a sync returns the diagnostics for the previous
// content — an error just fixed still showing, or one just introduced
// missing. Both are worse than saying nothing arrived, which is what false
// lets a caller report.
func (c *Client) WaitForDiagnostics(ctx context.Context, path string, after int, timeout time.Duration) ([]Diagnostic, bool) {
	uri := pathToURI(path)
	deadline := time.Now().Add(timeout)
	seen := after
	var lastPublish time.Time
	for {
		c.diagnosticsMu.Lock()
		generation := c.diagnosticsGeneration[uri]
		published := append([]Diagnostic(nil), c.diagnostics[uri]...)
		c.diagnosticsMu.Unlock()

		now := time.Now()
		if generation > seen {
			// A fresh publish is not necessarily the final one. metals
			// publishes an empty list on change and the real result after it
			// compiles; returning the first reports a broken file as clean.
			seen = generation
			lastPublish = now
		}
		busy := c.Busy() != ""
		if seen > after && !busy && now.Sub(lastPublish) >= diagnosticsQuiet {
			return published, true
		}
		if now.After(deadline) || c.closed.Load() {
			// Still working when time ran out means whatever was published
			// may predate the work that would find the error. Not an answer.
			if seen > after && !busy {
				return published, true
			}
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-c.exited:
			return nil, false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Close shuts the server down, politely first.
//
// The sequence is required by the protocol — shutdown, then exit — and the
// grace period is short because a server that will not leave gets killed
// anyway. Leaking a language server process per jade session would be
// noticeable within an afternoon: jdtls holds hundreds of megabytes.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		// Deliberately before marking closed: Call and Notify refuse once the
		// flag is set, so flipping it first would make the handshake below a
		// no-op and leave the server running.
		if !c.closed.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			_ = c.Call(ctx, "shutdown", nil, nil)
			_ = c.Notify("exit", nil)
			cancel()
		}

		c.closed.Store(true)
		_ = c.stdin.Close()

		select {
		case <-c.exited:
		case <-time.After(shutdownTimeout):
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
	})
	return nil
}

// filepathBase is filepath.Base, named locally so types.go's import of
// path/filepath is not duplicated across files for one call.
func filepathBase(path string) string {
	return filepath.Base(path)
}

const shutdownTimeout = 3 * time.Second
