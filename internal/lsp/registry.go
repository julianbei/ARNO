package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ServerSpec describes how to launch one language server.
type ServerSpec struct {
	// Language is arno's own identifier, matching internal/code's grammar
	// names so a file's language maps to a server without a second table.
	Language string

	Command string
	Args    []string

	// Alternatives are other binaries providing the same language, tried in
	// order when Command is absent. Ecosystems rarely settle on one server —
	// Python has pyright and pylsp, Ruby has ruby-lsp and solargraph — and
	// insisting on a favourite means arno goes semantic-blind on a machine
	// that has the other one installed.
	Alternatives []AlternativeSpec

	// InitializationOptions are passed through at initialize. Some servers
	// are close to useless without them.
	InitializationOptions map[string]any

	// LanguageID is what the server expects in didOpen. It is usually but not
	// always the same as Language: LSP's identifier for C# is "csharp", and
	// for TSX it is "typescriptreact".
	LanguageID string

	// DiagnosticsAfterCompile marks a server whose diagnostics for a change
	// exist only once it has compiled that change. metals publishes an empty
	// list on save and the real errors after compiling, sometimes a minute
	// later while a build import runs — with no progress reported in between.
	// For such a server only a publish that follows a compile counts.
	DiagnosticsAfterCompile bool

	// AnswerPrompt decides how to answer a window/showMessageRequest: return
	// the title of the action to take, or "" to decline. Nil declines
	// everything. Prompts are how servers ask permission for side effects —
	// metals asks before running sbt and writing .bloop/ and .metals/ into the
	// repository — so the default is always no.
	AnswerPrompt func(message string, actions []string) string
}

// MetalsImportEnv opts in to letting metals import an sbt build. Importing
// runs `sbt bloopInstall` and creates .bloop/ and .metals/ in the workspace;
// without it metals cannot compile, so Scala edits report "not checked".
const MetalsImportEnv = "ARNO_METALS_IMPORT"

// answerMetalsPrompt accepts metals' build-import prompt when, and only when,
// the user opted in. Every other metals prompt is declined.
func answerMetalsPrompt(message string, actions []string) string {
	if os.Getenv(MetalsImportEnv) != "1" || !strings.Contains(strings.ToLower(message), "import the build") {
		return ""
	}
	for _, action := range actions {
		if action == "Import build" {
			return action
		}
	}
	return ""
}

// AlternativeSpec is a second choice of binary for the same language.
type AlternativeSpec struct {
	Command string
	Args    []string
}

// specs is the server table, keyed by arno's language identifier.
//
// Every entry is a server that speaks LSP over stdio and needs no
// configuration file to be useful. Servers requiring a project-specific setup
// step (jdtls wants a data directory, metals wants a build import) are still
// listed, because a machine that has them configured should get the benefit,
// and one that does not simply fails to start and falls back.
var specs = map[string]ServerSpec{
	"go": {
		Language:   "go",
		Command:    "gopls",
		LanguageID: "go",
	},
	"typescript": {
		Language:   "typescript",
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		LanguageID: "typescript",
	},
	"tsx": {
		Language:   "tsx",
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		LanguageID: "typescriptreact",
	},
	"javascript": {
		Language:   "javascript",
		Command:    "typescript-language-server",
		Args:       []string{"--stdio"},
		LanguageID: "javascript",
	},
	"rust": {
		Language:   "rust",
		Command:    "rust-analyzer",
		LanguageID: "rust",
	},
	"python": {
		Language:   "python",
		Command:    "pyright-langserver",
		Args:       []string{"--stdio"},
		LanguageID: "python",
		Alternatives: []AlternativeSpec{
			{Command: "pylsp"},
			{Command: "jedi-language-server"},
		},
	},
	"ruby": {
		Language:   "ruby",
		Command:    "ruby-lsp",
		LanguageID: "ruby",
		Alternatives: []AlternativeSpec{
			{Command: "solargraph", Args: []string{"stdio"}},
		},
	},
	"java": {
		Language:   "java",
		Command:    "jdtls",
		LanguageID: "java",
	},
	"scala": {
		Language:                "scala",
		Command:                 "metals",
		LanguageID:              "scala",
		DiagnosticsAfterCompile: true,
		AnswerPrompt:            answerMetalsPrompt,
	},
}

