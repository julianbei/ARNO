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

	closed bool
}

func NewManager(root string) *Manager {
	return &Manager{
		root:    root,
		clients: make(map[string]*Client),
		failed:  make(map[string]error),
	}
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
// jade edits files on disk rather than holding buffers, so there is no
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
	client.openMu.Unlock()

	if !alreadyOpen {
		return client.Notify("textDocument/didOpen", DidOpenTextDocumentParams{
			TextDocument: TextDocumentItem{
				URI:        uri,
				LanguageID: languageID,
				Version:    version,
				Text:       text,
			},
		})
	}

	return client.Notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: version},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: text}},
	})
}

// Close shuts every server down. Called when the jade process exits; leaking
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

// LanguageForPath maps a file to jade's language identifier, matching the
// extensions internal/code parses with a grammar. A file jade cannot parse
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
