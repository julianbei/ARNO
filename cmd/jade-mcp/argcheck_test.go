package main

import (
	"strings"
	"testing"
)

// The reported papercut: replace_text called with old/new rather than
// oldText/newText. Nothing rejected it, so the call ran with empty strings and
// failed later with an error about the anchor — which sends the reader after
// the wrong problem entirely.
func TestMisspelledArgumentNamesTheRealOne(t *testing.T) {
	err := checkArguments("jade.replace_text", map[string]interface{}{
		"path": "greet.go",
		"old":  "a",
		"new":  "b",
	})
	if err == nil {
		t.Fatal("expected a near-miss argument to be reported")
	}
	for _, want := range []string{`unknown argument "old"`, `did you mean "oldText"`, "accepted:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in: %v", want, err)
		}
	}
}

// A correct call must pass untouched — the check sits on every tool call, so a
// false positive here would be worse than the papercut it replaces.
func TestCorrectArgumentsPass(t *testing.T) {
	err := checkArguments("jade.replace_text", map[string]interface{}{
		"path":    "greet.go",
		"oldText": "a",
		"newText": "b",
	})
	if err != nil {
		t.Fatalf("a correct call must not be rejected, got: %v", err)
	}
}

// An unrecognised key that is not a near-miss for a missing required argument
// is left alone. A client attaching its own metadata is not making a typo, and
// rejecting it would break callers to catch a mistake this does not describe.
func TestUnrelatedExtraArgumentIsIgnored(t *testing.T) {
	err := checkArguments("jade.replace_text", map[string]interface{}{
		"path":       "greet.go",
		"oldText":    "a",
		"newText":    "b",
		"_clientTag": "abc",
	})
	if err != nil {
		t.Fatalf("an unrelated extra argument must not fail the call, got: %v", err)
	}
}

// The required argument being present means there is no typo to diagnose, even
// if some other unknown key happens to look similar.
func TestNoComplaintWhenTheRequiredArgumentIsSupplied(t *testing.T) {
	err := checkArguments("jade.replace_text", map[string]interface{}{
		"path":    "greet.go",
		"oldText": "a",
		"newText": "b",
		"old":     "stray",
	})
	if err != nil {
		t.Fatalf("nothing is missing, so nothing should be reported, got: %v", err)
	}
}

func TestUnknownToolIsNotOurProblemHere(t *testing.T) {
	if err := checkArguments("jade.does_not_exist", map[string]interface{}{"x": 1}); err != nil {
		t.Fatalf("dispatch reports unknown tools, not this check, got: %v", err)
	}
}

// Every tool's schema must be readable by the check. A tool whose required
// list or properties could not be parsed would silently opt out of it.
func TestEveryToolSchemaIsReadable(t *testing.T) {
	for _, tool := range tools() {
		properties, required := schemaShape(tool.InputSchema)
		if len(properties) == 0 && len(required) > 0 {
			t.Fatalf("%s declares required args but no readable properties", tool.Name)
		}
		for _, name := range required {
			if !properties[name] {
				t.Fatalf("%s requires %q which is not a declared property", tool.Name, name)
			}
		}
	}
}
