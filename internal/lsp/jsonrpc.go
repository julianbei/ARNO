// Package lsp speaks the Language Server Protocol to real language servers.
//
// Until this package, arno's only semantic tooling was three `gopls` CLI
// subcommands, which meant `references` and `rename` were exact for Go and
// approximate-or-refused everywhere else. A CLI invocation also pays full
// process startup and workspace load on every call, and can answer only the
// questions that happen to have a subcommand.
//
// The protocol is deliberately implemented here rather than pulled in: the
// client needs maybe six requests and four notifications, and the failure
// behaviour arno wants — never block, never crash the server on a bad reply,
// degrade to the existing fallbacks when a server is missing — is most of the
// work regardless of who writes the framing.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
)

// Message is one JSON-RPC 2.0 frame in either direction. Request, response
// and notification share a struct because the protocol distinguishes them by
// which fields are present, and a single decode target avoids guessing before
// reading.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

// ResponseError is a server-reported failure. It is data, not a transport
// problem: a server that answers "no such symbol" is working correctly.
type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("lsp error %d: %s", e.Code, e.Message)
}

// writeMessage frames one message with the Content-Length header LSP requires.
// Note this is the same framing MCP uses over stdio, which is not a
// coincidence — both are JSON-RPC over a byte stream with no message
// boundaries of its own.
func writeMessage(w io.Writer, message Message) error {
	message.JSONRPC = "2.0"
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// readMessage reads one framed message.
//
// The header block is parsed with textproto rather than by scanning for a
// blank line, because servers vary in what they send alongside Content-Length
// (Content-Type in particular) and a hand-rolled reader that assumes exactly
// one header works until it meets one that does not.
func readMessage(r *bufio.Reader) (Message, error) {
	headers, err := textproto.NewReader(r).ReadMIMEHeader()
	if err != nil {
		return Message{}, err
	}

	raw := headers.Get("Content-Length")
	if raw == "" {
		return Message{}, fmt.Errorf("lsp frame has no Content-Length")
	}
	length, err := strconv.Atoi(raw)
	if err != nil {
		return Message{}, fmt.Errorf("lsp frame has an unreadable Content-Length %q", raw)
	}
	if length < 0 || length > maxFrameBytes {
		return Message{}, fmt.Errorf("lsp frame length %d is out of range", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Message{}, err
	}

	var message Message
	if err := json.Unmarshal(payload, &message); err != nil {
		return Message{}, fmt.Errorf("lsp frame is not valid JSON: %w", err)
	}
	return message, nil
}

// maxFrameBytes bounds a single frame. A language server that has decided to
// send arno 64MB of anything has gone wrong in a way that reading it all
// would only make worse — rust-analyzer's workspace notifications and
// jdtls's progress reports can both get large.
const maxFrameBytes = 64 << 20
