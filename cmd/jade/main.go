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
	"github.com/julianbei/jade/internal/protocol"
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

	mcpServer := mcp.NewServer(wm, ci, es, ds, jr, lr, bus)
	internalServer := internalapi.NewServer(wm, ci, es, ds, jr, lr, bus)

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

	if len(os.Args) == 3 && os.Args[1] == "api-outline" {
		response, outlineErr := internalServer.Outline(protocol.OutlineRequest{Path: os.Args[2]})
		if outlineErr != nil {
			log.Fatalf("api outline failed: %v", outlineErr)
		}
		for _, item := range response.Outline {
			fmt.Printf("%s %s %s:%d-%d\n", item.Kind, item.Name, item.Path, item.From, item.To)
		}
		fmt.Printf("revision=%s drifted=%t\n", response.Revision, response.Freshness.Drifted)
		return
	}

	if len(os.Args) == 3 && os.Args[1] == "api-outline-sections" {
		response, outlineErr := internalServer.Outline(protocol.OutlineRequest{Path: os.Args[2]})
		if outlineErr != nil {
			log.Fatalf("api outline failed: %v", outlineErr)
		}
		fmt.Printf(
			"imports=%d types=%d classes=%d functions=%d methods=%d other=%d\n",
			len(response.Sections.Imports),
			len(response.Sections.Types),
			len(response.Sections.Classes),
			len(response.Sections.Functions),
			len(response.Sections.Methods),
			len(response.Sections.Other),
		)
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "api-read-symbol" {
		response, readErr := internalServer.ReadSymbol(protocol.ReadSymbolRequest{
			Path:       os.Args[2],
			SymbolName: os.Args[3],
		})
		if readErr != nil {
			log.Fatalf("api read symbol failed: %v", readErr)
		}
		fmt.Printf("status=%s query=%s selected=%s\n", response.Resolve.Status, response.Resolve.Query, response.Resolve.SelectedID)
		if len(response.Resolve.CandidateIDs) > 0 {
			fmt.Printf("candidates=%v\n", response.Resolve.CandidateIDs)
		}
		if response.Source != "" {
			fmt.Println(response.Source)
		}
		return
	}

	log.Println("jade runtime initialized")
}
