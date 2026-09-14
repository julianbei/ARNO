package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/julianbei/jade/internal/setup"
)

// runInstall is `jade-mcp install`: a menu of the language servers Jade can
// use, or with flags the same without questions. install.sh opens it after a
// first install; plugins join the menu later as another kind of component.
func runInstall(args []string, stdin *os.File, stdout io.Writer) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(stdout)
	list := flags.Bool("list", false, "show what is installed and how the rest would be, then exit")
	all := flags.Bool("all", false, "install every missing language server this machine has an installer for")
	servers := flags.String("servers", "", "install these without asking, comma-separated: go,java,scala,typescript,python,rust,ruby")
	if err := flags.Parse(args); err != nil {
		return err
	}

	menu := setup.Menu{In: stdin, Out: stdout, Env: setup.System(), Components: setup.LanguageServers()}
	switch {
	case *list:
		menu.List()
		return nil
	case *all || *servers != "":
		var keys []string
		if *servers != "" {
			keys = strings.Split(*servers, ",")
		}
		return menu.Install(keys, *all)
	case !isTerminal(stdin):
		// A menu nobody can answer would wait forever or read garbage.
		menu.List()
		fmt.Fprintln(stdout, "Run jade-mcp install in a terminal to choose, or pass --servers go,java or --all.")
		return nil
	default:
		return menu.Run()
	}
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
