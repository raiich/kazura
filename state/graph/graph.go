// Package graph provides a generic, type-safe graph data structure
// optimized for finite state machines and workflow modeling.
//
// This package supports creating directed graphs with typed nodes and transitions,
// including wildcard transitions that can be triggered from any node.
//
// # Basic Usage
//
//	type (
//		LoginState struct{}
//		LoggedInState struct{}
//		LoginEvent struct{}
//	)
//
//	g, err := graph.New(
//		LoginState{},
//		&graph.Edge[any, reflect.Type]{
//			From: LoginState{},
//			Event: reflect.TypeOf(LoginEvent{}),
//			To: LoggedInState{},
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
// Global transitions available from any state using nil as the source:
//
//	&graph.Edge[any, reflect.Type]{
//		From: nil,  // nil indicates wildcard
//		Event: reflect.TypeOf(ErrorEvent{}),
//		To: ErrorState{},
//	}
//
// # Type Safety
//
// The package uses Go generics to ensure type safety:
// - S: any type for state
// - E: comparable type for transition conditions
//
// States implementing the Namer interface will be identified by their Name()
// method rather than their type, allowing multiple instances of the same type.
package graph

import (
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

// Graph represents a directed state transition graph with typed nodes and transitions.
// It supports both regular transitions between specific nodes and wildcard transitions
// that can be triggered from any node in the graph.
type Graph[S State, E Event] struct {
	// InitialNode is the starting Node of the Graph
	InitialNode *Node[S, E]
	// Wildcards contains global transitions from any Node
	Wildcards Wildcards[S, E]
}

// getEdges converts a Graph back to its Edge representation.
// This is useful for dumpers that work with edge lists rather than the graph structure.
func (g *Graph[S, E]) getEdges() []Edge[S, E] {
	var ret []Edge[S, E]
	var node *Node[S, E]

	// Process wildcard transitions (transitions with nil From)
	for i, event := range g.Wildcards.events {
		ret = append(ret, Edge[S, E]{
			Event: event,
			To:    g.Wildcards.nextNodes[i].State,
		})
	}

	visited := make(map[*Node[S, E]]bool)
	queue := []*Node[S, E]{g.InitialNode}
	// Add wildcard destination nodes to queue for traversal
	queue = append(queue, g.Wildcards.nextNodes...)

	// Traverse all reachable nodes and collect their edges
	for len(queue) > 0 {
		node, queue = queue[0], queue[1:]
		if visited[node] {
			continue
		}
		visited[node] = true
		queue = append(queue, node.nextNodes...)
		// Convert each transition to an edge
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

// New creates a new Graph with the given initial state and edges.
// Returns an error if validation fails.
func New[S State, E Event](init S, edges ...Edge[S, E]) (*Graph[S, E], error) {
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

	// Perform reachability analysis to detect unreachable nodes
	// This ensures that every defined node can be reached through some path
	visited := make(map[*Node[S, E]]bool)
	queue := []*Node[S, E]{initialNode}

	// Mark wildcard destinations as reachable since they can be accessed from any node
	for _, node := range registry.wilds {
		if visited[node] {
			continue
		}
		visited[node] = true
		queue = append(queue, node.nextNodes...)
	}

	// Breadth-first traversal to mark all reachable nodes
	var head *Node[S, E]
	for len(queue) > 0 {
		head, queue = queue[0], queue[1:]
		if visited[head] {
			continue
		}
		visited[head] = true
		// Add all destination nodes of this node to the queue
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
