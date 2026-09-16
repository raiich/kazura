package state_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/task/eventloop"
)

// Verification policy (definition of done and falsification condition: state/machine_test.go):
//
//	Guaranteed: the errors Trigger / Stop / AfterFunc return on the AfterFuncMachine a timer
//	  callback receives (errors.Is / errors.As), CurrentState after a transition, and that a
//	  stale handle attaches nothing to the machine.
//	Not guaranteed: timer firing order and cancellation (entrymachine_test.go), calling the
//	  captured Machine directly (machine_test.go, "Machine.Trigger from a timer callback
//	  performs the transition").
//	Not automated: none.
//	Strength check required: "the handle is stale after its own Trigger succeeds"

func TestAfterFuncMachine_Trigger(t *testing.T) {
	type NextEvent struct{}
	type UndefinedEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("Trigger performs the transition and returns no error", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var machine *state.Machine[State, *TestValue]
		var triggerErr error
		var reached State
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					triggerErr = m.Trigger(NextEvent{})
					reached, _ = machine.CurrentState()
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))

		assert.NoError(t, triggerErr)
		assert.Equal(t, next, reached)
	})

	t.Run("Trigger returns ErrNoTransition for an undefined event", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var triggerErr error
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					triggerErr = m.Trigger(UndefinedEvent{})
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))

		assert.ErrorIs(t, triggerErr, state.ErrNoTransition)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("Trigger returns the Guarded of the visit's own exit action", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		guarded := &state.Guarded{Reason: errors.New("blocked")}
		var triggerErr error
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return guarded
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					triggerErr = m.Trigger(NextEvent{})
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))

		assert.Same(t, guarded, triggerErr)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)

		assert.Same(t, guarded, machine.Trigger(NextEvent{}))
	})

	t.Run("Trigger returns the failure of the chained Entry", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var triggerErr error
		next := &TestState{
			name: "next",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Trigger(UndefinedEvent{})
			},
		}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					triggerErr = m.Trigger(NextEvent{})
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))

		assert.ErrorIs(t, triggerErr, state.ErrNoTransition)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("a transition from a timer callback cancels the other timers of the visit", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		lateCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					require.NoError(t, m.Trigger(NextEvent{}))
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 5*time.Second, func(*AfterFuncMachine) {
					lateCount++
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		assert.Equal(t, 0, lateCount)
	})

	t.Run("the handle is stale after its own Trigger succeeds", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var afterFuncErr, stopErr error
		secondCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					require.NoError(t, m.Trigger(NextEvent{}))
					afterFuncErr = m.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
						secondCount++
					})
					stopErr = m.Stop()
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))

		assert.ErrorIs(t, afterFuncErr, state.ErrStateLeft)
		assert.ErrorIs(t, stopErr, state.ErrStateLeft)
		assert.Equal(t, 0, secondCount)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("Trigger on a left AfterFuncMachine returns ErrStateLeft", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var left *AfterFuncMachine
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					left = m
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		require.NoError(t, machine.Trigger(NextEvent{}))

		assert.ErrorIs(t, left.Trigger(NextEvent{}), state.ErrStateLeft)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})
}

func TestAfterFuncMachine_Stop(t *testing.T) {
	type NextEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("Stop stops the machine from a timer callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var exitEvents []Event
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(event Event) *state.Guarded {
					exitEvents = append(exitEvents, event)
					return nil
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					require.NoError(t, m.Stop())
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))

		assert.Equal(t, []Event{nil}, exitEvents)
		assert.Equal(t, []Transition{{To: initial}, {From: initial}}, tracer.calls)
		_, err = machine.CurrentState()
		assert.ErrorIs(t, err, state.ErrNotLaunched)
	})

	t.Run("Stop on a left AfterFuncMachine returns ErrStateLeft", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var left *AfterFuncMachine
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					left = m
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		require.NoError(t, machine.Trigger(NextEvent{}))

		assert.ErrorIs(t, left.Stop(), state.ErrStateLeft)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})
}

func TestAfterFuncMachine_AfterFunc(t *testing.T) {
	type NextEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("a timer callback can schedule another timer of the same visit", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var firedAt []time.Duration
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 2*time.Second, func(m *AfterFuncMachine) {
					firedAt = append(firedAt, 2*time.Second)
					require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
						firedAt = append(firedAt, 3*time.Second)
					}))
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.Equal(t, []time.Duration{2 * time.Second}, firedAt)

		require.NoError(t, dispatcher.FastForward(baseTime.Add(3*time.Second)))
		assert.Equal(t, []time.Duration{2 * time.Second, 3 * time.Second}, firedAt)
	})

	t.Run("AfterFunc on a left AfterFuncMachine returns ErrStateLeft", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var left *AfterFuncMachine
		fireCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(m *AfterFuncMachine) {
					left = m
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		require.NoError(t, machine.Trigger(NextEvent{}))

		err = left.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
			fireCount++
		})
		assert.ErrorIs(t, err, state.ErrStateLeft)
		assert.Equal(t, 0, machine.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		assert.Equal(t, 0, fireCount)
	})
}
