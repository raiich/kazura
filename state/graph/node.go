package graph

import (
	"fmt"
	"reflect"
)

// Node represents a single state node in the Graph.
type Node[S State, E Event] struct {
	// State associated with this Node
	State S
	// events lists all events that can trigger transitions from this node
	events []E
	// nextNodes contains destination nodes corresponding to each event (parallel to events slice)
	nextNodes []*Node[S, E]
}

// FindNext returns the destination Node and true if a transition for the given event exists,
// otherwise returns nil and false.
func (n *Node[S, E]) FindNext(event E) (*Node[S, E], bool) {
	for i, e := range n.events {
		if e == event {
			return n.nextNodes[i], true
		}
	}
	return nil, false
}

// Wildcards represents global transitions that can be triggered from any node in the graph.
// These transitions provide a way to define common behaviors (like error handling or cancellation)
// that should be available regardless of the current state.
type Wildcards[S State, E Event] struct {
	// events lists all globally available transition events
	events []E
	// nextNodes contains destination nodes for each global event (parallel to events slice)
	nextNodes []*Node[S, E]
}

// FindNext returns the destination Node and true if a transition for the given event exists,
// otherwise returns nil and false.
func (n *Wildcards[S, E]) FindNext(event E) (*Node[S, E], bool) {
	for i, t := range n.events {
		if t == event {
			return n.nextNodes[i], true
		}
	}
	return nil, false
}

// nodeRegistry manages node creation and lookup during graph construction.
// It handles both named nodes (implementing Namer interface) and typed nodes.
type nodeRegistry[S State, E Event] struct {
	// Named nodes indexed by their Name() method
	names map[string]*Node[S, E]
	// Typed nodes indexed by their reflect.Type
	types map[reflect.Type]*Node[S, E]
	// Wildcard transition destinations with event
	wilds map[E]*Node[S, E]
}

// GetOrCreate retrieves an existing node or creates a new one for the given state.
// It enforces referential integrity by ensuring that the same logical node
// (identified by name or type) always contains the exact same state value.
// Returns an error if a node with the same identity but different state value already exists.
func (r *nodeRegistry[S, E]) GetOrCreate(s S) (*Node[S, E], error) {
	node := r.getOrCreate(s)
	if any(node.State) != any(s) {
		return nil, fmt.Errorf("node %v already exists as %v", asStringer(s), asStringer(node.State))
	}
	return node, nil
}

// getOrCreate is the internal method that implements the core node creation and lookup logic.
// For types implementing Namer interface, it uses the custom name as the unique identifier.
// For other types, it uses the Go reflect.Type as the identifier, ensuring type safety.
func (r *nodeRegistry[S, E]) getOrCreate(s S) *Node[S, E] {
	// Check if element implements Namer interface for custom naming
	// Named nodes are deduplicated by their Name() method
	if namer, ok := any(s).(Namer); ok {
		name := namer.Name()
		if node, already := r.names[name]; already {
			return node
		}
		node := &Node[S, E]{State: s}
		r.names[name] = node
		return node
	}

	// Use type-based lookup for non-named elements
	// Type-based nodes are deduplicated by their Go type
	typ := reflect.TypeOf(s)
	if node, already := r.types[typ]; already {
		return node
	}
	node := &Node[S, E]{State: s}
	r.types[typ] = node
	return node
}

// Handle processes and validates all provided edges, building the internal graph structure.
// It separates edges into wildcard transitions (From field is nil) and regular transitions,
// then processes each category with appropriate validation rules.
func (r *nodeRegistry[S, E]) Handle(edges []Edge[S, E]) error {
	var wilds []Edge[S, E]
	var es []Edge[S, E]

	// Separate wildcard and regular edges, validating destination nodes
	for _, edge := range edges {
		from, to := edge.From, edge.To
		// Validate that destination is not nil (source can be nil for wildcards)
		if any(to) == nil {
			transition := edge.Event
			return fmt.Errorf("invalid edge: node is nil (%v -> %v: %v)", from, to, transition)
		}
		// Categorize edge based on source: nil source indicates wildcard
		if any(from) == nil {
			wilds = append(wilds, edge)
		} else {
			es = append(es, edge)
		}
	}

	for _, edge := range wilds {
		if err := r.handleWild(edge.Event, edge.To); err != nil {
			return err
		}
	}
	for _, edge := range es {
		transition, from, to := edge.Event, edge.From, edge.To
		if err := r.handle(transition, from, to); err != nil {
			return err
		}
	}
	return nil
}

// handleWild processes a wildcard transition that can be triggered from any node in the graph.
// It validates that no duplicate wildcard events exist, as each wildcard event
// can only have one destination to maintain deterministic behavior.
func (r *nodeRegistry[S, E]) handleWild(transition E, to S) error {
	node, err := r.GetOrCreate(to)
	if err != nil {
		return fmt.Errorf("failed to get or create node %v: %w", asStringer(to), err)
	}
	if _, already := r.wilds[transition]; already {
		return fmt.Errorf("wildcard transition already exists: %v", transition)
	}
	r.wilds[transition] = node
	return nil
}

// handle processes a regular transition between two specific nodes.
// It validates that:
// - No duplicate events exist from the same source node
// - The event doesn't conflict with existing wildcard transitions
// This ensures deterministic transition behavior.
func (r *nodeRegistry[S, E]) handle(transition E, from, to S) error {
	node, err := r.GetOrCreate(from)
	if err != nil {
		return fmt.Errorf("failed to get or create node %v: %w", asStringer(from), err)
	}
	toNode, err := r.GetOrCreate(to)
	if err != nil {
		return fmt.Errorf("failed to get or create node %v: %w", asStringer(to), err)
	}

	// Check for duplicate transitions from the same node
	// Each node can only have one transition per event to maintain determinism
	for _, t := range node.events {
		if t == transition {
			return fmt.Errorf("transition %v already exists for node %v", transition, asStringer(from))
		}
	}
	// Ensure transition doesn't conflict with wildcard transitions
	// Wildcards take precedence, so regular transitions cannot override them
	if _, already := r.wilds[transition]; already {
		return fmt.Errorf("wildcard transition already exists: %v", transition)
	}

	node.events = append(node.events, transition)
	node.nextNodes = append(node.nextNodes, toNode)
	return nil
}

// Namer interface allows custom naming of graph elements.
// Elements implementing this interface will be identified by their Name() rather than their type.
type Namer interface {
	Name() string
}

// asNamer converts any value to a Namer interface.
// If the value implements Namer, returns it directly; otherwise uses its reflect.Type.
func asNamer(s any) Namer {
	if namer, ok := s.(Namer); ok {
		return namer
	}
	typ := reflect.TypeOf(s)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

// asStringer converts any value to a fmt.Stringer for consistent string representation.
func asStringer(s any) fmt.Stringer {
	return namerStringer{
		Namer: asNamer(s),
	}
}

// namerStringer adapts a Namer to fmt.Stringer interface.
type namerStringer struct {
	Namer
}

// String returns the name from the embedded Namer.
func (n namerStringer) String() string {
	return n.Namer.Name()
}
