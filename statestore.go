package statemachine

type StateStore[K comparable, T any] interface {
	Has(key K) (bool, error)
	Get(key K) (T, error)
	Set(key K, state T) error
}

type storedState[K comparable, T any] struct {
	key K
	ss  StateStore[K, T]
}

func (s *storedState[K, T]) Get() (T, error) {
	return s.ss.Get(s.key)
}

func (s *storedState[K, T]) Set(state T) error {
	return s.ss.Set(s.key, state)
}
