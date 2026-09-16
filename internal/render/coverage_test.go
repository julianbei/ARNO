package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

// renderedResponses is every protocol response type that has a renderer.
// The test below cross-checks this list against the protocol package's
// actual declarations, so adding a new *Response type without a renderer
// fails the build rather than silently shipping as raw JSON.
var renderedResponses = map[string]interface{}{
	"CapabilitiesResponse":   protocol.CapabilitiesResponse{},
	"InspectResponse":        protocol.InspectResponse{},
	"SearchResponse":         protocol.SearchResponse{},
	"SearchNudgeResponse":    protocol.SearchNudgeResponse{},
	"ReferencesResponse":     protocol.ReferencesResponse{},
	"ChangesResponse":        protocol.ChangesResponse{},
	"WorkspaceTreeResponse":  protocol.WorkspaceTreeResponse{},
	"DiffResponse":           protocol.DiffResponse{},
	"EditResponse":           protocol.EditResponse{},
	"JobStatusResponse":      protocol.JobStatusResponse{},
	"JobOutputResponse":      protocol.JobOutputResponse{},
	"RunTestsResponse":       protocol.RunTestsResponse{},
	"RepositoryMapResponse":  protocol.RepositoryMapResponse{},
	"RetrievalResponse":      protocol.RetrievalResponse{},
	"EventsResponse":         protocol.EventsResponse{},
	"CheckpointResponse":     protocol.CheckpointResponse{},
	"ContextResponse":        protocol.ContextResponse{},
	"HistoryResponse":        protocol.HistoryResponse{},
	"CheckResponse":          protocol.CheckResponse{},
	"GrepResponse":           protocol.GrepResponse{},
	"GrepBatchResponse":      protocol.GrepBatchResponse{},
	"TelemetryResponse":      protocol.TelemetryResponse{},
	"RunCommandResponse":     protocol.RunCommandResponse{},
	"DeclareCommandResponse": protocol.DeclareCommandResponse{},
	"ApplyResponse":          protocol.ApplyResponse{},
	"FindResponse":           protocol.FindResponse{},
}

// TestEveryResponseTypeHasARenderer is the acceptance bar 10.6 asks for,
// enforced rather than documented. The 8.3 benchmark showed an unrendered
// response costs roughly 10x its rendered form, so a new tool that forgets a
// renderer is a measurable regression, not a cosmetic one.
func TestEveryResponseTypeHasARenderer(t *testing.T) {
	declared := declaredResponseTypes(t)

	missing := make([]string, 0)
	for _, name := range declared {
		if _, ok := renderedResponses[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("protocol response types with no renderer: %s\n"+
			"Add a case to render.Text and an entry to renderedResponses.",
			strings.Join(missing, ", "))
	}

	// The reverse direction too: a stale entry here would let the guard
	// claim coverage for a type that no longer exists.
	declaredSet := make(map[string]bool, len(declared))
	for _, name := range declared {
		declaredSet[name] = true
	}
	for name := range renderedResponses {
		if !declaredSet[name] {
			t.Fatalf("renderedResponses lists %s, which protocol no longer declares", name)
		}
	}
}

func TestEveryRenderedResponseActuallyRenders(t *testing.T) {
	for name, value := range renderedResponses {
		out, ok := Text(value)
		if !ok {
			t.Fatalf("%s is listed as rendered but Text() does not handle it", name)
		}
		// A zero-valued response must still say something. Returning an
		// empty string would leave the agent with no output at all and no
		// way to tell success from a dropped response.
		if strings.TrimSpace(out) == "" {
			t.Fatalf("%s rendered to nothing for its zero value", name)
		}
	}
}

// declaredResponseTypes parses the protocol package for every exported type
// named *Response. Parsing the source rather than maintaining a second hand
// written list is the point: the guard must notice types nobody remembered
// to tell it about.
func declaredResponseTypes(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("..", "protocol", "types.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	names := make([]string, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || !spec.Name.IsExported() {
			return true
		}
		if !strings.HasSuffix(spec.Name.Name, "Response") {
			return true
		}
		if _, isStruct := spec.Type.(*ast.StructType); !isStruct {
			return true
		}
		names = append(names, spec.Name.Name)
		return true
	})

	if len(names) == 0 {
		t.Fatalf("found no response types in %s — the guard would pass vacuously", path)
	}
	sort.Strings(names)
	return names
}
