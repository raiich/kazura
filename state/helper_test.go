package state_test

import (
	"github.com/raiich/kazura/state"
)

// Common test types and utilities used across all state package tests

// TestValue represents test data managed by the state machine
type TestValue struct {
	Map map[string]any
}

// Type aliases for cleaner test code
type State = state.State[*TestValue]
type Event = state.Event
type EntryMachine = state.EntryMachine[*TestValue]
type AfterFuncMachine = state.AfterFuncMachine[*TestValue]
type Transition = state.Transition[State]

// TestState is a test implementation of the State interface
type TestState struct {
	name  string
	entry func(machine *EntryMachine, event Event) state.Command
}

func (s *TestState) Name() string {
	return s.name
}

func (s *TestState) Entry(machine *EntryMachine, event Event) state.Command {
	if s.entry == nil {
		return nil
	}
	return s.entry(machine, event)
}

// On creates a state transition edge for testing
func On[E Event](from, to State) state.Edge[State] {
	return state.On[State, E](from, to)
}

// recordingTracer records every Transition it observes, so a test can assert on
// the sequence of transitions and stops.
type recordingTracer struct {
	calls []Transition
}

func (r *recordingTracer) Trace(t Transition) {
	r.calls = append(r.calls, t)
}
