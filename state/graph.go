package state

import (
	"reflect"

	"github.com/raiich/kazura/state/graph"
)

// Edge is a transition of a state graph, keyed by the event's [reflect.Type].
type Edge[S any] graph.Edge[S, reflect.Type]

// NewGraph builds the graph of a [Machine] from the initial state and its edges.
// It returns the errors [graph.New] returns.
func NewGraph[S any](init S, edges ...Edge[S]) (*graph.Graph[S, reflect.Type], error) {
	var es []graph.Edge[S, reflect.Type]
	for _, edge := range edges {
		es = append(es, graph.Edge[S, reflect.Type](edge))
	}
	return graph.New[S, reflect.Type](init, es...)
}

// On creates the edge from one state to another for events of type T. A nil from
// makes it a wildcard edge, taken from any state.
//
// Example:
//
//	On[MyState, StartEvent](MenuState{}, GameState{})  // MenuState -> GameState on StartEvent
//	On[MyState, QuitEvent](nil, MenuState{})           // any state -> MenuState on QuitEvent
func On[S, T any](from, to S) Edge[S] {
	return Edge[S]{
		From:  from,
		Event: reflect.TypeOf([0]T{}).Elem(),
		To:    to,
	}
}
