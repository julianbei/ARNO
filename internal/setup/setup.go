// Package setup installs what Jade can use but does not ship — language
// servers today, plugins later — behind `jade-mcp install`.
//
// Nothing runs without being shown first: every component lists the exact
// command it would run, a menu asks which to install and confirms once, and a
// component with no installer on the machine gets manual instructions instead
// of a guess.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/julianbei/jade/internal/lsp"
)

// Component is one thing Jade can install.
type Component struct {
	// Key selects it on the command line: go, java, scala…
	Key string
	// Kind groups the menu. Plugins will be a second kind.
	Kind string
	Name string
	// For is who it serves, as the menu shows it.
	For string
	// Languages are the Jade language identifiers it provides a server for.
	Languages []string
	// Detect reports where it is installed.
	Detect func() (string, bool)
	// Recipes are tried in order; the first whose Needs is on the machine is
	// the one offered.
	Recipes []Recipe
	// Manual says how to install it by hand when no recipe fits.
	Manual string
	// Note is shown after installing: a prerequisite or an opt-in.
	Note string
}

// Recipe is one way to install a component.
type Recipe struct {
	Needs   string
	Command []string
}

// Environment is the machine a menu works on, replaceable in tests.
type Environment struct {
	LookPath func(string) (string, error)
	Run      func(command []string, out io.Writer) error
}

// System is the real machine.
func System() Environment {
	return Environment{
		LookPath: exec.LookPath,
		Run: func(command []string, out io.Writer) error {
			cmd := exec.Command(command[0], command[1:]...)
			cmd.Stdout = out
			cmd.Stderr = out
			return cmd.Run()
		},
	}
}

// Recipe returns the first recipe this machine can run.
func (c Component) Recipe(env Environment) (Recipe, bool) {
	for _, recipe := range c.Recipes {
		if _, err := env.LookPath(recipe.Needs); err == nil {
			return recipe, true
		}
	}
	return Recipe{}, false
}

func located(language string) func() (string, bool) {
	return func() (string, bool) {
		spec, ok := lsp.SpecFor(language)
		if !ok {
			return "", false
		}
		return spec.Locate()
	}
}

// LanguageServers lists the language servers Jade can use, JVM and Go first.
func LanguageServers() []Component {
	const kind = "language server"
	return []Component{
		{
			Key: "go", Kind: kind, Name: "gopls", For: "Go", Languages: []string{"go"}, Detect: located("go"),
			Recipes: []Recipe{{Needs: "go", Command: []string{"go", "install", "golang.org/x/tools/gopls@latest"}}},
			Manual:  "install Go from https://go.dev/dl, then: go install golang.org/x/tools/gopls@latest",
		},
		{
			Key: "java", Kind: kind, Name: "jdtls", For: "Java", Languages: []string{"java"}, Detect: located("java"),
			Recipes: []Recipe{{Needs: "brew", Command: []string{"brew", "install", "jdtls"}}},
			Manual:  "needs a JDK 21 or newer; download jdtls from https://download.eclipse.org/jdtls/milestones/ and put its bin/jdtls on PATH",
			Note:    "jdtls needs a JDK 21 or newer on PATH",
		},
		{
			Key: "scala", Kind: kind, Name: "metals", For: "Scala", Languages: []string{"scala"}, Detect: located("scala"),
			Recipes: []Recipe{{Needs: "cs", Command: []string{"cs", "install", "metals"}}},
			Manual:  "install coursier from https://get-coursier.io, then: cs install metals",
			Note:    "set " + lsp.MetalsImportEnv + "=1 so metals may import sbt builds (it creates .bloop/ and .metals/)",
		},
		{
			Key: "typescript", Kind: kind, Name: "typescript-language-server", For: "TypeScript, JavaScript",
			Languages: []string{"typescript", "tsx", "javascript"}, Detect: located("typescript"),
			Recipes: []Recipe{{Needs: "npm", Command: []string{"npm", "install", "-g", "typescript-language-server", "typescript"}}},
			Manual:  "install Node.js from https://nodejs.org, then: npm install -g typescript-language-server typescript",
		},
		{
			Key: "python", Kind: kind, Name: "pyright", For: "Python", Languages: []string{"python"}, Detect: located("python"),
			Recipes: []Recipe{
				{Needs: "npm", Command: []string{"npm", "install", "-g", "pyright"}},
				{Needs: "pipx", Command: []string{"pipx", "install", "pyright"}},
			},
			Manual: "install Node.js, then: npm install -g pyright (or: pipx install pyright)",
		},
		{
			Key: "rust", Kind: kind, Name: "rust-analyzer", For: "Rust", Languages: []string{"rust"}, Detect: located("rust"),
			Recipes: []Recipe{
				{Needs: "rustup", Command: []string{"rustup", "component", "add", "rust-analyzer"}},
				{Needs: "brew", Command: []string{"brew", "install", "rust-analyzer"}},
			},
			Manual: "install Rust from https://rustup.rs, then: rustup component add rust-analyzer",
		},
		{
			Key: "ruby", Kind: kind, Name: "ruby-lsp", For: "Ruby", Languages: []string{"ruby"}, Detect: located("ruby"),
			Recipes: []Recipe{{Needs: "gem", Command: []string{"gem", "install", "ruby-lsp"}}},
			Manual:  "install Ruby, then: gem install ruby-lsp",
			Note:    "Ruby method rename also needs solargraph: gem install solargraph",
		},
	}
}

