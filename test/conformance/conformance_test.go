// Package conformance drives jade the way a client does — a real jade-mcp
// process, real MCP framing over stdio, real language servers — against one
// small repository per supported language.
//
// It exists because the unit tests cannot answer the question that matters.
// They prove jade's own logic; they cannot prove that pyright resolves a
// Python reference, that jdtls starts at all, or that a rename written by a
// server jade has never run against lands correctly on disk. Every one of
// those has to be found against the real thing, and the only honest way to
// have the real thing on hand is a container that installs it.
//
// Each fixture repository has deliberately the same shape — a Store class
// whose put method is called twice from a second file — so one set of
// assertions describes all eight languages and a per-language difference in
// the result is a real difference rather than a difference in the fixture.
//
// Run: make conformance    (builds the image and runs this inside it)
// Locally it still runs, and skips the languages whose servers are absent.
package conformance

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// languageCase describes what one language's fixture must produce.
type languageCase struct {
	name string
	dir  string

	// file and symbol name the declaration to ask about.
	file   string
	symbol string

	// declarations must all appear in the outline. These come from the
	// grammar and must hold with no language server installed at all.
	declarations []string

	// server is the binary that provides semantics, for skipping.
	server string

	// otherFile is the second file the rename must also rewrite, proving the
	// answer is cross-file rather than local.
	otherFile string

	// renamed is the new name, and what must appear in both files afterwards.
	renamed string

	// renameLimitation records a server that advertises renameProvider but
	// cannot actually rename this kind of symbol. Recorded rather than
	// skipped: the assertion becomes "jade reports the limitation
	// accurately", which is the behaviour that matters when a server
	// declines. Empty means rename is expected to work.
	renameLimitation string
}

func cases() []languageCase {
	return []languageCase{
		{
			name: "go", dir: "go", file: "store.go", symbol: "Put",
			declarations: []string{"Store", "Put", "NewStore"},
			server:       "gopls", otherFile: "use.go", renamed: "Store2",
		},
		{
			name: "typescript", dir: "typescript", file: "store.ts", symbol: "put",
			declarations: []string{"Store", "put", "newStore"},
			server:       "typescript-language-server", otherFile: "use.ts", renamed: "store2",
		},
		{
			name: "javascript", dir: "javascript", file: "store.js", symbol: "put",
			declarations: []string{"Store", "put", "newStore"},
			server:       "typescript-language-server", otherFile: "use.js", renamed: "store2",
		},
		{
			name: "python", dir: "python", file: "store.py", symbol: "put",
			declarations: []string{"Store", "put", "new_store"},
			server:       "pyright-langserver", otherFile: "use.py", renamed: "store2",
		},
		{
			name: "ruby", dir: "ruby", file: "store.rb", symbol: "put",
			declarations: []string{"Store", "put", "new_store"},
			server:       "ruby-lsp", otherFile: "use.rb", renamed: "store2",
			// ruby-lsp 0.26 renames classes and modules but returns null for
			// a method, even fully indexed. The image also has solargraph,
			// which renames methods, so this case proves the fallback: the
			// primary declines, jade asks the alternative, both files change.
		},
		{
			name: "rust", dir: "rust", file: "src/lib.rs", symbol: "put",
			declarations: []string{"Store", "put", "use_store"},
			server:       "rust-analyzer", otherFile: "src/lib.rs", renamed: "store2",
		},
		{
			name: "java", dir: "java", file: "src/main/java/conformance/Store.java", symbol: "put",
			declarations: []string{"Store", "put"},
			server:       "jdtls", otherFile: "src/main/java/conformance/UseStore.java", renamed: "store2",
		},
		{
			name: "scala", dir: "scala", file: "src/main/scala/Store.scala", symbol: "put",
			declarations: []string{"Store", "put", "UseStore"},
			server:       "metals", otherFile: "src/main/scala/Store.scala", renamed: "store2",
		},
	}
}

// TestStructure covers what jade does with nothing installed. Grammars are
// compiled into the binary, so a failure here is jade's alone.
func TestStructure(t *testing.T) {
	binary := jadeBinary(t)

	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtureCopy(t, tc.dir)
			session := newSession(t, binary, root)
			defer session.close()

			out := session.call(t, "jade.outline", map[string]any{"path": tc.file})

			if strings.Contains(out, "no "+tc.name+" grammar") {
				t.Fatalf("%s should be parsed by a grammar, got a text-scan caveat:\n%s", tc.name, out)
			}
			for _, declaration := range tc.declarations {
				if !strings.Contains(out, declaration) {
					t.Errorf("outline is missing %q\n%s", declaration, out)
				}
			}
		})
	}
}

