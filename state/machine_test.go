package state_test

import (
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/state/graph"
	"github.com/raiich/kazura/task/eventloop"
)

// Not guaranteed: concurrent use from several goroutines (serializing on a Dispatcher is
// the caller's job), a Dispatcher that fires timers reentrantly from a callback,
// transitions and reuse after a callback panicked (only Stop is guaranteed), exhaustive
// graph validation (state/graph; graph_test.go covers what the Machine uses), real-time
// timer accuracy (the synchronous model driven by eventloop's FastForward only).

func TestNewMachine(t *testing.T) {
	t.Run("nil graph is accepted and reported by Launch", func(t *testing.T) {
		machine := state.NewMachine[State](nil, &TestValue{})

		assert.ErrorIs(t, machine.Launch(), state.ErrNilGraph)
	})

	t.Run("a graph without an initial node is reported by Launch", func(t *testing.T) {
		machine := state.NewMachine[State](&graph.Graph[State, reflect.Type]{}, &TestValue{})

		assert.ErrorIs(t, machine.Launch(), state.ErrNilGraph)
	})

	t.Run("valid graph and value succeeds", func(t *testing.T) {
		value := &TestValue{Map: map[string]any{"key": "value"}}
		g, err := state.NewGraph[State](&TestState{name: "initial"})
		require.NoError(t, err)

		machine := state.NewMachine(g, value)

		assert.Equal(t, value, machine.Value())
	})
}