// Menu lists components and installs the ones picked.
type Menu struct {
	In         io.Reader
	Out        io.Writer
	Env        Environment
	Components []Component
}

// List prints each component, whether it is installed, and how it would be.
func (m Menu) List() {
	fmt.Fprintln(m.Out, "Language servers give Jade exact references, cross-file rename and type errors on edit.")
	fmt.Fprintln(m.Out, "Jade works without them and says when an answer is approximate.")
	fmt.Fprintln(m.Out)
	for i, component := range m.Components {
		fmt.Fprintf(m.Out, "  %d) %-11s %-27s %s\n", i+1, component.Key, component.Name, m.status(component))
	}
	fmt.Fprintln(m.Out)
}

func (m Menu) status(component Component) string {
	if path, ok := component.Detect(); ok {
		return "installed · " + path
	}
	if recipe, ok := component.Recipe(m.Env); ok {
		return "missing · would run: " + strings.Join(recipe.Command, " ")
	}
	return "missing · by hand: " + component.Manual
}

// ErrInstallFailed reports that at least one installer failed.
var ErrInstallFailed = errors.New("some installs failed")

// Run lists the components, asks which to install, confirms, and installs.
func (m Menu) Run() error {
	reader := bufio.NewReader(m.In)
	m.List()
	var picked []Component
	for {
		fmt.Fprint(m.Out, "Install which? Numbers or names separated by spaces, a for all missing, Enter for none: ")
		line, err := reader.ReadString('\n')
		choices, parseErr := m.pick(line)
		if parseErr == nil {
			picked = choices
			break
		}
		fmt.Fprintln(m.Out, parseErr)
		if err != nil {
			return nil
		}
	}
	if len(picked) == 0 {
		fmt.Fprintln(m.Out, "Nothing installed. Run jade-mcp install any time.")
		return nil
	}
	return m.install(picked, reader)
}

// Install installs the components with the given keys, or every missing one,
// without asking.
func (m Menu) Install(keys []string, all bool) error {
	if all {
		return m.install(m.Components, nil)
	}
	var picked []Component
	for _, key := range keys {
		component, ok := m.byKey(strings.TrimSpace(key))
		if !ok {
			return fmt.Errorf("unknown language server %q; choose from %s", key, strings.Join(m.keys(), ", "))
		}
		picked = append(picked, component)
	}
	return m.install(picked, nil)
}

