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

// TestState is a test implementation of the State interface
type TestState struct {
	name  string
	entry func(machine *EntryMachine, event Event)
}

func (s *TestState) Name() string {
	return s.name
}

func (s *TestState) Entry(machine *EntryMachine, event Event) {
	if s.entry != nil {
		s.entry(machine, event)
	}
}

// On creates a state transition edge for testing
func On[E Event](from, to State) state.Edge[State] {
	return state.On[State, E](from, to)
}