func TestMachine_Launch(t *testing.T) {
	type NextEvent struct{}
	type UndefinedEvent struct{}

	t.Run("Launch enters the initial state with a nil event", func(t *testing.T) {
		value := &TestValue{}
		var entries []Event
		var entryValue *TestValue
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, event Event) state.Command {
				entries = append(entries, event)
				entryValue = m.Value()
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, value)
		require.NoError(t, machine.Launch())

		assert.Equal(t, []Event{nil}, entries)
		assert.Same(t, value, entryValue)
	})

	t.Run("CurrentState before Launch returns ErrNotLaunched", func(t *testing.T) {
		g, err := state.NewGraph[State](&TestState{name: "initial"})
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		_, err = machine.CurrentState()

		assert.ErrorIs(t, err, state.ErrNotLaunched)
	})

	t.Run("duplicate Launch returns ErrAlreadyLaunched", func(t *testing.T) {
		entryCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(*EntryMachine, Event) state.Command {
				entryCount++
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.ErrorIs(t, machine.Launch(), state.ErrAlreadyLaunched)
		assert.Equal(t, 1, entryCount)
	})

	t.Run("Launch after Stop enters the initial state again", func(t *testing.T) {
		var entries []Event
		initial := &TestState{
			name: "initial",
			entry: func(_ *EntryMachine, event Event) state.Command {
				entries = append(entries, event)
				return nil
			},
		}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))
		require.NoError(t, machine.Stop())

		require.NoError(t, machine.Launch())
		assert.Equal(t, []Event{nil, nil}, entries)
		require.Len(t, tracer.calls, 4)
		assert.Equal(t, Transition{To: initial}, tracer.calls[3])
	})

	t.Run("Launch processes the event returned by the initial Entry", func(t *testing.T) {
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Trigger(NextEvent{})
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("Launch returns the failure of the event the initial Entry returned", func(t *testing.T) {
		initial := &TestState{
			name: "initial",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Trigger(UndefinedEvent{})
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		assert.ErrorIs(t, machine.Launch(), state.ErrNoTransition)

		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})
}

func TestMachine_Trigger(t *testing.T) {
	type NextEvent struct{}
	type FirstEvent struct{}
	type SelfEvent struct{}
	type UndefinedEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("Trigger before Launch returns ErrNotLaunched", func(t *testing.T) {
		g, err := state.NewGraph[State](&TestState{name: "initial"})
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		assert.ErrorIs(t, machine.Trigger(NextEvent{}), state.ErrNotLaunched)
	})

	t.Run("Trigger with a nil event returns ErrNilEvent", func(t *testing.T) {
		exitCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.ErrorIs(t, machine.Trigger(nil), state.ErrNilEvent)
		assert.Equal(t, 0, exitCount)
	})

	t.Run("Trigger with no transition returns ErrNoTransition", func(t *testing.T) {
		guarded := &state.Guarded{Reason: errors.New("blocked")}
		exitCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return guarded
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.ErrorIs(t, machine.Trigger(UndefinedEvent{}), state.ErrNoTransition)
		assert.Equal(t, 0, exitCount)

		assert.Same(t, guarded, machine.Trigger(NextEvent{}))
		assert.Equal(t, 1, exitCount)
	})

	t.Run("Trigger performs the defined transition", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("Trigger from the destination without an edge returns ErrNoTransition", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		assert.ErrorIs(t, machine.Trigger(NextEvent{}), state.ErrNoTransition)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("Trigger returns the Guarded of the transition it performs", func(t *testing.T) {
		guarded := &state.Guarded{Reason: errors.New("blocked")}
		entryCount := 0
		next := &TestState{
			name: "next",
			entry: func(*EntryMachine, Event) state.Command {
				entryCount++
				return nil
			},
		}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return guarded
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.Same(t, guarded, machine.Trigger(NextEvent{}))
		assert.Equal(t, 0, entryCount)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("Trigger returns the failure of a chained event", func(t *testing.T) {
		a := &TestState{
			name: "a",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Trigger(UndefinedEvent{})
			},
		}
		initial := &TestState{name: "initial"}
		g, err := state.NewGraph[State](initial, On[FirstEvent](initial, a))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.ErrorIs(t, machine.Trigger(FirstEvent{}), state.ErrNoTransition)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, a, current)
	})

	t.Run("Launch, Trigger and Stop from inside Entry are refused", func(t *testing.T) {
		var machine *state.Machine[State, *TestValue]
		entryCount := 0
		next := &TestState{
			name: "next",
			entry: func(*EntryMachine, Event) state.Command {
				entryCount++
				return nil
			},
		}
		initial := &TestState{
			name: "initial",
			entry: func(*EntryMachine, Event) state.Command {
				assert.ErrorIs(t, machine.Launch(), state.ErrInCallback)
				assert.ErrorIs(t, machine.Trigger(NextEvent{}), state.ErrInTransition)
				assert.ErrorIs(t, machine.Stop(), state.ErrInTransition)
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.Equal(t, 0, entryCount)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("Launch, Trigger and Stop from inside the exit action are refused", func(t *testing.T) {
		var machine *state.Machine[State, *TestValue]
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					assert.ErrorIs(t, machine.Launch(), state.ErrInCallback)
					assert.ErrorIs(t, machine.Trigger(NextEvent{}), state.ErrInTransition)
					assert.ErrorIs(t, machine.Stop(), state.ErrInTransition)
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("Machine.Trigger from a timer callback performs the transition", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var machine *state.Machine[State, *TestValue]
		var triggerErr error
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, time.Second, func(*AfterFuncMachine) {
					triggerErr = machine.Trigger(NextEvent{})
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(time.Second)))

		assert.NoError(t, triggerErr)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})

	t.Run("Machine.Launch from a timer callback returns ErrInCallback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var machine *state.Machine[State, *TestValue]
		var stopErr, launchErr error
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, time.Second, func(*AfterFuncMachine) {
					stopErr = machine.Stop()
					launchErr = machine.Launch()
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(time.Second)))

		assert.NoError(t, stopErr)
		assert.ErrorIs(t, launchErr, state.ErrInCallback)
		_, err = machine.CurrentState()
		assert.ErrorIs(t, err, state.ErrNotLaunched)
	})

	t.Run("self transition runs the exit action and then Entry", func(t *testing.T) {
		var order []string
		initial := &TestState{name: "initial"}
		initial.entry = func(m *EntryMachine, _ Event) state.Command {
			order = append(order, "entry")
			require.NoError(t, m.OnExit(func(Event) *state.Guarded {
				order = append(order, "exit")
				return nil
			}))
			return nil
		}
		g, err := state.NewGraph[State](initial, On[SelfEvent](initial, initial))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(SelfEvent{}))
		assert.Equal(t, []string{"entry", "exit", "entry"}, order)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("Trigger after Stop returns ErrNotLaunched and does not rerun the exit action", func(t *testing.T) {
		exitCount := 0
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Stop())

		assert.ErrorIs(t, machine.Trigger(NextEvent{}), state.ErrNotLaunched)
		assert.Equal(t, 1, exitCount)
	})
}

func TestMachine_Stop(t *testing.T) {
	baseTime := time.Unix(0, 0)

	t.Run("Stop runs the exit action with a nil event", func(t *testing.T) {
		var exitEvents []Event
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(event Event) *state.Guarded {
					exitEvents = append(exitEvents, event)
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Stop())
		assert.Equal(t, []Event{nil}, exitEvents)
		_, err = machine.CurrentState()
		assert.ErrorIs(t, err, state.ErrNotLaunched)
	})

	t.Run("Stop is not blocked by a Guarded and returns no error", func(t *testing.T) {
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return &state.Guarded{Reason: errors.New("blocked")}
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Stop())
		assert.ErrorIs(t, machine.Stop(), state.ErrNotLaunched)
	})

	t.Run("Stop cancels the timers of the current visit", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		timerFired := false
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, 5*time.Second, func(*AfterFuncMachine) {
					timerFired = true
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Stop())

		require.NoError(t, dispatcher.FastForward(baseTime.Add(10*time.Second)))
		assert.False(t, timerFired)
	})

	t.Run("duplicate Stop returns ErrNotLaunched", func(t *testing.T) {
		exitCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Stop())

		assert.ErrorIs(t, machine.Stop(), state.ErrNotLaunched)
		assert.Equal(t, 1, exitCount)
	})

	t.Run("Stop before Launch returns ErrNotLaunched", func(t *testing.T) {
		g, err := state.NewGraph[State](&TestState{name: "initial"})
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		assert.ErrorIs(t, machine.Stop(), state.ErrNotLaunched)
	})

	t.Run("Stop succeeds after Entry panicked", func(t *testing.T) {
		var exitEvents []Event
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(event Event) *state.Guarded {
					exitEvents = append(exitEvents, event)
					return nil
				}))
				panic("entry failed")
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		assert.Panics(t, func() {
			_ = machine.Launch()
		})

		require.NoError(t, machine.Stop())
		assert.Equal(t, []Event{nil}, exitEvents)
	})

	t.Run("Stop succeeds after the exit action panicked and does not rerun it", func(t *testing.T) {
		type NextEvent struct{}
		dispatcher := eventloop.NewDispatcher(baseTime)
		exitCount := 0
		fired := false
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					panic("exit failed")
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 1*time.Second, func(*AfterFuncMachine) {
					fired = true
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		assert.Panics(t, func() {
			_ = machine.Trigger(NextEvent{})
		})

		require.NoError(t, machine.Stop())
		assert.Equal(t, 1, exitCount)
		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.False(t, fired)
	})
}

func TestMachine_Chain(t *testing.T) {
	type ChainEvent struct{}
	type LoopEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("a nil event from Entry ends the chain", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		assert.Equal(t, []Transition{{To: initial}}, tracer.calls)
	})

	t.Run("the event returned by Entry is processed after Entry returns", func(t *testing.T) {
		var machine *state.Machine[State, *TestValue]
		var order []string
		b := &TestState{
			name: "b",
			entry: func(*EntryMachine, Event) state.Command {
				order = append(order, "b-entry")
				return nil
			},
		}
		a := &TestState{name: "a"}
		a.entry = func(*EntryMachine, Event) state.Command {
			order = append(order, "a-entry")
			current, err := machine.CurrentState()
			require.NoError(t, err)
			assert.Equal(t, a, current)
			return state.Trigger(ChainEvent{})
		}
		g, err := state.NewGraph[State](a, On[ChainEvent](a, b))
		require.NoError(t, err)

		machine = state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.Equal(t, []string{"a-entry", "b-entry"}, order)
	})

	t.Run("a chain across states runs in order", func(t *testing.T) {
		var order []string
		initial := &TestState{name: "initial"}
		b := &TestState{
			name: "B",
			entry: func(*EntryMachine, Event) state.Command {
				order = append(order, "B-entry")
				return nil
			},
		}
		a := &TestState{
			name: "A",
			entry: func(*EntryMachine, Event) state.Command {
				order = append(order, "A-entry")
				return state.Trigger(ChainEvent{})
			},
		}
		g, err := state.NewGraph[State](
			initial,
			On[LoopEvent](initial, a),
			On[ChainEvent](a, b),
		)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(LoopEvent{}))
		assert.Equal(t, []string{"A-entry", "B-entry"}, order)
	})

	t.Run("a long chain does not exhaust the stack", func(t *testing.T) {
		const loops = 100000
		entryCount := 0
		firstDepth, lastDepth := 0, 0
		looping := &TestState{name: "looping"}
		looping.entry = func(*EntryMachine, Event) state.Command {
			entryCount++
			lastDepth = stackDepth()
			if entryCount == 1 {
				firstDepth = lastDepth
			}
			if entryCount > loops {
				return nil
			}
			return state.Trigger(LoopEvent{})
		}
		g, err := state.NewGraph[State](looping, On[LoopEvent](looping, looping))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.Equal(t, loops+1, entryCount)
		assert.Equal(t, firstDepth, lastDepth)
	})

	t.Run("a chained event blocked by a Guarded is returned and the chain stops there", func(t *testing.T) {
		guarded := &state.Guarded{Reason: errors.New("blocked")}
		exitCount := 0
		b := &TestState{name: "b"}
		initial := &TestState{name: "initial"}
		a := &TestState{
			name: "a",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return guarded
				}))
				return state.Trigger(ChainEvent{})
			},
		}
		g, err := state.NewGraph[State](
			initial,
			On[LoopEvent](initial, a),
			On[ChainEvent](a, b),
		)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		err = machine.Trigger(LoopEvent{})
		var blocked *state.Guarded
		require.ErrorAs(t, err, &blocked)
		assert.Same(t, guarded, blocked)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, a, current)

		assert.Same(t, guarded, machine.Trigger(ChainEvent{}))
		assert.Equal(t, 2, exitCount)
	})

	t.Run("a Command the package did not construct is returned as ErrUnknownCommand", func(t *testing.T) {
		type embeddedCommand struct{ state.Command }
		entryCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(*EntryMachine, Event) state.Command {
				entryCount++
				return embeddedCommand{Command: state.Trigger(ChainEvent{})}
			},
		}
		g, err := state.NewGraph[State](initial, On[ChainEvent](initial, initial))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		assert.ErrorIs(t, machine.Launch(), state.ErrUnknownCommand)
		assert.Equal(t, 1, entryCount)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("a nil event returned by Entry is returned as ErrNilEvent", func(t *testing.T) {
		exitCount := 0
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					exitCount++
					return nil
				}))
				return state.Trigger(nil)
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		assert.ErrorIs(t, machine.Launch(), state.ErrNilEvent)
		assert.Equal(t, 0, exitCount)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("a Stop returned by Entry stops the machine once Entry returns", func(t *testing.T) {
		var machine *state.Machine[State, *TestValue]
		var order []string
		var exitEvents []Event
		initial := &TestState{name: "initial"}
		initial.entry = func(m *EntryMachine, _ Event) state.Command {
			order = append(order, "entry")
			require.NoError(t, m.OnExit(func(event Event) *state.Guarded {
				order = append(order, "exit")
				exitEvents = append(exitEvents, event)
				return nil
			}))
			return state.Stop()
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine = state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		assert.Equal(t, []string{"entry", "exit"}, order)
		assert.Equal(t, []Event{nil}, exitEvents)
		assert.Equal(t, []Transition{{To: initial}, {From: initial}}, tracer.calls)
		_, err = machine.CurrentState()
		assert.ErrorIs(t, err, state.ErrNotLaunched)
	})

	t.Run("a Stop returned by a chained Entry ends the chain and Trigger returns no error", func(t *testing.T) {
		b := &TestState{
			name: "b",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Stop()
			},
		}
		a := &TestState{
			name: "a",
			entry: func(*EntryMachine, Event) state.Command {
				return state.Trigger(ChainEvent{})
			},
		}
		initial := &TestState{name: "initial"}
		g, err := state.NewGraph[State](
			initial,
			On[LoopEvent](initial, a),
			On[ChainEvent](a, b),
		)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(LoopEvent{}))
		assert.Equal(t, []Transition{
			{To: initial},
			{From: initial, To: a, Event: LoopEvent{}},
			{From: a, To: b, Event: ChainEvent{}},
			{From: b},
		}, tracer.calls)
		_, err = machine.CurrentState()
		assert.ErrorIs(t, err, state.ErrNotLaunched)
	})

	t.Run("a returned Stop cancels the timers of the visit", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.AfterFunc(dispatcher, time.Second, func(*AfterFuncMachine) {
					t.Error("a timer of a stopped visit must not run")
				}))
				return state.Stop()
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		assert.Equal(t, 0, machine.ActiveTimerCount())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
	})

	t.Run("the handle is valid inside the exit action of a returned Stop and its timer is canceled", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		var errs []error
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					errs = append(errs,
						m.OnExit(func(Event) *state.Guarded { return nil }),
						m.AfterFunc(dispatcher, time.Second, func(*AfterFuncMachine) {
							t.Error("a timer of a stopped visit must not run")
						}))
					return nil
				}))
				return state.Stop()
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())

		require.Len(t, errs, 2)
		assert.ErrorIs(t, errs[0], state.ErrExitActionRegistered)
		assert.NoError(t, errs[1])
		assert.Equal(t, 0, machine.ActiveTimerCount())
		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
	})
}

