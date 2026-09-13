package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// frozenSurface is the 0.0.1 tool contract: every tool name, and the arguments
// a caller must supply.
//
// This is a golden list on purpose. The MCP catalog is fixed at connection
// time, so a client holds whatever surface it saw when it connected — which
// makes tool names and required arguments a published interface the moment
// anyone integrates, and makes an accidental change invisible until someone
// else's session breaks.
//
// **Changing this list is allowed. Changing it by accident is not.** If a test
// here fails, decide which of these you are doing and say so in the commit:
//
//   - Adding a tool, or making a required argument optional, is backward
//     compatible. Existing callers keep working; new callers need a reconnect
//     before the tool appears at all.
//   - Removing or renaming a tool, or making an optional argument required,
//     breaks callers silently at their next call. That needs a version bump
//     and a note in the release.
//
// A required argument is a promise the server actually enforces. Declaring one
// the implementation ignores is worse than either choice alone: a client that
// trusts the schema sends a value it did not need, and one that does not is
// told it is wrong when it is not. That was true of expectedRevision on
// replace_text and replace_range until this freeze, and is why the lists below
// were checked against behaviour rather than copied from the schema.
var frozenSurface = map[string][]string{
	"jade.apply": {"edits"},
	// Added in 0.0.6; adding a tool is compatible.
	"jade.capabilities":    {},
	"jade.changes":         {},
	"jade.check":           {},
	"jade.checkpoint":      {},
	"jade.context":         {"path"},
	"jade.create_file":     {"content", "path"},
	"jade.declare_command": {"name"},
	"jade.delete_file":     {"path"},
	"jade.delete_symbol":   {"path"},
	"jade.diff":            {},
	"jade.events":          {},
	// query or queries; the server enforces one of them. Made optional in
	// 0.0.3, which is compatible.
	"jade.find": {},
	// query or queries; the server enforces one of them. Made optional in
	// 0.0.4, which is compatible.
	"jade.grep":    {},
	"jade.history": {"path"},
	// Added after the 0.0.1 freeze. Adding a tool is backward compatible:
	// existing callers are unaffected, and new ones need a reconnect before it
	// appears at all, since the MCP catalog is fixed at connection time.
	"jade.insert":     {"path", "text"},
	"jade.job_output": {"id"},
	"jade.job_status": {"id"},
	"jade.outline":    {"path"},
	// path or ranges; the server enforces one of them. Made optional in 0.0.3.
	"jade.read_range":     {},
	"jade.read_symbol":    {"path"},
	"jade.references":     {"path"},
	"jade.rename":         {"newName", "path"},
	"jade.replace_file":   {"content", "path"},
	"jade.replace_range":  {"endLine", "newCode", "path", "startLine"},
	"jade.replace_symbol": {"newCode", "symbolId"},
	"jade.replace_text":   {"newText", "oldText", "path"},
	"jade.repository_map": {"query"},
	"jade.retrieve":       {"query"},
	"jade.revert":         {"checkpointId"},
	"jade.run_command":    {},
	"jade.run_tests":      {},
	"jade.search":         {"query"},
	"jade.search_nudge":   {"command"},
	"jade.telemetry":      {},
	"jade.workspace_tree": {},
}

func TestToolSurfaceMatchesTheFrozenContract(t *testing.T) {
	served := map[string][]string{}
	for _, tool := range tools() {
		served[tool.Name] = requiredArgs(t, tool.InputSchema)
	}

	for name := range served {
		if _, frozen := frozenSurface[name]; !frozen {
			t.Errorf("tool %q is served but not in the frozen contract\n"+
				"  Adding a tool is backward compatible — add it to frozenSurface and note that\n"+
				"  clients need a reconnect before it appears.", name)
		}
	}

	for name := range frozenSurface {
		if _, ok := served[name]; !ok {
			t.Errorf("tool %q is in the frozen contract but no longer served\n"+
				"  This breaks every existing caller at their next call. If it is intended,\n"+
				"  bump the version and say so in the release notes.", name)
		}
	}

	for name, want := range frozenSurface {
		got, ok := served[name]
		if !ok {
			continue
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("tool %q required arguments changed\n  frozen: %v\n  served: %v\n"+
				"  Making an argument optional is compatible; making one required is not.",
				name, want, got)
		}
	}
}

func TestEveryToolDescribesItself(t *testing.T) {
	// A tool with no description is invisible to the model choosing between
	// them, which in practice means it loses to bash.
	for _, tool := range tools() {
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
	}
}

