package lsp

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// runFakeServer is a language server that misbehaves on purpose.
//
// Each script reproduces one failure jade has to survive. They are written
// here rather than mocked at the Client boundary because the behaviours worth
// testing live in the framing and the read loop — a mock that returned canned
// structs would assert nothing about either.
func runFakeServer(script string) {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout

	for {
		message, err := readMessage(in)
		if err != nil {
			return
		}

		switch message.Method {
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
			result, _ := json.Marshal(map[string]any{"capabilities": capabilities})
			_ = writeMessage(out, Message{ID: message.ID, Result: result})

		case "initialized":
			// Notification: no reply.

		case "shutdown":
			_ = writeMessage(out, Message{ID: message.ID, Result: json.RawMessage("null")})

		case "exit":
			os.Exit(0)

		case "textDocument/didOpen":
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
			// Ask jade something first, and only answer once jade answers.
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
