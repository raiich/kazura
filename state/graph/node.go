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
// It returns an error when the state is not comparable, or when a node of the
// same identity already carries a different state value.
func (r *nodeRegistry[S, E]) GetOrCreate(s S) (*Node[S, E], error) {
	// The value, not the type: an interface field may hold an uncomparable value.
	if !reflect.ValueOf(s).Comparable() {
		return nil, fmt.Errorf("uncomparable state %T", s)
	}
	node := r.getOrCreate(s)
	if any(node.State) != any(s) {
		return nil, fmt.Errorf("node %v already exists as %v", asStringer(s), asStringer(node.State))
	}
	return node, nil
}

// getOrCreate looks the node up by the state's Name when it implements Namer, and
// by its reflect.Type otherwise.
func (r *nodeRegistry[S, E]) getOrCreate(s S) *Node[S, E] {
	if namer, ok := any(s).(Namer); ok {
		name := namer.Name()
		if node, already := r.names[name]; already {
			return node
		}
		node := &Node[S, E]{State: s}
		r.names[name] = node
		return node
	}

	typ := reflect.TypeOf(s)
	if node, already := r.types[typ]; already {
		return node
	}
	node := &Node[S, E]{State: s}
	r.types[typ] = node
	return node
}

// Handle validates the edges and builds the nodes they connect. Wildcard edges
// (a nil From) are processed first, so a regular edge can be rejected for the
// event a wildcard already takes.
func (r *nodeRegistry[S, E]) Handle(edges []Edge[S, E]) error {
	var wilds []Edge[S, E]
	var es []Edge[S, E]

	for _, edge := range edges {
		from, to := edge.From, edge.To
		if any(to) == nil {
			transition := edge.Event
			return fmt.Errorf("invalid edge: node is nil (%v -> %v: %v)", from, to, transition)
		}
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

// handleWild registers a wildcard transition. An event may have only one
// wildcard destination.
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

// handle registers a transition between two nodes. The event must not already
// leave the source node, nor be taken by a wildcard.
func (r *nodeRegistry[S, E]) handle(transition E, from, to S) error {
	node, err := r.GetOrCreate(from)
	if err != nil {
		return fmt.Errorf("failed to get or create node %v: %w", asStringer(from), err)
	}
	toNode, err := r.GetOrCreate(to)
	if err != nil {
		return fmt.Errorf("failed to get or create node %v: %w", asStringer(to), err)
	}

	for _, t := range node.events {
		if t == transition {
			return fmt.Errorf("transition %v already exists for node %v", transition, asStringer(from))
		}
	}
	if _, already := r.wilds[transition]; already {
		return fmt.Errorf("wildcard transition already exists: %v", transition)
	}

	node.events = append(node.events, transition)
	node.nextNodes = append(node.nextNodes, toNode)
	return nil
}

// Namer identifies a state by a name instead of by its type, so several states
// of one type can be distinct nodes.
type Namer interface {
	Name() string
}

// asNamer returns s as a Namer, falling back to its reflect.Type.
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
