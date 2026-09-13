// Package commands implements jade's repo command registry: a small,
// version-controlled list of named shell commands a project declares once and
// replays by name thereafter.
//
// Why this exists. jade's check() covers exactly three kinds — build,
// typecheck and tests — so every project-specific command an agent needs
// (vet, lint, codegen, migrate, a docker build) falls off the cliff straight
// back to raw bash. Everything jade provides is lost with it: the pass/fail
// verdict, the decisive-summary extraction, the revision bookkeeping, and any
// telemetry at all. A bash fallback is jade not being in the loop.
//
// The one rule that makes this more than "bash with extra steps": running
// takes a *name*, never a shell string. That single restriction is what buys
// everything else.
//
//   - Enumerable. You can count which commands an agent actually uses, and
//     which were declared once and never touched again.
//   - Reviewable. A command is declared in a file that lives in the repo and
//     shows up in a diff, rather than being synthesized fresh on every call
//     where nobody ever sees it.
//   - Teachable. An unknown name can answer "no command \"lnit\"; declared:
//     build, lint, test" instead of failing as a shell error about a binary
//     that does not exist.
//
// Declaration is therefore the privileged operation and is deliberately kept
// separate from invocation. The shell string is written once, into a file a
// human can read.
package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/julianbei/jade/internal/writes"
)

// Dir and File name the registry's location inside the workspace. It is a
// directory rather than a dotfile at the root because .jade/ is where later
// per-repo jade configuration belongs too.
const (
	Dir  = ".jade"
	File = "commands.json"
)

// RelPath is the registry's path relative to the workspace root, for error
// messages and responses that tell a caller where the file lives.
var RelPath = filepath.Join(Dir, File)

// maxCommands bounds the registry. A repo with hundreds of declared commands
// has stopped using this as a curated list and started using it as a shell
// history, which defeats the point — and the list is rendered in full in
// error messages, so it has to stay readable.
const maxCommands = 64

// maxRunLength bounds a single command string. Long enough for a real
// pipeline, short enough that an entire script is not smuggled into one line
// where nobody will read it.
const maxRunLength = 2000

// namePattern constrains command names to what can be typed unambiguously and
// printed without quoting. Leading character must be alphanumeric so a name
// can never be mistaken for a flag.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:_-]{0,39}$`)

// Command is one declared command.
type Command struct {
	Run string `json:"run"`
	// Description is optional and exists for the agent that did not declare
	// the command: "test" is obvious, "seed" is not.
	Description string `json:"description,omitempty"`
	// Kind makes the command a validation step: lint, codegen, build,
	// typecheck or tests. check runs the lint and codegen commands of its kind.
	Kind string `json:"kind,omitempty"`
}

// Registry is a workspace's declared commands, loaded from disk.
type Registry struct {
	root     string
	commands map[string]Command
}

// Load reads the registry for the workspace rooted at root.
//
// A missing file is not an error and yields an empty registry. Most repos
// will never have one, and jade has to work in a repo that has not opted in —
// the same "degrade, don't fail" rule the ecosystem discovery follows. A file
// that exists but does not parse *is* an error: silently treating a
// malformed registry as empty would report "no command \"build\"" for a
// command the caller can plainly see declared in the file.
func Load(root string) (*Registry, error) {
	registry := &Registry{root: root, commands: map[string]Command{}}

	data, err := os.ReadFile(filepath.Join(root, RelPath))
	if os.IsNotExist(err) {
		return registry, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &registry.commands); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", RelPath, err)
	}
	for name := range registry.commands {
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("%s: %w", RelPath, err)
		}
	}
	return registry, nil
}

// Names returns the declared command names in sorted order. Sorted rather
// than insertion-ordered so the list is stable across calls: an unstable list
// makes a response undiffable and a re-read look like a change.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns every declared command, sorted by name.
func (r *Registry) All() []Declared {
	names := r.Names()
	out := make([]Declared, 0, len(names))
	for _, name := range names {
		out = append(out, Declared{Name: name, Command: r.commands[name]})
	}
	return out
}

