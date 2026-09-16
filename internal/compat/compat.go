// Package compat keeps the names Jade used working for one release after the
// rename to ARNO. Everything here is deleted in the release after 0.0.12; it
// exists so a tester's shell profile, MCP client config and checked-in
// .jade/commands.json do not break the moment they update.
package compat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LegacyDir is the workspace directory Jade wrote. ARNO writes ArnoDir.
const (
	LegacyDir = ".jade"
	ArnoDir   = ".arno"
)

// legacyEnvPrefix and arnoEnvPrefix name the two environment prefixes.
const (
	legacyEnvPrefix = "JADE_"
	arnoEnvPrefix   = "ARNO_"
)

var (
	warnedMu sync.Mutex
	warned   = map[string]bool{}
)

// warn prints one deprecation line per distinct old name, to stderr, which is
// where an MCP server's diagnostics belong: stdout carries the protocol.
func warn(format string, args ...any) {
	key := fmt.Sprintf(format, args...)
	warnedMu.Lock()
	defer warnedMu.Unlock()
	if warned[key] {
		return
	}
	warned[key] = true
	fmt.Fprintln(os.Stderr, "arno: "+key)
}

// Getenv reads an ARNO_ variable, falling back to its JADE_ spelling. The new
// name always wins, so a machine carrying both is not surprised by the old
// one.
func Getenv(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	legacy, ok := strings.CutPrefix(name, arnoEnvPrefix)
	if !ok {
		return ""
	}
	legacyName := legacyEnvPrefix + legacy
	value := os.Getenv(legacyName)
	if value != "" {
		warn("%s is deprecated and will stop working after 0.0.12; use %s", legacyName, name)
	}
	return value
}

// StatePath is where a state file lives inside a workspace: .arno/<file>, or
// the .jade/ one while that is the only copy. The returned path is absolute.
func StatePath(root, file string) string {
	current := filepath.Join(root, ArnoDir, file)
	if _, err := os.Stat(current); err == nil {
		return current
	}
	legacy := filepath.Join(root, LegacyDir, file)
	if _, err := os.Stat(legacy); err == nil {
		warn("reading %s; Jade is now ARNO, so rename the directory: git mv %s %s", filepath.Join(LegacyDir, file), LegacyDir, ArnoDir)
		return legacy
	}
	return current
}

// ToolName maps a tool name Jade served to ARNO's. It returns "" for anything
// that is not an old name, so callers can tell a rename from a typo.
func ToolName(name string) string {
	for _, prefix := range []string{"jade.", "jade_"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" {
			warn("%s is deprecated and will stop working after 0.0.12; use arno.%s", name, rest)
			return "arno." + rest
		}
	}
	return ""
}
