// Package main provides type aliases and helper functions for the vending machine example.
package main

import (
	"fmt"

	"github.com/raiich/kazura/state"
)

// Type aliases for vending machine state machine types

type State = state.State[*VendingMachine]
type Event = state.Event

type EntryMachine = state.EntryMachine[*VendingMachine]
type ExitMachine = state.ExitMachine[*VendingMachine]
type AfterEntryMachine = state.AfterEntryMachine[*VendingMachine]
type AfterFuncMachine = state.AfterFuncMachine[*VendingMachine]

// On creates a state transition edge for the given event type.
func On[E Event](from, to State) state.Edge[State] {
	return state.On[State, E](from, to)
}

// Guarded creates a guard condition with a formatted error message.
func Guarded(format string, args ...any) *state.Guarded {
	return &state.Guarded{
		Reason: fmt.Errorf(format, args...),
	}
}