// Declared pairs a name with its command, for listing.
type Declared struct {
	Name string
	Command
}

// Lookup returns the command declared under name.
func (r *Registry) Lookup(name string) (Command, bool) {
	command, ok := r.commands[strings.TrimSpace(name)]
	return command, ok
}

// Declare adds or replaces a command and writes the registry to disk. The
// bool reports whether an existing command was replaced, which the caller
// surfaces: silently overwriting a command another agent declared is exactly
// the kind of change that should be visible in the response, not just in the
// git diff.
func (r *Registry) Declare(name string, command Command) (replaced bool, err error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return false, err
	}

	command.Run = strings.TrimSpace(command.Run)
	command.Description = strings.TrimSpace(command.Description)
	if command.Run == "" {
		return false, fmt.Errorf("command %q needs a run string", name)
	}
	if len(command.Run) > maxRunLength {
		return false, fmt.Errorf("command %q is %d characters, over the %d limit — put a script in the repo and call it instead", name, len(command.Run), maxRunLength)
	}

	_, replaced = r.commands[name]
	if !replaced && len(r.commands) >= maxCommands {
		return false, fmt.Errorf("%s already holds %d commands — remove one before declaring another", RelPath, maxCommands)
	}

	r.commands[name] = command
	if err := r.save(); err != nil {
		// Roll the in-memory change back so a failed write cannot leave this
		// Registry claiming a command that is not on disk.
		if replaced {
			// The prior value is gone; reloading is the only honest recovery.
			if reloaded, loadErr := Load(r.root); loadErr == nil {
				r.commands = reloaded.commands
			}
		} else {
			delete(r.commands, name)
		}
		return false, err
	}
	return replaced, nil
}

// Remove deletes a command and rewrites the registry. Reports whether the
// command existed; removing something already absent is not an error, since
// the caller's intent — "this command should not be declared" — is satisfied
// either way.
func (r *Registry) Remove(name string) (bool, error) {
	name = strings.TrimSpace(name)
	command, existed := r.commands[name]
	if !existed {
		return false, nil
	}

	delete(r.commands, name)
	if err := r.save(); err != nil {
		r.commands[name] = command
		return false, err
	}
	return true, nil
}

// save writes the registry as indented JSON. Indented and newline-terminated
// on purpose: this file is meant to be read and diffed by humans, and a
// single-line blob would make every change look like a full rewrite.
func (r *Registry) save() error {
	if err := os.MkdirAll(filepath.Join(r.root, Dir), 0o755); err != nil {
		return err
	}

	// json.Marshal sorts map keys, so the file's ordering is stable without
	// any extra work here.
	data, err := json.MarshalIndent(r.commands, "", "  ")
	if err != nil {
		return err
	}
	// Through the write path like any edit: atomic, and seen by checkpoints,
	// so reverting past a declare_command restores the registry too.
	return writes.File(filepath.Join(r.root, RelPath), append(data, '\n'))
}

// ValidateName rejects names that cannot be typed or printed unambiguously.
// The error names the rule rather than just saying "invalid", so a caller
// that got it wrong can fix it without guessing.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("command name is required")
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid command name %q — use lowercase letters, digits, ':', '_' or '-', starting with a letter or digit, up to 40 characters", name)
	}
	return nil
}

// UnknownCommandError builds the teachable error for a name that is not
// declared. Listing what *is* available is the whole point: a bare "unknown
// command" sends the caller back to bash, whereas the list usually contains
// the thing they meant.
func UnknownCommandError(name string, available []string) error {
	if len(available) == 0 {
		return fmt.Errorf("no command %q, and none are declared — declare one first (writes %s)", name, RelPath)
	}
	return fmt.Errorf("no command %q; declared: %s", name, strings.Join(available, ", "))
}