func TestMachine_Tracer(t *testing.T) {
	type NextEvent struct{}
	type UndefinedEvent struct{}

	t.Run("Trace records the initial transition on Launch", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		assert.Equal(t, []Transition{{From: nil, To: initial, Event: nil}}, tracer.calls)
	})

	t.Run("Trace records From, To and Event on a transition", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		require.Len(t, tracer.calls, 2)
		assert.Equal(t, Transition{From: initial, To: next, Event: NextEvent{}}, tracer.calls[1])
	})

	t.Run("a blocked transition is not traced", func(t *testing.T) {
		next := &TestState{name: "next"}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return &state.Guarded{Reason: errors.New("blocked")}
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.Error(t, machine.Trigger(NextEvent{}))
		assert.Equal(t, []Transition{{To: initial}}, tracer.calls)
	})

	t.Run("an event with no transition is not traced", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.Error(t, machine.Trigger(UndefinedEvent{}))
		assert.Equal(t, []Transition{{To: initial}}, tracer.calls)
	})

	t.Run("Trace records the last state and a zero To on Stop", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Stop())
		require.Len(t, tracer.calls, 2)
		assert.Equal(t, Transition{From: initial, To: nil, Event: nil, Guarded: nil}, tracer.calls[1])
	})

	t.Run("Trace records the Guarded a stop overrode", func(t *testing.T) {
		guarded := &state.Guarded{Reason: errors.New("blocked")}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return guarded
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Stop())
		require.Len(t, tracer.calls, 2)
		assert.Equal(t, Transition{From: initial, To: nil, Event: nil, Guarded: guarded}, tracer.calls[1])
	})

	t.Run("Trace runs after the exit action and before Entry", func(t *testing.T) {
		var order []string
		next := &TestState{
			name: "next",
			entry: func(*EntryMachine, Event) state.Command {
				order = append(order, "entry")
				return nil
			},
		}
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					order = append(order, "exit")
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		tracer := &funcTracer{f: func(Transition) {
			order = append(order, "trace")
		}}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())
		order = nil

		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.Equal(t, []string{"exit", "trace", "entry"}, order)
	})

	t.Run("Trace is recorded before Entry on Launch, even if Entry panics", func(t *testing.T) {
		initial := &TestState{
			name: "initial",
			entry: func(*EntryMachine, Event) state.Command {
				panic("entry failed")
			},
		}
		g, err := state.NewGraph[State](initial)
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))

		assert.Panics(t, func() {
			_ = machine.Launch()
		})
		assert.Equal(t, []Transition{{To: initial}}, tracer.calls)
	})

	t.Run("Trace is recorded before Entry on Trigger, even if Entry panics", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		next := &TestState{
			name: "next",
			entry: func(*EntryMachine, Event) state.Command {
				panic("entry failed")
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		tracer := &recordingTracer{}
		machine := state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		assert.Panics(t, func() {
			_ = machine.Trigger(NextEvent{})
		})
		require.Len(t, tracer.calls, 2)
		assert.Equal(t, Transition{From: initial, To: next, Event: NextEvent{}}, tracer.calls[1])
	})

	t.Run("Launch, Trigger and Stop from inside Trace are refused", func(t *testing.T) {
		var machine *state.Machine[State, *TestValue]
		var errs []error
		initial := &TestState{name: "initial"}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, next))
		require.NoError(t, err)

		tracer := &funcTracer{f: func(Transition) {
			errs = append(errs, machine.Launch(), machine.Trigger(NextEvent{}), machine.Stop())
		}}
		machine = state.NewMachine(g, &TestValue{}, state.WithTracer[State](tracer))
		require.NoError(t, machine.Launch())

		require.Len(t, errs, 3)
		assert.ErrorIs(t, errs[0], state.ErrInCallback)
		assert.ErrorIs(t, errs[1], state.ErrInTransition)
		assert.ErrorIs(t, errs[2], state.ErrInTransition)
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("Machine without tracer does not panic", func(t *testing.T) {
		guard := false
		initial := &TestState{
			name: "initial",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					if guard {
						return &state.Guarded{Reason: errors.New("blocked")}
					}
					return nil
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](initial, On[NextEvent](initial, initial))
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		guard = true
		require.Error(t, machine.Trigger(NextEvent{}))
		require.Error(t, machine.Trigger(UndefinedEvent{}))
		require.NoError(t, machine.Stop())
	})
}

// funcTracer reports every Transition to f.
type funcTracer struct {
	f func(t Transition)
}

func (r *funcTracer) Trace(t Transition) {
	r.f(t)
}

// stackDepth returns the number of frames above the caller, to compare the stack
// the machine runs a chained Entry on.
func stackDepth() int {
	var pcs [1024]uintptr
	return runtime.Callers(0, pcs[:])
}