// SpecFor returns the server spec for a arno language identifier.
func SpecFor(language string) (ServerSpec, bool) {
	spec, ok := specs[language]
	return spec, ok
}

// Languages lists every language with a configured server, for reporting.
func Languages() []string {
	out := make([]string, 0, len(specs))
	for name := range specs {
		out = append(out, name)
	}
	return out
}

// Locate finds the binary to run, preferring Command and falling back through
// Alternatives. The returned spec's Args belong to whichever was found, which
// matters because the alternatives do not share a command line: solargraph
// needs `stdio` and ruby-lsp does not.
func (s ServerSpec) Locate() (string, bool) {
	if path, ok := lookPath(s.Command); ok {
		return path, true
	}
	for _, alternative := range s.Alternatives {
		if path, ok := lookPath(alternative.Command); ok {
			return path, true
		}
	}
	return "", false
}

// Resolve returns the spec as it should actually be launched, with the args of
// whichever binary was found. Calling Locate alone and reusing the original
// args is the bug this exists to prevent.
func (s ServerSpec) Resolve() (ServerSpec, bool) {
	if _, ok := lookPath(s.Command); ok {
		return s, true
	}
	for _, alternative := range s.Alternatives {
		if _, ok := lookPath(alternative.Command); ok {
			resolved := s
			resolved.Command = alternative.Command
			resolved.Args = alternative.Args
			return resolved, true
		}
	}
	return s, false
}

// Fallbacks returns launchable specs for every installed alternative other
// than the one Resolve picks, in preference order.
//
// Resolve answers "which server do I start"; this answers "which server do I
// ask when that one declines". They are different questions because a
// server can be installed, running and still unable to do one operation:
// ruby-lsp 0.26 renames classes and modules but returns null for a method,
// which solargraph renames correctly.
func (s ServerSpec) Fallbacks() []ServerSpec {
	primary, found := s.Resolve()
	if !found {
		return nil
	}
	var out []ServerSpec
	for _, alternative := range s.Alternatives {
		if alternative.Command == primary.Command {
			continue
		}
		if _, ok := lookPath(alternative.Command); !ok {
			continue
		}
		fallback := s
		fallback.Command = alternative.Command
		fallback.Args = alternative.Args
		fallback.Alternatives = nil
		out = append(out, fallback)
	}
	return out
}

// lookPath finds an executable, searching the usual language-toolchain
// directories in addition to PATH.
//
// This is not over-engineering: gopls is installed by `go install` into
// ~/go/bin, which is not on PATH by default — on the machine arno was
// developed on, `command -v gopls` finds nothing while gopls is installed and
// working. A client that only consulted PATH would report Go as having no
// language server on the very system that has one.
func lookPath(command string) (string, bool) {
	if command == "" {
		return "", false
	}
	if path, err := exec.LookPath(command); err == nil {
		return path, true
	}

	for _, dir := range extraBinDirs() {
		candidate := filepath.Join(dir, executableName(command))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && isExecutable(info) {
			return candidate, true
		}
	}
	return "", false
}

// extraBinDirs lists toolchain install locations that are commonly not on
// PATH.
func extraBinDirs() []string {
	dirs := make([]string, 0, 8)

	if gobin := os.Getenv("GOBIN"); gobin != "" {
		dirs = append(dirs, gobin)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, entry := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(entry, "bin"))
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, "go", "bin"),        // go install default
			filepath.Join(home, ".cargo", "bin"),    // rustup
			filepath.Join(home, ".local", "bin"),    // pip --user, pipx
			filepath.Join(home, ".coursier", "bin"), // coursier, for metals
		)
	}
	return dirs
}

func executableName(command string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(command, ".exe") {
		return command + ".exe"
	}
	return command
}

func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}
