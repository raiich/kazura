// Package graph provides a generic, type-safe graph data structure
// optimized for finite state machines and workflow modeling.
//
// A node is identified by the type of its state, or by its Name when the state
// implements [Namer], so several nodes can share a type.
//
// # Basic Usage
//
//	type (
//		LoginState    struct{}
//		LoggedInState struct{}
//		LoginEvent    struct{}
//	)
//
//	g, err := graph.New[any, reflect.Type](
//		LoginState{},
//		graph.Edge[any, reflect.Type]{
//			From:  LoginState{},
//			Event: reflect.TypeOf(LoginEvent{}),
//			To:    LoggedInState{},
//		},
//	)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// # Mermaid Diagram Output
//
// The graph can be visualized using Mermaid state diagrams:
//
//	fmt.Println(graph.Dump(g))
//	// Output:
//	// stateDiagram-v2
//	//   [*] --> LoginState
//	//   LoginState --> LoggedInState: LoginEvent
//
// # Wildcard Transitions
//
// An edge with a nil From is taken from any node:
//
//	graph.Edge[any, reflect.Type]{
//		From:  nil,
//		Event: reflect.TypeOf(ErrorEvent{}),
//		To:    ErrorState{},
//	}
package graph

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// State represents any type that can be used as a graph node state.
// States can optionally implement the Namer interface for custom identification.
type State any

// Event represents any comparable type that can trigger state transitions.
// Common examples include reflect.Type for event types or string/int for simple events.
type Event comparable

// Graph is a directed state transition graph with typed nodes and transitions.
type Graph[S State, E Event] struct {
	// InitialNode is the starting Node of the Graph
	InitialNode *Node[S, E]
	// Wildcards contains global transitions from any Node
	Wildcards Wildcards[S, E]
}

// FindNext returns the destination of event from node and true when a transition
// exists: the node's own transition, or else the wildcard one.
func (g *Graph[S, E]) FindNext(node *Node[S, E], event E) (*Node[S, E], bool) {
	if next, found := node.FindNext(event); found {
		return next, true
	}
	return g.Wildcards.FindNext(event)
}

// getEdges converts a Graph back to its Edge representation.
func (g *Graph[S, E]) getEdges() []Edge[S, E] {
	var ret []Edge[S, E]
	var node *Node[S, E]

	for i, event := range g.Wildcards.events {
		ret = append(ret, Edge[S, E]{
			Event: event,
			To:    g.Wildcards.nextNodes[i].State,
		})
	}

	visited := make(map[*Node[S, E]]bool)
	queue := []*Node[S, E]{g.InitialNode}
	queue = append(queue, g.Wildcards.nextNodes...)

	for len(queue) > 0 {
		node, queue = queue[0], queue[1:]
		if visited[node] {
			continue
		}
		visited[node] = true
		queue = append(queue, node.nextNodes...)
		for i, event := range node.events {
			ret = append(ret, Edge[S, E]{
				From:  node.State,
				Event: event,
				To:    node.nextNodes[i].State,
			})
		}
	}

	return ret
}

// New creates a Graph from the initial state and the edges. It returns an error
// when an edge has a nil To, when a state is not comparable, when two edges
// share an event from the same node or as a wildcard, when one node identity
// carries different state values, or when a node is unreachable from the
// initial node.
func New[S State, E Event](init S, edges ...Edge[S, E]) (*Graph[S, E], error) {
	if any(init) == nil {
		return nil, errors.New("initial node is nil")
	}
	registry := &nodeRegistry[S, E]{
		names: make(map[string]*Node[S, E]),
		types: make(map[reflect.Type]*Node[S, E]),
		wilds: make(map[E]*Node[S, E]),
	}
	initialNode, err := registry.GetOrCreate(init)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial node: %w", err)
	}
	if err := registry.Handle(edges); err != nil {
		return nil, fmt.Errorf("failed to handle edges: %w", err)
	}

	visited := make(map[*Node[S, E]]bool)
	queue := []*Node[S, E]{initialNode}

	// Wildcard destinations are reachable from every node, so they seed the traversal.
	for _, node := range registry.wilds {
		if visited[node] {
			continue
		}
		visited[node] = true
		queue = append(queue, node.nextNodes...)
	}

	var head *Node[S, E]
	for len(queue) > 0 {
		head, queue = queue[0], queue[1:]
		if visited[head] {
			continue
		}
		visited[head] = true
		queue = append(queue, head.nextNodes...)
	}

	var unreachableNodes []string
	for _, node := range registry.names {
		if visited[node] {
			continue
		}
		unreachableNodes = append(unreachableNodes, asNamer(node.State).Name())
	}
	for _, node := range registry.types {
		if visited[node] {
			continue
		}
		unreachableNodes = append(unreachableNodes, asNamer(node.State).Name())
	}
	if len(unreachableNodes) > 0 {
		sort.Slice(unreachableNodes, func(i, j int) bool {
			return unreachableNodes[i] < unreachableNodes[j]
		})
		return nil, fmt.Errorf("unreachable nodes: %v", unreachableNodes)
	}

	ret := &Graph[S, E]{
		InitialNode: initialNode,
	}
	for typ, node := range registry.wilds {
		ret.Wildcards.events = append(ret.Wildcards.events, typ)
		ret.Wildcards.nextNodes = append(ret.Wildcards.nextNodes, node)
	}
	return ret, nil
}

// Edge represents a directed state transition in the graph.
type Edge[S State, E Event] struct {
	// From is the source state of this transition (nil indicates a wildcard transition)
	From S
	// Event is the trigger condition that causes this transition to occur
	Event E
	// To is the destination state reached when this transition is triggered
	To S
}