// TestSemantics covers what needs a language server: exact references and a
// cross-file rename. Skipped per language when the server is not installed,
// which is the whole reason the container exists.
func TestSemantics(t *testing.T) {
	binary := jadeBinary(t)

	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			if reason, ok := serverUsable(tc.server); !ok {
				t.Skipf("%s: %s", tc.server, reason)
			}

			root := fixtureCopy(t, tc.dir)
			session := newSession(t, binary, root)
			defer session.close()
			started := time.Now()

			// A server indexes before it can answer, so retry rather than
			// sleeping a guessed amount. The failure being guarded against is
			// a flaky "0 references" that looks like a jade bug.
			var references string
			deadline := time.Now().Add(serverDeadline)
			for time.Now().Before(deadline) {
				references = session.call(t, "jade.references", map[string]any{
					"path": tc.file, "symbolName": tc.symbol,
				})
				// Match the wording the renderer actually produces. An earlier
				// version looked for "(approximate" with a leading paren,
				// which never matched "2 approximate references" — so the
				// retry never fired and every language that needed a moment
				// to index was reported as a jade failure at 0.3s.
				if strings.Contains(references, "approximate") || strings.Contains(references, "0 references") {
					time.Sleep(3 * time.Second)
					continue
				}
				break
			}

			// Recorded for every run so a release that makes a server slower to
			// start or to answer shows up; the budget that fails on it follows
			// from a baseline.
			t.Logf("%s: first exact references after %s", tc.name, time.Since(started).Round(time.Millisecond))

			if strings.Contains(references, "approximate") {
				t.Fatalf("%s: expected the language server to answer, got the name-matched fallback:\n%s",
					tc.name, references)
			}

			renameOut := session.call(t, "jade.rename", map[string]any{
				"path": tc.file, "symbolName": tc.symbol, "newName": tc.renamed,
			})

			if tc.renameLimitation != "" {
				// The server declined. jade must name the server and repeat
				// its reason, never claim no server is available — that
				// sends someone to install what they already have.
				if !strings.Contains(renameOut, tc.renameLimitation) {
					t.Fatalf("%s: expected jade to report the server's own reason (%q), got:\n%s",
						tc.name, tc.renameLimitation, renameOut)
				}
				if strings.Contains(renameOut, "none is available") {
					t.Fatalf("%s: jade blamed a missing server for a running one's refusal:\n%s",
						tc.name, renameOut)
				}
				return
			}

			if strings.Contains(renameOut, "requires a language server") {
				t.Fatalf("%s: rename refused despite a running server:\n%s", tc.name, renameOut)
			}

			// The rename is only real if it is on disk, in both files.
			for _, path := range []string{tc.file, tc.otherFile} {
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				if !strings.Contains(string(data), tc.renamed) {
					t.Errorf("%s: %s does not contain the new name %q after rename:\n%s",
						tc.name, path, tc.renamed, data)
				}
			}
		})
	}
}

// TestDegraded is every language again with its language server hidden: an
// empty PATH and home, so neither PATH nor the toolchain directories jade
// searches can find one. It runs without the container. What it proves is
// that a missing server is reported as missing — by the capability report
// and in the fallback answer itself — rather than silently answered by name
// matching.
func TestDegraded(t *testing.T) {
	binary := jadeBinary(t)
	empty := t.TempDir()
	env := []string{
		"PATH=" + empty, "HOME=" + empty, "GOPATH=" + empty, "GOBIN=",
		"CARGO_HOME=" + empty, "XDG_CONFIG_HOME=" + empty, "JADE_TELEMETRY=0",
	}

	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			root := fixtureCopy(t, tc.dir)
			session := newSessionWithEnv(t, binary, root, env)
			defer session.close()

			capabilities := session.call(t, "jade.capabilities", map[string]any{})
			if !strings.Contains(capabilities, "no server ("+tc.server+" not installed)") {
				t.Fatalf("%s: capabilities should report %s as not installed, got:\n%s", tc.name, tc.server, capabilities)
			}

			references := session.call(t, "jade.references", map[string]any{"path": tc.file, "symbolName": tc.symbol})
			if !strings.Contains(references, "approximate · text index") {
				t.Fatalf("%s: references without a server should say it is approximate, got:\n%s", tc.name, references)
			}
			if !strings.Contains(references, tc.server+" is not installed") {
				t.Fatalf("%s: the fallback should name the missing server, got:\n%s", tc.name, references)
			}
		})
	}
}

const serverDeadline = 90 * time.Second

