package main

import (
	"strings"
	"testing"
)

// The Jade names are gone. 0.0.12 and 0.0.13 accepted them and said on stderr
// that they would stop working after 0.0.13, so a client config still saying
// jade.find now gets the same unknown-tool error as any other wrong name —
// which names the arno spelling, so the fix is in the refusal.
func TestJadeToolNamesNoLongerResolve(t *testing.T) {
	for _, name := range []string{"jade.find", "jade_find", "jade.read_range", "jade_apply"} {
		got, err := canonicalToolName(name)
		if err == nil {
			t.Errorf("canonicalToolName(%q) = %q, expected the old name to be gone", name, got)
			continue
		}
		// The refusal has to carry the new name, or a caller left on the old
		// spelling has nothing to act on.
		want := "arno." + strings.TrimPrefix(strings.TrimPrefix(name, "jade."), "jade_")
		if !strings.Contains(err.Error(), want) {
			t.Errorf("canonicalToolName(%q) should suggest %q, got: %v", name, want, err)
		}
	}
}

// The spellings that do resolve: arno.<name> and arno_<name>.
func TestArnoToolNamesResolveInBothSpellings(t *testing.T) {
	for name, want := range map[string]string{
		"arno.find":       "arno.find",
		"arno_find":       "arno.find",
		"arno.read_range": "arno.read_range",
		"arno_apply":      "arno.apply",
	} {
		got, err := canonicalToolName(name)
		if err != nil {
			t.Errorf("canonicalToolName(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("canonicalToolName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestUnknownToolIsStillUnknown(t *testing.T) {
	if _, err := canonicalToolName("jade.nonesuch"); err == nil {
		t.Fatal("expected an unknown tool under the old prefix to stay unknown")
	}
	if _, err := canonicalToolName("arno.nonesuch"); err == nil {
		t.Fatal("expected an unknown tool to stay unknown")
	}
}

// The rename must not leak into what agents read: every advertised name is
// arno.*, in both profiles.
func TestCatalogAdvertisesOnlyArnoNames(t *testing.T) {
	for _, profile := range []string{"core", "all"} {
		for _, tool := range listedTools(profile) {
			if !strings.HasPrefix(tool.Name, "arno.") {
				t.Errorf("%s profile advertises %q", profile, tool.Name)
			}
		}
	}
}
