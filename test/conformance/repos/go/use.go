package conformance

func useStore() *Store {
	s := NewStore()
	s.Put("a", "1")
	s.Put("b", "2")
	return s
}