func TestEveryRequiredArgumentIsAlsoDeclaredAsAProperty(t *testing.T) {
	// A required argument absent from properties is unusable: a strict client
	// rejects the call before it is sent, and the schema documents nothing.
	for _, tool := range tools() {
		properties, _ := tool.InputSchema["properties"].(map[string]interface{})
		for _, name := range requiredArgs(t, tool.InputSchema) {
			if _, declared := properties[name]; !declared {
				t.Errorf("tool %q requires %q but does not declare it as a property", tool.Name, name)
			}
		}
	}
}

// pagedTools are the tools on the 0.0.5 budget convention
// (docs/tool-contract.md, "Budgets and provenance"), with the older size
// argument each still accepts, or "" when it had none.
var pagedTools = map[string]string{
	"jade.grep":           "limit",
	"jade.find":           "maxLines",
	"jade.references":     "",
	"jade.read_range":     "",
	"jade.read_symbol":    "maxLines",
	"jade.workspace_tree": "maxEntries",
	"jade.history":        "",
	"jade.diff":           "",
	"jade.job_output":     "",
}

func TestPagedToolsOfferBudgetAndContinue(t *testing.T) {
	served := map[string]map[string]interface{}{}
	for _, tool := range tools() {
		properties, _ := tool.InputSchema["properties"].(map[string]interface{})
		served[tool.Name] = properties
	}
	for name, older := range pagedTools {
		properties, ok := served[name]
		if !ok {
			t.Errorf("paged tool %q is not served", name)
			continue
		}
		for argument, kind := range map[string]string{"budget": "integer", "continue": "string"} {
			property, _ := properties[argument].(map[string]interface{})
			if property == nil || property["type"] != kind {
				t.Errorf("%s must declare %s as %s, got %v", name, argument, kind, property)
			}
		}
		// The older size argument stays accepted through 0.0.x and says what
		// replaces it, so a caller reading the schema moves to budget.
		if older != "" {
			property, _ := properties[older].(map[string]interface{})
			if description, _ := property["description"].(string); !strings.Contains(description, "Prefer budget") {
				t.Errorf("%s.%s should point to budget, got %q", name, older, description)
			}
		}
	}
}

func TestDeprecatedToolsSaySoFirst(t *testing.T) {
	served := map[string]string{}
	for _, tool := range tools() {
		served[tool.Name] = tool.Description
	}
	for name := range deprecatedTools {
		description, ok := served[name]
		if !ok {
			t.Errorf("deprecated tool %q is no longer served: removal belongs before 0.1.0, with a version bump, not silently in 0.0.x", name)
			continue
		}
		if !strings.HasPrefix(description, "Deprecated, removed before 0.1.0: ") {
			t.Errorf("deprecated tool %q should say so first, got %q", name, description)
		}
		if coreProfileTools[name] {
			t.Errorf("deprecated tool %q must not be in the core profile", name)
		}
	}
}

// TestEditContractNamesOnlyTestsThatExist keeps docs/tool-contract.md's edit
// contract honest: every guarantee there names the test that holds it, and a
// renamed or deleted test fails here rather than leaving a guarantee nobody
// checks.
func TestEditContractNamesOnlyTestsThatExist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "tool-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	start := strings.Index(doc, "## The edit contract")
	if start < 0 {
		t.Fatal("docs/tool-contract.md has no edit contract section")
	}
	section := doc[start+len("## The edit contract"):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	named := regexp.MustCompile("`(Test[A-Z][A-Za-z0-9_]*)`").FindAllStringSubmatch(section, -1)
	if len(named) < 20 {
		t.Fatalf("the edit contract names %d tests; expected every guarantee to name one", len(named))
	}

	declared := map[string]bool{}
	function := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	root := filepath.Join("..", "..")
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "repos") {
			return filepath.SkipDir
		}
		if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range function.FindAllStringSubmatch(string(source), -1) {
			declared[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range named {
		if !declared[name[1]] {
			t.Errorf("the edit contract names %s, which no test declares", name[1])
		}
	}
}

func requiredArgs(t *testing.T, schema map[string]interface{}) []string {
	t.Helper()
	raw, ok := schema["required"].([]string)
	if !ok {
		return []string{}
	}
	out := append([]string(nil), raw...)
	sort.Strings(out)
	return out
}