// versionProbe lists servers that answer `--version` quickly. Used to tell an
// absent server from one that is present but cannot run — which is not a
// hypothetical distinction: rustup puts a `rust-analyzer` shim on PATH whose
// only behaviour is to report "Unknown binary in official toolchain" unless
// the component is installed. LookPath finds it, so a test keyed on presence
// alone tries to use it and reports a jade failure for someone else's
// packaging.
//
// Servers absent from this list are assumed usable if present. jdtls and
// metals are launcher scripts with no fast, reliable version flag, and
// pyright-langserver rejects --version outright ("Connection input stream is
// not set") because it only speaks LSP — probing it wrongly skipped Python
// in the very container built to test it.
var versionProbe = map[string][]string{
	"gopls":                      {"version"},
	"typescript-language-server": {"--version"},
	"rust-analyzer":              {"--version"},
	"ruby-lsp":                   {"--version"},
	"solargraph":                 {"--version"},
}

func serverUsable(server string) (string, bool) {
	if _, err := exec.LookPath(server); err != nil {
		return "not installed", false
	}

	args, probeable := versionProbe[server]
	if !probeable {
		return "", true
	}

	cmd := exec.Command(server, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("installed but not runnable (%s)", firstLine(string(output))), false
	}
	return "", true
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	if text == "" {
		return "no output"
	}
	return text
}

// --- harness ---------------------------------------------------------------

// session is one jade-mcp process speaking MCP over stdio. Reused across calls
// within a test so language servers stay warm, which is also the behaviour
// worth exercising: a fresh process per call would never test reuse.
type session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

func newSession(t *testing.T, binary string, root string) *session {
	t.Helper()
	return newSessionWithEnv(t, binary, root, nil)
}

// newSessionWithEnv starts jade with env as its whole environment, or the
// test's own when env is nil.
func newSessionWithEnv(t *testing.T, binary string, root string, env []string) *session {
	t.Helper()

	cmd := exec.Command(binary, "--root", root)
	cmd.Dir = root
	if env != nil {
		cmd.Env = env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	s := &session{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20), nextID: 1}
	s.request(t, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "conformance", "version": "1"},
	})
	s.notify(t, "notifications/initialized", map[string]any{})
	return s
}

func (s *session) close() {
	_ = s.stdin.Close()
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
}

// call invokes a tool and returns its rendered text, which is what a model
// would actually see. Asserting on that rather than on a struct is deliberate:
// the text is the product.
func (s *session) call(t *testing.T, tool string, args map[string]any) string {
	t.Helper()
	result := s.request(t, "tools/call", map[string]any{"name": tool, "arguments": args})

	var payload struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("decode %s result: %v", tool, err)
	}
	if len(payload.Content) == 0 {
		return ""
	}
	return payload.Content[0].Text
}

func (s *session) request(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	id := s.nextID
	s.nextID++

	s.write(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})

	for {
		message := s.read(t)
		if message.ID == nil {
			continue // a notification from the server
		}
		var got int
		if err := json.Unmarshal(message.ID, &got); err != nil || got != id {
			continue
		}
		if message.Error != nil {
			t.Fatalf("%s failed: %s", method, message.Error.Message)
		}
		return message.Result
	}
}

func (s *session) notify(t *testing.T, method string, params any) {
	t.Helper()
	s.write(t, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// write sends one newline-delimited JSON message.
//
// MCP over stdio is newline-delimited JSON, not the Content-Length framing LSP
// uses — the two protocols are both JSON-RPC and are easy to confuse, and
// jade's server tolerates stray header lines by skipping what it cannot parse,
// so a client that sends the wrong framing appears to work and then hangs
// waiting for a reply in a shape it is not reading.
func (s *session) write(t *testing.T, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.stdin.Write(append(body, '\n')); err != nil {
		t.Fatal(err)
	}
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (s *session) read(t *testing.T) rpcMessage {
	t.Helper()

	for {
		line, err := s.stdout.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var message rpcMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			// Not a protocol frame. The server keeps stdout clean, but a
			// language server it starts could in principle print, and a test
			// that died on that would be reporting the wrong failure.
			continue
		}
		return message
	}
}

// fixtureCopy copies a fixture repository into a temp dir, because the tests
// edit it. Running against the checked-in fixture would leave the working tree
// dirty and make a second run assert against the first run's output.
func fixtureCopy(t *testing.T, name string) string {
	t.Helper()

	source := filepath.Join("repos", name)
	destination := t.TempDir()

	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return destination
}

// jadeBinary locates the jade-mcp binary under test, building it if needed so
// the suite works from a clean checkout.
func jadeBinary(t *testing.T) string {
	t.Helper()

	if path := os.Getenv("JADE_MCP_BINARY"); path != "" {
		return path
	}

	binary := filepath.Join(t.TempDir(), "jade-mcp")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/jade-mcp")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("build jade-mcp: %v", err)
	}
	return binary
}
