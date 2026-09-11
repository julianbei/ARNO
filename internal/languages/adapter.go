package languages

import "context"

// Adapter abstracts language tooling behind a stable contract.
type Adapter interface {
	Name() string
	Diagnostics(context.Context, string) error
}

// Registry maps language identifiers to adapters.
type Registry struct {
	byName map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Adapter)}
}

func (r *Registry) Register(name string, adapter Adapter) {
	r.byName[name] = adapter
}

func (r *Registry) Get(name string) (Adapter, bool) {
	adapter, ok := r.byName[name]
	return adapter, ok
}

type noopAdapter struct {
	name string
}

func NewNoopAdapter(name string) Adapter {
	return &noopAdapter{name: name}
}

func (a *noopAdapter) Name() string {
	return a.name
}

func (a *noopAdapter) Diagnostics(context.Context, string) error {
	return nil
}
