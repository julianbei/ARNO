package main

import (
	"fmt"
	"sort"
	"strings"
)

// checkArguments catches a caller who spelled an argument name almost right.
//
// Reported from use: replace_text called with `old`/`new` instead of
// `oldText`/`newText`. Nothing rejected it — the misspelled keys were simply
// never read, so the call proceeded with empty strings and failed further in
// with an error about the anchor, which points at the wrong thing entirely.
//
// The check is deliberately narrow. It fires only when a *required* argument
// is missing AND an unrecognised key is a near-miss for it, which is the
// signature of a typo rather than of a client attaching its own metadata.
// Rejecting every unknown key would be the obvious implementation and the
// wrong one: it would break callers that pass harmless extras, to catch a
// mistake this narrower rule already catches.
func checkArguments(toolName string, args map[string]interface{}) error {
	tool, ok := toolByName(toolName)
	if !ok {
		return nil
	}

	properties, required := schemaShape(tool.InputSchema)
	if len(properties) == 0 {
		return nil
	}

	unknown := make([]string, 0)
	for key := range args {
		if _, declared := properties[key]; !declared {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	for _, name := range required {
		if _, supplied := args[name]; supplied {
			continue
		}
		for _, key := range unknown {
			if isNearMiss(key, name) {
				return fmt.Errorf("unknown argument %q for %s — did you mean %q? (accepted: %s)",
					key, toolName, name, strings.Join(sortedKeys(properties), ", "))
			}
		}
	}
	return nil
}

// isNearMiss reports whether supplied looks like an attempt at declared.
//
// A prefix relationship covers the reported case and its siblings (old/oldText,
// new/newText, name/symbolName, id/symbolId) without the false positives an
// edit-distance threshold brings at these short lengths — "path" and "name"
// are two edits apart but are not each other's typo.
func isNearMiss(supplied string, declared string) bool {
	a := strings.ToLower(supplied)
	b := strings.ToLower(declared)
	if a == b {
		return false
	}
	return strings.HasPrefix(b, a) || strings.HasSuffix(b, a)
}

// schemaShape pulls the declared property names and the required list out of a
// tool's JSON schema. Both are built as map[string]interface{} literals in
// tools(), so the assertions mirror how they are written rather than assuming
// a decoded-JSON shape.
func schemaShape(schema map[string]interface{}) (map[string]bool, []string) {
	properties := make(map[string]bool)
	if raw, ok := schema["properties"].(map[string]interface{}); ok {
		for name := range raw {
			properties[name] = true
		}
	}

	var required []string
	switch list := schema["required"].(type) {
	case []string:
		required = list
	case []interface{}:
		for _, item := range list {
			if name, ok := item.(string); ok {
				required = append(required, name)
			}
		}
	}
	return properties, required
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func toolByName(name string) (mcpTool, bool) {
	for _, tool := range tools() {
		if tool.Name == name {
			return tool, true
		}
	}
	return mcpTool{}, false
}
