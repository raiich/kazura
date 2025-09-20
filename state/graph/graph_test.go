package graph_test

import (
	"reflect"
	"testing"

	"github.com/raiich/kazura/state/graph"
	"github.com/stretchr/testify/assert"
)

// TestNewGraph tests various scenarios of graph creation and validation.
func TestNewGraph(t *testing.T) {
	// Define event types for testing transitions
	type (
		Event0 struct{}
		Event1 struct{}
		Event2 struct{}
		Event3 struct{}
		Event4 struct{}
	)
	// Define state types for testing nodes
	type (
		State0 struct{}
		State1 struct{}
		State2 struct{}
		State3 struct{}
	)

	// Test basic graph creation with regular and wildcard transitions
	t.Run("basic graph", func(t *testing.T) {
		g, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State2{}),
			On[*Event2](State2{}, State1{}),
			On[*Event0](nil, State0{}),
			On[*Event4](nil, State0{}),
			On[*Event3](nil, State3{}),
			On[*Event2](State3{}, State1{}),
		)
		assert.NoError(t, err)
		expected := `stateDiagram-v2
  [*] --> State1
  * --> State0: Event0
  * --> State0: Event4
  * --> State3: Event3
  State1 --> State2: Event1
  State2 --> State1: Event2
  State3 --> State1: Event2`
		assert.Equal(t, expected, graph.Dump(g))
	})

	// Test detection of unreachable states that cannot be reached from initial state
	t.Run("unreachable states from initial state", func(t *testing.T) {
		_, err := NewGraph(
			State0{},
			On[*Event1](State1{}, State0{}),
			On[*Event1](State3{}, State1{}),
		)
		assert.ErrorContains(t, err, "unreachable nodes: [State1 State3]")
	})

	// Test detection of dangling states that are completely isolated
	t.Run("unreachable states (dangling)", func(t *testing.T) {
		type State4 = namerState

		_, err := NewGraph(
			State0{},
			On[*Event1](State0{}, State1{}),
			On[*Event1](State4{name: "s4"}, State1{}),
			On[*Event1](State4{name: "s4'"}, State1{}),
		)
		assert.ErrorContains(t, err, "unreachable nodes: [s4 s4']")
	})

	// Test error when same transition leads to different states from same node
	t.Run("duplicate transition with same event to different state", func(t *testing.T) {
		_, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State0{}),
			On[*Event1](State1{}, State2{}),
		)
		assert.ErrorContains(t, err, "already exists for node")
	})

	// Test error when same transition is defined multiple times to same state
	t.Run("duplicate transition with same event to same state", func(t *testing.T) {
		_, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State2{}),
			On[*Event1](State1{}, State2{}),
		)
		assert.ErrorContains(t, err, "already exists for node")
	})

	// Test error when wildcard transition conflicts with regular transition
	t.Run("duplicate wildcard transition with same event", func(t *testing.T) {
		_, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State2{}),
			On[*Event1](nil, State0{}),
		)
		assert.ErrorContains(t, err, "wildcard transition already exists")
	})

	// Test error when same state type is used with different values
	t.Run("same state type but not equal", func(t *testing.T) {
		type (
			myState1 struct{ x int }
			myState2 struct{ y int }
		)

		_, err := NewGraph(
			&myState1{},
			On[*Event1](&myState1{}, &myState2{}),
			On[*Event2](&myState1{}, &myState2{}),
		)
		assert.ErrorContains(t, err, "already exists as")

		s1, s2 := &myState1{}, &myState2{}
		_, err = NewGraph(
			s1,
			On[*Event1](s1, s2),
			On[*Event2](&myState1{}, s2),
		)
		assert.ErrorContains(t, err, "already exists as")

		_, err = NewGraph(
			s1,
			On[*Event1](s1, s2),
			On[*Event2](s1, &myState2{}),
		)
		assert.ErrorContains(t, err, "already exists as")
	})
}

// testState is a marker interface for state types in tests.
type testState interface {
}

// testEvent is a marker interface for event types in tests.
type testEvent interface {
}

// Type aliases for cleaner test code
type Edge = graph.Edge[testState, reflect.Type]
type Graph = graph.Graph[testState, reflect.Type]

// On creates an edge with the given event type as transition.
// Uses reflection to get the type of the event for the transition.
func On[E testEvent](from, to testState) Edge {
	return Edge{
		From:  from,
		Event: reflect.TypeOf([0]E{}).Elem(),
		To:    to,
	}
}

// NewGraph is a wrapper around graph.New for test convenience.
func NewGraph(init testState, edges ...Edge) (*Graph, error) {
	return graph.New(init, edges...)
}

// namerState is a test state type that implements the Namer interface.
type namerState struct {
	name string
}

// Name returns the custom name for this state.
func (s namerState) Name() string {
	if len(s.name) == 0 {
		return "<unnamed>"
	}
	return s.name
}
