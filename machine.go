package statemachine

import (
	"context"
	"errors"
	"sync/atomic"
)

var ErrTerminated = errors.New("normal shutdown of state machine")

type Event struct {
	User any
}

type NextStep[T any] func(ctx Context, st T) error

// Planner processes in queue events
// It returns:
// 1. a handler of type -- func(ctx Context, st <T>) (func(*<T>), error), where <T> is the typeOf(User) param
// 2. the number of events processed
// 3. an error if occured
type Planner[T any] func(events []Event, user *T) (NextStep[T], uint64, error)

type StateMachine[K comparable, T any] struct {
	logger Logger

	planner  Planner[T]
	eventsIn chan Event

	name K
	st   storedState[K, T]

	closing chan struct{}
	closed  chan struct{}

	busy int32
}

func (fsm *StateMachine[K, T]) run() {
	defer close(fsm.closed)

	var pendingEvents []Event

	stageDone := make(chan struct{})

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	for {
		// NOTE: This requires at least one event to be sent to trigger a stage
		//  This means that after restarting the state machine users of this
		//  code must send a 'restart' event
		select {
		case evt := <-fsm.eventsIn:
			pendingEvents = append(pendingEvents, evt)
			fsm.logger.Debug("appended new pending evt", "len(pending)", len(pendingEvents))
		case <-stageDone:
			if len(pendingEvents) == 0 {
				continue
			}
		case <-fsm.closing:
			return
		}

		select {
		case <-fsm.closing:
			return
		default:
		}

		if atomic.CompareAndSwapInt32(&fsm.busy, 0, 1) {
			fsm.logger.Debug("sm run in critical zone", "len(pending)", len(pendingEvents))

			var nextStep NextStep[T]
			var ustate *T
			var processed uint64
			var terminated bool

			err := fsm.mutateUser(func(user *T) (err error) {
				nextStep, processed, err = fsm.planner(pendingEvents, user)
				ustate = user
				if errors.Is(err, ErrTerminated) {
					terminated = true
					return nil
				}
				return err
			})
			if terminated {
				return
			}
			if err != nil {
				fsm.logger.Error("Executing event planner failed.", "error", err)
				return
			}

			if processed < uint64(len(pendingEvents)) {
				pendingEvents = pendingEvents[processed:]
			} else {
				pendingEvents = nil
			}

			ctx := Context{
				Context: ctx,
				send: func(evt any) error {
					return fsm.send(Event{User: evt})
				},
			}

			go func() {
				defer fsm.logger.Debug("leaving critical zone and resetting atomic var to zero.")

				if nextStep != nil {
					err := nextStep(ctx, *ustate)
					if err != nil {
						fsm.logger.Error("executing step.", "error", err) // TODO: propagate top level
						return
					}
				}

				atomic.StoreInt32(&fsm.busy, 0)
				select {
				case stageDone <- struct{}{}:
				case <-fsm.closed:
				}
			}()

		}
	}
}

func (fsm *StateMachine[K, T]) mutateUser(cb func(user *T) error) error {
	user, err := fsm.st.Get()
	if err != nil {
		return err
	}

	err = cb(&user)
	if err != nil {
		return err
	}

	return fsm.st.Set(user)
}

func (fsm *StateMachine[K, T]) send(evt Event) error {
	select {
	case <-fsm.closed:
		return ErrTerminated
	case fsm.eventsIn <- evt: // TODO: ctx, at least
		return nil
	}
}

func (fsm *StateMachine[K, T]) stop(ctx context.Context) error {
	close(fsm.closing)

	select {
	case <-fsm.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
