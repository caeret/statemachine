package statemachine

import (
	"context"
	"fmt"
	"sync"
)

type StateHandler[T any] interface {
	Plan(events []Event, user *T) (NextStep[T], uint64, error)
}

type StateHandlerWithInit[T any] interface {
	StateHandler[T]
	Init(<-chan struct{})
}

// StateGroup manages a group of state machines sharing the same logic
type StateGroup[K comparable, T any] struct {
	logger Logger

	sts StateStore[K, T]
	hnd StateHandler[T]

	closing      chan struct{}
	initNotifier sync.Once

	lk  sync.Mutex
	sms map[K]*StateMachine[K, T]
}

func New[K comparable, T any](logger Logger, sts StateStore[K, T], hnd StateHandler[T]) *StateGroup[K, T] {
	return &StateGroup[K, T]{
		logger:  logger,
		sts:     sts,
		hnd:     hnd,
		closing: make(chan struct{}),
		sms:     map[K]*StateMachine[K, T]{},
	}
}

func (s *StateGroup[K, T]) init() {
	initter, ok := s.hnd.(StateHandlerWithInit[T])
	if ok {
		initter.Init(s.closing)
	}
}

// Begin initiates tracking with a specific value for a given identifier
func (s *StateGroup[K, T]) Begin(id K, userState T) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	sm, exist := s.sms[id]
	if exist {
		return fmt.Errorf("Begin: already tracking identifier `%v`", id)
	}

	exists, err := s.sts.Has(id)
	if err != nil {
		return fmt.Errorf("failed to check if state for %v exists: %w", id, err)
	}
	if exists {
		return fmt.Errorf("Begin: cannot initiate a state for identifier `%v` that already exists", id)
	}

	sm, err = s.loadOrCreate(id, userState)
	if err != nil {
		return fmt.Errorf("loadOrCreate state: %w", err)
	}
	s.sms[id] = sm
	return nil
}

// Send sends an event to machine identified by `id`.
// `evt` is going to be passed into StateHandler.Planner, in the events[].User param
//
// If a state machine with the specified id doesn't exits, it's created, and it's
// state is set to zero-value of stateType provided in group constructor
func (s *StateGroup[K, T]) Send(id K, evt any) (err error) {
	s.lk.Lock()
	defer s.lk.Unlock()

	sm, exist := s.sms[id]
	if !exist {
		userState := *new(T)
		sm, err = s.loadOrCreate(id, userState)
		if err != nil {
			return fmt.Errorf("loadOrCreate state: %w", err)
		}
		s.sms[id] = sm
	}

	return sm.send(Event{User: evt})
}

func (s *StateGroup[K, T]) loadOrCreate(name K, userState T) (*StateMachine[K, T], error) {
	s.initNotifier.Do(s.init)
	exists, err := s.sts.Has(name)
	if err != nil {
		return nil, fmt.Errorf("failed to check if state for %v exists: %w", name, err)
	}

	if !exists {
		err = s.sts.Set(name, userState)
		if err != nil {
			return nil, fmt.Errorf("saving initial state: %w", err)
		}
	}

	res := &StateMachine[K, T]{
		logger: s.logger,

		planner:  s.hnd.Plan,
		eventsIn: make(chan Event),

		name: name,
		st: storedState[K, T]{
			key: name,
			ss:  s.sts,
		},

		closing: make(chan struct{}),
		closed:  make(chan struct{}),
	}

	go res.run()

	return res, nil
}

// Stop stops all state machines in this group
func (s *StateGroup[K, T]) Stop(ctx context.Context) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	for _, sm := range s.sms {
		if err := sm.stop(ctx); err != nil {
			return err
		}
	}

	close(s.closing)
	return nil
}
