package main

import (
	"context"
	"log"

	"github.com/julianbei/jade/internal/code"
	"github.com/julianbei/jade/internal/diagnostics"
	"github.com/julianbei/jade/internal/edit"
	"github.com/julianbei/jade/internal/jobs"
	"github.com/julianbei/jade/internal/languages"
	"github.com/julianbei/jade/internal/transport/internalapi"
	"github.com/julianbei/jade/internal/transport/mcp"
	"github.com/julianbei/jade/internal/workspace"
)

func main() {
	ctx := context.Background()

	wm := workspace.NewManager()
	ci := code.NewIndex()
	es := edit.NewService(wm, ci)
	ds := diagnostics.NewService()
	jr := jobs.NewRunner()
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

	log.Println("jade scaffold initialized")
}
