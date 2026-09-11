package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/events"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/languages"
	"github.com/julianbei/jade/internal/transport/internalapi"
	"github.com/julianbei/jade/internal/transport/mcp"
	"github.com/julianbei/jade/internal/workspace"
)

func main() {
	ctx := context.Background()
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to resolve working directory: %v", err)
	}

	bus := events.NewBus()
	eventsCh := bus.Subscribe(32)
	go func() {
		for event := range eventsCh {
			log.Printf("event type=%s entity=%s payload=%v", event.Type, event.Entity, event.Payload)
		}
	}()

	wm := workspace.NewManager(root, bus)
	ci := code.NewIndex(root, bus)
	ds := diagnostics.NewService()
	jr := jobs.NewRunner(bus)
	es := edit.NewService(wm, ci, ds, jr)
	lr := languages.NewRegistry()

	lr.Register("typescript", languages.NewNoopAdapter("typescript"))
	lr.Register("go", languages.NewNoopAdapter("go"))
	lr.Register("rust", languages.NewNoopAdapter("rust"))

	mcpServer := mcp.NewServer(wm, ci, es, ds, jr, lr)
	internalServer := internalapi.NewServer(wm, ci, es, ds, jr, lr)

	if err := mcpServer.Start(ctx); err != nil {
		log.Fatalf("failed to start mcp transport: %v", err)
	}
	if err := internalServer.Start(ctx); err != nil {
		log.Fatalf("failed to start internal transport: %v", err)
	}

	if len(os.Args) == 3 && os.Args[1] == "outline" {
		symbols, outlineErr := ci.Outline(os.Args[2])
		if outlineErr != nil {
			log.Fatalf("outline failed: %v", outlineErr)
		}
		for _, symbol := range symbols {
			fmt.Printf("%s %s %s:%d-%d\n", symbol.Kind, symbol.Name, symbol.Path, symbol.From, symbol.To)
		}
		return
	}

	log.Println("jade runtime initialized")
}
