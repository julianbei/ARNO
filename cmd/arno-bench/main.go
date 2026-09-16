// Command arno-bench measures what it costs an agent to answer questions
// with arno versus with shell and file tools.
//
// Usage:
//
//	arno-bench [-root <path>]             tokens to answer, no agent
//	arno-bench agent -tasks f -repo dir    real agent, three arms
//
// The default mode answers docs/scope.md §26's question for the tokens
// dimension only. `agent` measures success, turns, tokens, cost and diff size
// with Claude Code headless; see docs/benchmark.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/julianbei/arno/internal/bench"
	"github.com/julianbei/arno/internal/code"
	"github.com/julianbei/arno/internal/diagnostics"
	"github.com/julianbei/arno/internal/edit"
	"github.com/julianbei/arno/internal/events"
	"github.com/julianbei/arno/internal/jobs"
	"github.com/julianbei/arno/internal/languages"
	"github.com/julianbei/arno/internal/transport/internalapi"
	"github.com/julianbei/arno/internal/workspace"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		os.Exit(runAgent(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "agent-report" {
		os.Exit(runAgentReport(os.Args[2:]))
	}

	root := flag.String("root", "", "repository root to benchmark against (default: working directory)")
	flag.Parse()

	if *root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve working directory: %v\n", err)
			os.Exit(1)
		}
		*root = cwd
	}

	api, err := newAPI(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start arno: %v\n", err)
		os.Exit(1)
	}

	results := bench.Run(bench.DefaultScenarios(api, *root))
	fmt.Print(bench.Report(results))
}

// newAPI builds the same server the MCP entrypoint builds, so the benchmark
// measures the real tool surface rather than a stripped-down stand-in.
func newAPI(root string) (*internalapi.Server, error) {
	bus := events.NewBus()
	wm := workspace.NewManager(root, bus)
	ci := code.NewIndex(root, bus)
	ds := diagnostics.NewService(root)
	jr := jobs.NewRunner(bus)
	es := edit.NewService(wm, ci, ds, jr)
	lr := languages.NewRegistry()

	lr.Register("typescript", languages.NewNoopAdapter("typescript"))
	lr.Register("go", languages.NewNoopAdapter("go"))
	lr.Register("rust", languages.NewNoopAdapter("rust"))

	api := internalapi.NewServer(wm, ci, es, ds, jr, lr, bus)
	if err := api.Start(context.Background()); err != nil {
		return nil, err
	}
	return api, nil
}
