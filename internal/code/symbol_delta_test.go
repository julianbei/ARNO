package code

import (
	"testing"

	"github.com/julianbei/arno/internal/protocol"
)

func findChange(changes []protocol.SymbolChange, symbol string) (protocol.SymbolChange, bool) {
	for _, change := range changes {
		if change.Symbol == symbol {
			return change, true
		}
	}
	return protocol.SymbolChange{}, false
}

func TestSymbolDeltaDetectsAddedRemovedAndModified(t *testing.T) {
	oldSource := []byte(`package fixture

func Kept() int {
	return 1
}

func Changed() int {
	return 2
}

func Removed() int {
	return 3
}
`)
	newSource := []byte(`package fixture

func Kept() int {
	return 1
}

func Changed() int {
	return 22
}

func Added() int {
	return 4
}
`)

	changes := NewIndex(t.TempDir(), nil).SymbolDelta("fixture.go", oldSource, newSource)

	if change, ok := findChange(changes, "Added"); !ok || change.Change != protocol.SymbolAdded {
		t.Fatalf("expected Added to be reported as added, got %+v", changes)
	}
	if change, ok := findChange(changes, "Removed"); !ok || change.Change != protocol.SymbolRemoved {
		t.Fatalf("expected Removed to be reported as removed, got %+v", changes)
	}
	if change, ok := findChange(changes, "Changed"); !ok || change.Change != protocol.SymbolModified {
		t.Fatalf("expected Changed to be reported as modified, got %+v", changes)
	}
	if _, ok := findChange(changes, "Kept"); ok {
		t.Fatalf("expected an untouched symbol to be absent, got %+v", changes)
	}
}

func TestSymbolDeltaIgnoresSymbolsThatOnlyMoved(t *testing.T) {
	// The property that makes this feature usable rather than noisy: adding
	// a function at the top shifts every line below it, and a line-range
	// comparison would report the whole file as modified.
	oldSource := []byte(`package fixture

func Stable() int {
	return 1
}
`)
	newSource := []byte(`package fixture

func Inserted() int {
	return 0
}

func Stable() int {
	return 1
}
`)

	changes := NewIndex(t.TempDir(), nil).SymbolDelta("fixture.go", oldSource, newSource)

	if _, ok := findChange(changes, "Stable"); ok {
		t.Fatalf("expected a symbol that only moved to be unchanged, got %+v", changes)
	}
	if change, ok := findChange(changes, "Inserted"); !ok || change.Change != protocol.SymbolAdded {
		t.Fatalf("expected the inserted symbol to be added, got %+v", changes)
	}
}

func TestSymbolDeltaTreatsMissingOldSourceAsAllAdded(t *testing.T) {
	newSource := []byte("package fixture\n\nfunc Brand() int { return 1 }\n")

	changes := NewIndex(t.TempDir(), nil).SymbolDelta("fixture.go", nil, newSource)

	if len(changes) != 1 || changes[0].Change != protocol.SymbolAdded {
		t.Fatalf("expected a new file's symbols to all be added, got %+v", changes)
	}
}

func TestSymbolDeltaTreatsMissingNewSourceAsAllRemoved(t *testing.T) {
	oldSource := []byte("package fixture\n\nfunc Gone() int { return 1 }\n")

	changes := NewIndex(t.TempDir(), nil).SymbolDelta("fixture.go", oldSource, nil)

	if len(changes) != 1 || changes[0].Change != protocol.SymbolRemoved {
		t.Fatalf("expected a deleted file's symbols to all be removed, got %+v", changes)
	}
}

func TestSymbolDeltaReportsNothingForIdenticalSource(t *testing.T) {
	source := []byte("package fixture\n\nfunc Same() int { return 1 }\n")

	changes := NewIndex(t.TempDir(), nil).SymbolDelta("fixture.go", source, source)
	if len(changes) != 0 {
		t.Fatalf("expected no changes for identical source, got %+v", changes)
	}
}

func TestSymbolDeltaDegradesForUnparseableLanguages(t *testing.T) {
	// A language arno cannot parse must yield no symbol detail rather than
	// wrong detail — the file-level counts still stand on their own.
	changes := NewIndex(t.TempDir(), nil).SymbolDelta("notes.txt",
		[]byte("some prose\n"), []byte("different prose\n"))

	if len(changes) != 0 {
		t.Fatalf("expected no symbol changes for an unparseable file, got %+v", changes)
	}
}

func TestSymbolDeltaCarriesKind(t *testing.T) {
	newSource := []byte("package fixture\n\ntype Thing struct{}\n")

	changes := NewIndex(t.TempDir(), nil).SymbolDelta("fixture.go", nil, newSource)
	if len(changes) == 0 {
		t.Fatalf("expected a change for the new type")
	}
	if changes[0].Kind == "" {
		t.Fatalf("expected the symbol kind to be reported, got %+v", changes[0])
	}
}

func TestSymbolDeltaDoesNotPoisonTheSymbolCache(t *testing.T) {
	// The old side of a diff never exists on disk. Caching it under the
	// current file's identity would make a later read return content the
	// file does not contain.
	index := NewIndex(t.TempDir(), nil)
	index.SymbolDelta("fixture.go", []byte("package fixture\n\nfunc Ghost() {}\n"), nil)

	index.cacheMu.RLock()
	defer index.cacheMu.RUnlock()
	if len(index.cache) != 0 {
		t.Fatalf("expected in-memory diffing to leave the cache untouched, got %d entries", len(index.cache))
	}
}
