package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Timeouts. Startup is generous and per-request is not, for the same reason:
// a server's first act is indexing the workspace, which is legitimately slow,
// while an answer from an indexed server is fast or is not coming.
const (
	StartTimeout   = 45 * time.Second
	RequestTimeout = 15 * time.Second
)

// Manager owns the running language servers for one workspace.
//
// Servers are started lazily on first use and kept alive afterwards. A
// language whose server is missing, broken or slow to start is remembered as
// unavailable so the next call does not pay the timeout again — a tool that
// costs 45 seconds to tell you it cannot help is worse than one that says so
// immediately.
type Manager struct {
	root string

	mu      sync.Mutex
	clients map[string]*Client
	// failed records languages whose server could not be started, with why.
	// Kept for reporting: "pyright is not installed" is actionable, and
	// "semantic features unavailable" is not.
	failed map[string]error
	// restarts counts how often each language's server was started again
	// after dying.
	restarts map[string]int

	closed bool
}

func NewManager(root string) *Manager {
	return &Manager{
		root:    root,
		clients: make(map[string]*Client),
		failed:  make(map[string]error),
	}
}

// maxRestarts is how often a language's server is started again after it
// dies. Once turns one bad moment into a working session; more turns a server
// that crashes on this workspace into a crash per call.
const maxRestarts = 1

// allowRestart records that language's server died and says whether to start
// it again: up to maxRestarts times, then the language is marked failed with
// the reason, which capabilities and references report. Called with m.mu held.
func (m *Manager) allowRestart(language string) bool {
	if m.restarts == nil {
		m.restarts = map[string]int{}
	}
	if m.restarts[language] >= maxRestarts {
		m.failed[language] = errors.New("the " + language + " server exited again after a restart; not restarted this session")
		return false
	}
	m.restarts[language]++
	return true
}

// ServerState is what a language's server is doing, telling apart the three
// kinds of missing — no server known, server not installed, server installed
// but failed — from one that runs, indexes, or has not been needed yet.
type ServerState struct {
	// State is "running", "indexing", "not started", "failed", "not installed"
	// or "not supported".
	State string
	// Detail is the server's command, its current work while indexing, or the
	// reason it failed.
	Detail string
}

// Status reports language's server without starting one.
func (m *Manager) Status(language string) ServerState {
	spec, known := SpecFor(language)
	if !known {
		return ServerState{State: "not supported"}
	}
	if m != nil {
		m.mu.Lock()
		client, running := m.clients[language]
		failure := m.failed[language]
		m.mu.Unlock()
		if running && !client.closed.Load() {
			if busy := client.Busy(); busy != "" {
				return ServerState{State: "indexing", Detail: busy}
			}
			return ServerState{State: "running", Detail: spec.Command}
		}
		if failure != nil {
			var missing errNotInstalled
			if errors.As(failure, &missing) {
				return ServerState{State: "not installed", Detail: spec.Command}
			}
			return ServerState{State: "failed", Detail: failure.Error()}
		}
	}
	if resolved, found := spec.Resolve(); found {
		return ServerState{State: "not started", Detail: resolved.Command}
	}
	return ServerState{State: "not installed", Detail: spec.Command}
}

// ClientFor returns a running server for the given language, starting one if
// needed.
//
// The bool is the whole contract: false means "no semantic answer available,
// use your fallback", and is returned for a server that is not installed, one
// that failed to start, and one that has since died. Callers must never treat
// it as an error condition — before this package, every language except Go
// was in exactly this state permanently.
func (m *Manager) ClientFor(ctx context.Context, language string) (*Client, bool) {
	if m == nil {
		return nil, false
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, false
	}
	if client, ok := m.clients[language]; ok {
		if !client.closed.Load() {
			m.mu.Unlock()
			return client, true
		}
		// The server died since last time. Drop it and try once more: a
		// crashed server that is never retried turns one bad moment into a
		// permanently degraded session.
		delete(m.clients, language)
		if !m.allowRestart(language) {
			m.mu.Unlock()
			return nil, false
		}
	}
	if err, ok := m.failed[language]; ok && err != nil {
		m.mu.Unlock()
		return nil, false
	}
	m.mu.Unlock()

	spec, ok := SpecFor(language)
	if !ok {
		m.markFailed(language, errNoServerConfigured)
		return nil, false
	}
	resolved, found := spec.Resolve()
	if !found {
		m.markFailed(language, errNotInstalled{command: spec.Command})
		return nil, false
	}

	client, err := m.start(ctx, language, resolved)
	if err != nil {
		m.markFailed(language, err)
		return nil, false
	}
	return client, true
}

// FallbacksFor returns running clients for the language's installed
// alternatives, starting them on demand.
//
// Only used after the primary server declined an operation, so the cost of a
// second process is paid by the sessions that need it and nobody else. A
// fallback that fails to start is skipped rather than recorded against the
// language: the primary is still working.
func (m *Manager) FallbacksFor(ctx context.Context, language string) []*Client {
	if m == nil {
		return nil
	}
	spec, ok := SpecFor(language)
	if !ok {
		return nil
	}

	var out []*Client
	for _, fallback := range spec.Fallbacks() {
		key := language + "/" + fallback.Command
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return out
		}
		existing, running := m.clients[key]
		_, broken := m.failed[key]
		m.mu.Unlock()

		if running && !existing.closed.Load() {
			out = append(out, existing)
			continue
		}
		if broken {
			continue
		}
		client, err := m.start(ctx, key, fallback)
		if err != nil {
			m.markFailed(key, err)
			continue
		}
		out = append(out, client)
	}
	return out
}

