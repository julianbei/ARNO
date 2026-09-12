package conformance

// Store keeps values by key.
type Store struct {
	values map[string]string
}

// Put records a value.
func (s *Store) Put(key string, value string) {
	s.values[key] = value
}

func NewStore() *Store {
	return &Store{values: map[string]string{}}
}
