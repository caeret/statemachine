package statemachine

type StateStore interface {
	Has(key interface{}) (bool, error)
	Begin(i interface{}, state interface{}) error
	Get(key interface{}) StoredState
	List(interface{}) error
}

type StoredState interface {
	End() error
	Get(interface{}) error
	Mutate(mutator interface{}) error
}
