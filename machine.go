package statemachine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
)

var ErrTerminated = errors.New("normal shutdown of state machine")

type Logger interface {
	Debug(msg string, kvs ...interface{})
	Error(msg string, kvs ...interface{})
}

type Event struct {
	User interface{}
}

// Planner processes in queue events
// It returns:
// 1. a handler of type -- func(ctx Context, st <T>) (func(*<T>), error), where <T> is the typeOf(User) param
// 2. the number of events processed
// 3. an error if occured
type Planner func(events []Event, user interface{}) (interface{}, uint64, error)

type StateMachine struct {
	logger Logger

	planner  Planner
	eventsIn chan Event

	name      interface{}
	st        storedState
	stateType reflect.Type

	closing chan struct{}
	closed  chan struct{}

	busy int32
}

func (fsm *StateMachine) run() {
	defer close(fsm.closed)

	var pendingEvents []Event

	stageDone := make(chan struct{})
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

			var nextStep interface{}
			var ustate interface{}
			var processed uint64
			var terminated bool

			err := fsm.mutateUser(func(user interface{}) (err error) {
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
				ctx: context.TODO(),
				send: func(evt interface{}) error {
					return fsm.send(Event{User: evt})
				},
			}

			go func() {
				defer fsm.logger.Debug("leaving critical zone and resetting atomic var to zero.")

				if nextStep != nil {
					res := reflect.ValueOf(nextStep).Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(ustate).Elem()})

					if res[0].Interface() != nil {
						fsm.logger.Error("executing step.", "error", res[0].Interface().(error)) // TODO: propagate top level
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

func (fsm *StateMachine) mutateUser(cb func(user interface{}) error) error {
	user, err := fsm.st.Get()
	if err != nil {
		return err
	}

	userV := reflect.ValueOf(user)
	if !userV.Type().AssignableTo(fsm.stateType) {
		return fmt.Errorf("mutate item with incorrect type %T", user)
	}

	state := reflect.New(userV.Type())
	state.Elem().Set(userV)

	err = cb(state.Interface())
	if err != nil {
		return err
	}

	return fsm.st.Set(state.Elem().Interface())
}

func (fsm *StateMachine) send(evt Event) error {
	select {
	case <-fsm.closed:
		return ErrTerminated
	case fsm.eventsIn <- evt: // TODO: ctx, at least
		return nil
	}
}

func (fsm *StateMachine) stop(ctx context.Context) error {
	close(fsm.closing)

	select {
	case <-fsm.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
