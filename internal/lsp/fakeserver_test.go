package lsp

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// runFakeServer is a language server that misbehaves on purpose.
//
// Each script reproduces one failure ARNO has to survive. They are written
// here rather than mocked at the Client boundary because the behaviours worth
// testing live in the framing and the read loop — a mock that returned canned
// structs would assert nothing about either.
// fakeIndexingStep spaces the "indexing" script's progress messages. The whole
// run is three steps, long enough past settleGrace to tell a client that waits
// for `end` from one that merely waits out the grace period.
const fakeIndexingStep = 1200 * time.Millisecond

func runFakeServer(script string) {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout

	for {
		message, err := readMessage(in)
		if err != nil {
			return
		}

		switch message.Method {
		case "textDocument/diagnostic":
			if script == "pull" {
				report, _ := json.Marshal(map[string]any{
					"kind": "full",
					"items": []Diagnostic{{
						Range:    Range{Start: Position{Line: 2, Character: 4}},
						Severity: SeverityError,
						Message:  "pulled, not published",
					}},
				})
				_ = writeMessage(out, Message{ID: message.ID, Result: report})
				continue
			}
			_ = writeMessage(out, Message{ID: message.ID, Error: &ResponseError{Code: -32601, Message: "no pull"}})

		case "initialize":
			capabilities := map[string]any{
				"referencesProvider": true,
				// An options object rather than a boolean: real servers do
				// this, and a client that only accepts booleans decides the
				// feature is missing.
				"renameProvider":     map[string]any{"prepareProvider": false},
				"codeLensProvider":   false,
				"hoverProvider":      true,
				"definitionProvider": true,
			}
			if script == "pull" {
				capabilities["diagnosticProvider"] = map[string]any{"interFileDependencies": false}
				capabilities["textDocumentSync"] = map[string]any{"change": 1, "save": map[string]any{"includeText": false}}
			}
			if script == "settling" {
				capabilities["textDocumentSync"] = 1
			}
			result, _ := json.Marshal(map[string]any{"capabilities": capabilities})
			_ = writeMessage(out, Message{ID: message.ID, Result: result})

		case "initialized":
			// Notification: no reply.
			if script == "indexing" {
				// Index the way ruby-lsp does: announce, work, finish. Written
				// from a goroutine so the read loop keeps serving meanwhile —
				// a real server answers requests while indexing, wrongly.
				go func() {
					token := json.RawMessage(`"indexing"`)
					for _, kind := range []string{"begin", "report", "end"} {
						params, _ := json.Marshal(map[string]any{"token": token, "value": map[string]any{"kind": kind}})
						_ = writeMessage(out, Message{Method: "$/progress", Params: params})
						time.Sleep(fakeIndexingStep)
					}
				}()
			}

		case "shutdown":
			_ = writeMessage(out, Message{ID: message.ID, Result: json.RawMessage("null")})

		case "exit":
			os.Exit(0)

		case "textDocument/didOpen":
			if script == "settling" {
				// metals' shape: an empty publish first, the real one after.
				var params DidOpenTextDocumentParams
				_ = json.Unmarshal(message.Params, &params)
				empty, _ := json.Marshal(PublishDiagnosticsParams{URI: params.TextDocument.URI, Diagnostics: []Diagnostic{}})
				_ = writeMessage(out, Message{Method: "textDocument/publishDiagnostics", Params: empty})
				time.Sleep(150 * time.Millisecond)
				real, _ := json.Marshal(PublishDiagnosticsParams{
					URI:         params.TextDocument.URI,
					Diagnostics: []Diagnostic{{Severity: SeverityError, Message: "the real answer"}},
				})
				_ = writeMessage(out, Message{Method: "textDocument/publishDiagnostics", Params: real})
			}
			if script == "diagnostics" {
				var params DidOpenTextDocumentParams
				_ = json.Unmarshal(message.Params, &params)
				publish, _ := json.Marshal(PublishDiagnosticsParams{
					URI: params.TextDocument.URI,
					Diagnostics: []Diagnostic{{
						Range:    Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 1}},
						Severity: SeverityError,
						Source:   "fake",
						Message:  "something is wrong",
					}},
				})
				_ = writeMessage(out, Message{
					Method: "textDocument/publishDiagnostics",
					Params: publish,
				})
			}

		case "custom/didYouAnswerMe":
			// Ask ARNO something first, and only answer once ARNO answers.
			// A client that ignores server-to-client requests never gets past
			// this point, which is exactly the deadlock being tested for.
			askParams, _ := json.Marshal(map[string]any{
				"items": []map[string]any{{"section": "fake"}},
			})
			askID, _ := json.Marshal(9001)
			_ = writeMessage(out, Message{ID: askID, Method: "workspace/configuration", Params: askParams})

			answered := false
			for {
				reply, err := readMessage(in)
				if err != nil {
					return
				}
				if string(reply.ID) == string(askID) {
					answered = true
					break
				}
			}
			result, _ := json.Marshal(map[string]any{"answered": answered})
			_ = writeMessage(out, Message{ID: message.ID, Result: result})

		default:
			switch script {
			case "hang":
				// Read the request and never answer it.
				continue
			case "die":
				os.Exit(1)
			default:
				empty, _ := json.Marshal([]Location{})
				_ = writeMessage(out, Message{ID: message.ID, Result: empty})
			}
		}

		if script == "slowstart" {
			time.Sleep(50 * time.Millisecond)
		}
	}
}
