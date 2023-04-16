package statemachine

import "context"

type StateStore[K comparable, T any] interface {
	Has(ctx context.Context, key K) (bool, error)
	Get(ctx context.Context, key K) (T, error)
	Set(ctx context.Context, key K, state T) error
}

type storedState[K comparable, T any] struct {
	key K
	ss  StateStore[K, T]
}

func (s *storedState[K, T]) Get(ctx context.Context) (T, error) {
	return s.ss.Get(ctx, s.key)
}

func (s *storedState[K, T]) Set(ctx context.Context, state T) error {
	return s.ss.Set(ctx, s.key, state)
}