// start launches a server and records it under key, handling the races with
// Close and with a concurrent start for the same key.
func (m *Manager) start(ctx context.Context, key string, spec ServerSpec) (*Client, error) {
	startCtx, cancel := context.WithTimeout(ctx, StartTimeout)
	defer cancel()

	client, err := Start(startCtx, spec, m.root)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		// Raced with Close. Do not leak the process just started.
		go client.Close()
		return nil, errManagerClosed
	}
	// Another goroutine may have started one for the same key while this one
	// was initializing. Keep the winner and shut down the duplicate.
	if existing, ok := m.clients[key]; ok && !existing.closed.Load() {
		go client.Close()
		return existing, nil
	}
	m.clients[key] = client
	return client, nil
}

var errManagerClosed = errors.New("language server manager is closed")

func (m *Manager) markFailed(language string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failed[language] = err
}

// Unavailable reports why a language has no server, for diagnostics and
// reporting. Empty means it is available or has not been tried.
func (m *Manager) Unavailable(language string) string {
	if m == nil {
		return "no language server manager"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.failed[language]; ok && err != nil {
		return err.Error()
	}
	return ""
}

// Sync makes sure the server's view of a file matches what is on disk.
//
// ARNO edits files on disk rather than holding buffers, so there is no
// incremental change to report — the file simply differs from what the server
// last saw. Sending didOpen once and didChange with the full text afterwards
// is both correct and the honest description.
//
// This is the step that decides whether answers are right. A server asked
// about a file it believes is two edits old will answer confidently about
// code that no longer exists.
func (m *Manager) Sync(client *Client, path string, languageID string) error {
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(m.root, path)
	}

	content, err := os.ReadFile(absolute)
	if err != nil {
		return err
	}
	text := string(content)
	uri := pathToURI(absolute)

	client.openMu.Lock()
	version, alreadyOpen := client.open[uri]
	version++
	client.open[uri] = version
	previous := client.openText[uri]
	client.openText[uri] = text
	client.openMu.Unlock()

	if !alreadyOpen {
		err = client.Notify("textDocument/didOpen", DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        uri,
				LanguageID: languageID,
				Version:    version,
				Text:       text,
			},
		})
	} else if client.syncKind() == 2 {
		// Incremental servers get one change whose range covers the whole
		// previous document. A rangeless change is valid LSP, but ruby-lsp
		// declares incremental sync and silently keeps the old content when
		// sent one — every diagnostic it then returns is for code that is no
		// longer on disk.
		err = client.Notify("textDocument/didChange", map[string]any{
			"textDocument": VersionedTextDocumentIdentifier{URI: uri, Version: version},
			"contentChanges": []map[string]any{{
				"range": map[string]any{
					"start": Position{Line: 0, Character: 0},
					"end":   documentEnd(previous),
				},
				"text": text,
			}},
		})
	} else {
		err = client.Notify("textDocument/didChange", DidChangeTextDocumentParams{
			TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: version},
			ContentChanges: []TextDocumentContentChangeEvent{{Text: text}},
		})
	}
	if err != nil {
		return err
	}

	// The content is already on disk, so this is a save, and servers that
	// analyse on save (metals compiles then) need to hear it. Only sent to
	// servers that asked; the rest treat it as noise at best.
	if client.WantsSave() {
		return client.Notify("textDocument/didSave", map[string]any{
			"textDocument": TextDocumentIdentifier{URI: uri},
			"text":         text,
		})
	}
	return nil
}

// Close shuts every server down. Called when the ARNO process exits; leaking
// language servers is not hypothetical, jdtls and metals both hold hundreds
// of megabytes each.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	clients := make([]*Client, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clients = make(map[string]*Client)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			_ = c.Close()
		}(client)
	}
	wg.Wait()
}

// LanguageForPath maps a file to arno's language identifier, matching the
// extensions internal/code parses with a grammar. A file ARNO cannot parse
// structurally is one no server here claims either.
func LanguageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".rs":
		return "rust"
	case ".py":
		return "python"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".scala", ".sc":
		return "scala"
	default:
		return ""
	}
}

type errNotInstalledType struct{ command string }

type errNotInstalled = errNotInstalledType

func (e errNotInstalledType) Error() string {
	return e.command + " is not installed"
}

var errNoServerConfigured = errNoServerConfiguredType{}

type errNoServerConfiguredType struct{}

func (errNoServerConfiguredType) Error() string {
	return "no language server is configured for this file type"
}

// documentEnd is the LSP position just past the last character of text: the
// last line, at its length in UTF-16 code units.
func documentEnd(text string) Position {
	lines := strings.Split(text, "\n")
	last := lines[len(lines)-1]
	return Position{Line: len(lines) - 1, Character: utf16Column(last, len(last)+1)}
}
