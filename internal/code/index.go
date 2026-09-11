package code

// Symbol is the minimum structural unit surfaced to agents.
type Symbol struct {
	ID   string
	Kind string
	Name string
	Path string
	From int
	To   int
}

// Index is a placeholder for parser-backed symbol indexing.
type Index struct{}

func NewIndex() *Index {
	return &Index{}
}

func (i *Index) Outline(path string) []Symbol {
	return []Symbol{
		{
			ID:   path + "::outline",
			Kind: "file",
			Name: path,
			Path: path,
			From: 1,
			To:   1,
		},
	}
}
