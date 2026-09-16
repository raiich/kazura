package state_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/task/eventloop"
)

// Verification policy (definition of done and falsification condition: state/machine_test.go):
//
//	Guaranteed: the shape of the graph NewGraph / On build (initial node, edges, wildcards),
//	  that NewGraph reports the graph.New errors the Machine relies on (unreachable state, an event
//	  declared twice from one state or as both an edge and a wildcard), and the Machine's
//	  behavior when leaving through a wildcard (OnExit, timer cancellation).
//	Not guaranteed: the rest of graph validation (state/graph's tests), precedence among
//	  three or more wildcards.
//	Not automated: none.
//	Strength check required: "a wildcard transition runs the exit action and cancels the visit's timers"

func TestNewGraph(t *testing.T) {
	type InputEvent struct{}
	type ResetEvent struct{}

	t.Run("edges and wildcards are reachable from the graph", func(t *testing.T) {
		state0 := &TestState{name: "state0"}
		state1 := &TestState{name: "state1"}

		g, err := state.NewGraph[State](
			state0,
			On[InputEvent](state0, state1),
			On[InputEvent](state1, state1),
			On[ResetEvent](nil, state0),
		)
		require.NoError(t, err)

		assert.Equal(t, state0, g.InitialNode.State)
		next, ok := g.InitialNode.FindNext(reflect.TypeFor[InputEvent]())
		require.True(t, ok)
		assert.Equal(t, state1, next.State)

		wild, ok := g.Wildcards.FindNext(reflect.TypeFor[ResetEvent]())
		require.True(t, ok)
		assert.Equal(t, state0, wild.State)
	})

	t.Run("an unreachable state returns an error", func(t *testing.T) {
		state0 := &TestState{name: "state0"}
		orphan := &TestState{name: "orphan"}

		_, err := state.NewGraph[State](state0, On[InputEvent](orphan, orphan))
		assert.ErrorContains(t, err, "orphan")
	})

	t.Run("the same event declared twice from one state returns an error", func(t *testing.T) {
		state0 := &TestState{name: "state0"}
		state1 := &TestState{name: "state1"}

		_, err := state.NewGraph[State](
			state0,
			On[InputEvent](state0, state1),
			On[InputEvent](state0, state1),
		)
		assert.ErrorContains(t, err, "already exists")
	})

	t.Run("an event declared both on a state and as a wildcard returns an error", func(t *testing.T) {
		state0 := &TestState{name: "state0"}
		state1 := &TestState{name: "state1"}

		_, err := state.NewGraph[State](
			state0,
			On[ResetEvent](state0, state1),
			On[ResetEvent](nil, state1),
		)
		assert.ErrorContains(t, err, "wildcard transition already exists")
	})
}

func TestMachine_Wildcard(t *testing.T) {
	type NextEvent struct{}
	type ResetEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("a wildcard applies when the current state has no edge for the event", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		next := &TestState{name: "next"}
		g, err := state.NewGraph[State](
			initial,
			On[NextEvent](initial, next),
			On[ResetEvent](nil, initial),
		)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		require.NoError(t, machine.Trigger(ResetEvent{}))
		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initial, current)
	})

	t.Run("a wildcard transition runs the exit action and cancels the visit's timers", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		initial := &TestState{name: "initial"}
		var exitEvents []Event
		var timerFired bool
		next := &TestState{
			name: "next",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(event Event) *state.Guarded {
					exitEvents = append(exitEvents, event)
					return nil
				}))
				require.NoError(t, m.AfterFunc(dispatcher, 10*time.Second, func(*AfterFuncMachine) {
					timerFired = true
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](
			initial,
			On[NextEvent](initial, next),
			On[ResetEvent](nil, initial),
		)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		require.NoError(t, machine.Trigger(ResetEvent{}))
		assert.Equal(t, []Event{ResetEvent{}}, exitEvents)

		require.NoError(t, dispatcher.FastForward(baseTime.Add(20*time.Second)))
		assert.False(t, timerFired)
	})

	t.Run("a wildcard transition can be blocked by the exit action", func(t *testing.T) {
		initial := &TestState{name: "initial"}
		blocked := errors.New("blocked")
		next := &TestState{
			name: "next",
			entry: func(m *EntryMachine, _ Event) state.Command {
				require.NoError(t, m.OnExit(func(Event) *state.Guarded {
					return &state.Guarded{Reason: blocked}
				}))
				return nil
			},
		}
		g, err := state.NewGraph[State](
			initial,
			On[NextEvent](initial, next),
			On[ResetEvent](nil, initial),
		)
		require.NoError(t, err)

		machine := state.NewMachine(g, &TestValue{})
		require.NoError(t, machine.Launch())
		require.NoError(t, machine.Trigger(NextEvent{}))

		err = machine.Trigger(ResetEvent{})
		var guarded *state.Guarded
		require.ErrorAs(t, err, &guarded)
		assert.Equal(t, blocked, guarded.Reason)

		current, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, next, current)
	})
}
