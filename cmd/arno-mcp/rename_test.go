package main

import (
	"strings"
	"testing"
)

// A tester's client config, and any agent that learned the name before the
// rename, still says jade.find. Both spellings resolve until the release
// after 0.0.12; the catalog only ever advertises arno.*.
func TestJadeToolNamesStillResolve(t *testing.T) {
	cases := map[string]string{
		"jade.find":       "arno.find",
		"jade_find":       "arno.find",
		"jade.read_range": "arno.read_range",
		"jade_apply":      "arno.apply",
		"arno.find":       "arno.find",
		"arno_find":       "arno.find",
	}
	for name, want := range cases {
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