// install runs the recipes for the missing components among picked. A nil
// reader installs without confirming.
func (m Menu) install(picked []Component, confirm *bufio.Reader) error {
	type step struct {
		component Component
		recipe    Recipe
	}
	var steps []step
	for _, component := range picked {
		if path, ok := component.Detect(); ok {
			fmt.Fprintf(m.Out, "%s is already installed at %s\n", component.Name, path)
			continue
		}
		recipe, ok := component.Recipe(m.Env)
		if !ok {
			fmt.Fprintf(m.Out, "%s: no installer on this machine; by hand: %s\n", component.Name, component.Manual)
			continue
		}
		steps = append(steps, step{component, recipe})
	}
	if len(steps) == 0 {
		return nil
	}

	if confirm != nil {
		fmt.Fprintln(m.Out, "\nThis will run:")
		for _, s := range steps {
			fmt.Fprintf(m.Out, "  %s\n", strings.Join(s.recipe.Command, " "))
		}
		fmt.Fprint(m.Out, "Go ahead? [Y/n] ")
		answer, _ := confirm.ReadString('\n')
		if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "" && answer != "y" && answer != "yes" {
			fmt.Fprintln(m.Out, "Nothing installed.")
			return nil
		}
	}

	failed := 0
	for _, s := range steps {
		fmt.Fprintf(m.Out, "\n→ %s\n", strings.Join(s.recipe.Command, " "))
		if err := m.Env.Run(s.recipe.Command, m.Out); err != nil {
			failed++
			fmt.Fprintf(m.Out, "%s: install failed (%v); by hand: %s\n", s.component.Name, err, s.component.Manual)
			continue
		}
		if path, ok := s.component.Detect(); ok {
			fmt.Fprintf(m.Out, "%s installed at %s\n", s.component.Name, path)
		} else {
			fmt.Fprintf(m.Out, "%s installed, but not where Jade looks yet; open a new shell or add its bin directory to PATH\n", s.component.Name)
		}
		if s.component.Note != "" {
			fmt.Fprintf(m.Out, "note: %s\n", s.component.Note)
		}
	}
	fmt.Fprintln(m.Out, "\nReconnect your MCP client (/mcp in Claude Code) so Jade picks the servers up.")
	if failed > 0 {
		return ErrInstallFailed
	}
	return nil
}

// pick reads a menu answer: numbers or keys, "a" for every missing component,
// empty for none.
func (m Menu) pick(line string) ([]Component, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	if lower := strings.ToLower(line); lower == "a" || lower == "all" {
		var missing []Component
		for _, component := range m.Components {
			if _, ok := component.Detect(); !ok {
				missing = append(missing, component)
			}
		}
		return missing, nil
	}
	var picked []Component
	seen := map[string]bool{}
	for _, field := range strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == ',' }) {
		var component Component
		if n, err := strconv.Atoi(field); err == nil {
			if n < 1 || n > len(m.Components) {
				return nil, fmt.Errorf("%d is not in the list; pick 1 to %d", n, len(m.Components))
			}
			component = m.Components[n-1]
		} else if found, ok := m.byKey(strings.ToLower(field)); ok {
			component = found
		} else {
			return nil, fmt.Errorf("%q is not in the list; use a number or one of %s", field, strings.Join(m.keys(), ", "))
		}
		if !seen[component.Key] {
			seen[component.Key] = true
			picked = append(picked, component)
		}
	}
	return picked, nil
}

func (m Menu) byKey(key string) (Component, bool) {
	for _, component := range m.Components {
		if component.Key == key {
			return component, true
		}
	}
	return Component{}, false
}

func (m Menu) keys() []string {
	keys := make([]string, 0, len(m.Components))
	for _, component := range m.Components {
		keys = append(keys, component.Key)
	}
	return keys
}
