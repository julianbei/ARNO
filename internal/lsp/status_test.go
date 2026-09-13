package lsp

import (
	"errors"
	"testing"
)

func TestStatusTellsTheKindsOfMissingApart(t *testing.T) {
	if got := (*Manager)(nil).Status("kotlin").State; got != "not supported" {
		t.Fatalf("a language with no known server: got %q", got)
	}

	manager := NewManager(t.TempDir())
	manager.markFailed("python", errors.New("exit status 1"))
	manager.markFailed("ruby", errNotInstalled{command: "ruby-lsp"})

	if got := manager.Status("python"); got.State != "failed" || got.Detail != "exit status 1" {
		t.Fatalf("a server that would not start: got %+v", got)
	}
	if got := manager.Status("ruby"); got.State != "not installed" || got.Detail != "ruby-lsp" {
		t.Fatalf("a server that is not installed: got %+v", got)
	}
}
