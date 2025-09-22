package state

import (
	"reflect"

	"github.com/raiich/kazura/state/graph"
)

type Edge[S any] graph.Edge[S, reflect.Type]

// NewGraph creates a new State Graph for state.Machine.
// It wraps the generic graph.New with reflect.Type as the event type,
// which is used for event-based state transitions.
// The init parameter specifies the initial state, and edges define the valid transitions.
// Returns an error if the graph structure is invalid (e.g., unreachable states).
func NewGraph[S any](init S, edges ...Edge[S]) (*graph.Graph[S, reflect.Type], error) {
	var es []graph.Edge[S, reflect.Type]
	for _, edge := range edges {
		es = append(es, graph.Edge[S, reflect.Type](edge))
	}
	return graph.New[S, reflect.Type](init, es...)
}

// On creates a state transition edge from one state to another, triggered by an event of type E.
// This is a convenience function for creating graph edges with type-based transitions.
// The transition is identified by the reflect.Type of E, allowing type-safe event handling.
// Use nil as the 'from' parameter to create wildcard transitions that work from any state.
//
// Example:
//
//	On[MyState, StartEvent](MenuState{}, GameState{})  // MenuState -> GameState on StartEvent
//	On[MyState, QuitEvent](nil, MenuState{})           // Any state -> MenuState on QuitEvent
func On[S, T any](from, to S) Edge[S] {
	return Edge[S]{
		From:  from,
		Event: reflect.TypeOf([0]T{}).Elem(),
		To:    to,
	}
}
