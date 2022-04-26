package statemachine

type StateStore interface {
	Has(key interface{}) (bool, error)
	Get(key interface{}) (interface{}, error)
	Set(key interface{}, state interface{}) error
}

type storedState struct {
	key interface{}
	ss  StateStore
}

func (s *storedState) Get() (interface{}, error) {
	return s.ss.Get(s.key)
}

func (s *storedState) Set(state interface{}) error {
	return s.ss.Set(s.key, state)
}
