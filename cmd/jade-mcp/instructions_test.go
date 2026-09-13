package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The instructions name tools for a host to preload. A renamed or removed tool
// left in that list sends every session looking for something that does not
// exist — the exact turn the list is meant to save.
func TestCoreToolsAreRealAndNamedInInstructions(t *testing.T) {
	known := knownToolNames()
	for _, name := range coreTools {
		if !known[name] {
			t.Errorf("core tool %s is not in the catalog", name)
		}
		if !strings.Contains(serverInstructions, name) {
			t.Errorf("core tool %s is not named in the server instructions", name)
		}
	}
}

// Instructions are paid for by every session.
func TestInstructionsStayShort(t *testing.T) {
	if len(serverInstructions) > 600 {
		t.Fatalf("server instructions are %d bytes; keep them under 600", len(serverInstructions))
	}
}

func TestSingleEditToolsPointAtApply(t *testing.T) {
	for _, tool := range tools() {
		if tool.Name == "jade.replace_text" || tool.Name == "jade.insert" {
			if !strings.Contains(tool.Description, "apply") {
				t.Errorf("%s does not mention apply; agents making many single edits never find it", tool.Name)
			}
		}
	}
}

func TestInitializeReportsInstructionsAndRealVersion(t *testing.T) {
	server, _ := newTestMCPServer(t)
	response := server.handleRequest(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize"})

	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Instructions string `json:"instructions"`
		ServerInfo   struct {
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.Instructions != serverInstructions {
		t.Fatalf("initialize did not send the instructions, got %q", result.Instructions)
	}
	if result.ServerInfo.Version != resolveVersion() {
		t.Fatalf("serverInfo.version %q does not match the binary's version %q", result.ServerInfo.Version, resolveVersion())
	}
}
