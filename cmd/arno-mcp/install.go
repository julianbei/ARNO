package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/julianbei/arno/internal/setup"
)

// runInstall is `arno-mcp install`: a menu of the language servers Arno can
// use, or with flags the same without questions. install.sh opens it after a
// first install; plugins join the menu later as another kind of component.
func runInstall(args []string, stdin *os.File, stdout io.Writer) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(stdout)
	list := flags.Bool("list", false, "show what is installed and how the rest would be, then exit")
	all := flags.Bool("all", false, "install every missing language server this machine has an installer for")
	servers := flags.String("servers", "", "install these without asking, comma-separated: go,java,scala,typescript,python,rust,ruby")
	asJSON := flags.Bool("json", false, "with --list, print the status as JSON for an agent or script")
	dryRun := flags.Bool("dry-run", false, "with --servers or --all, print the commands instead of running them")
	if err := flags.Parse(args); err != nil {
		return err
	}

	menu := setup.Menu{In: stdin, Out: stdout, Env: setup.System(), Components: setup.LanguageServers()}
	switch {
	case *list && *asJSON:
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(menu.Statuses())
	case *list:
		menu.List()
		return nil
	case *all || *servers != "":
		var keys []string
		if *servers != "" {
			keys = strings.Split(*servers, ",")
		}
		return menu.Install(keys, *all, *dryRun)
	case !isTerminal(stdin):
		// A menu nobody can answer would wait forever or read garbage. An
		// agent lands here: give it the commands it can use instead.
		menu.List()
		fmt.Fprintln(stdout, "No terminal to ask in. Use: arno-mcp install --list --json, then --servers go,java (add --dry-run to preview), or --all.")
		return nil
	default:
		return menu.Run()
	}
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
