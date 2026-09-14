package main

import (
	"encoding/json"
	"testing"
)

func TestCoreProfileListsOnlyRealToolsAndIsSmaller(t *testing.T) {
	core := listedTools("core")
	if len(core) != len(coreProfileTools) {
		names := map[string]bool{}
		for _, tool := range core {
			names[tool.Name] = true
		}
		for name := range coreProfileTools {
			if !names[name] {
				t.Errorf("core profile names %s, which is not a tool", name)
			}
		}
	}

	full, _ := json.Marshal(listedTools("all"))
	lean, _ := json.Marshal(core)
	if len(lean) >= len(full) {
		t.Errorf("core catalog is %d bytes, the full one %d; core must be smaller", len(lean), len(full))
	}
	// The host resends the tool list on every turn, so core's size is paid per
	// turn. It was a ratio, under half the full catalog, until 0.0.8 removed
	// five tools from the full one; the absolute size is what an agent pays.
	// 12.4 KB on 2026-09-14, with budget, continue and expectedDigest.
	if len(lean) > maxCoreCatalogBytes {
		t.Errorf("core catalog is %d bytes, over its %d-byte ceiling; a schema grew", len(lean), maxCoreCatalogBytes)
	}
}

// maxCoreCatalogBytes is the core profile's tools/list size ceiling.
const maxCoreCatalogBytes = 13000

func TestToolsFlag(t *testing.T) {
	cases := []struct {
		args    []string
		want    string
		wantErr bool
	}{
		{nil, "all", false},
		{[]string{"--root", "/repo", "--tools", "core"}, "core", false},
		{[]string{"--tools=all"}, "all", false},
		{[]string{"--tools"}, "", true},
		{[]string{"--tools=lean"}, "", true},
	}
	for _, tc := range cases {
		got, err := toolsFlag(tc.args)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%v: got %q, %v", tc.args, got, err)
		}
	}
}

func TestCoreProfileStillServesUnlistedTools(t *testing.T) {
	server, _ := newTestMCPServer(t)
	server.profile = "core"
	if _, err := callText(t, server, "jade.changes", map[string]interface{}{}); err != nil {
		t.Fatalf("an unlisted tool must stay callable: %v", err)
	}
}
