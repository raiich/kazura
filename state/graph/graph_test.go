package graph_test

import (
	"reflect"
	"testing"

	"github.com/raiich/kazura/state/graph"
	"github.com/stretchr/testify/assert"
)

func TestNewGraph(t *testing.T) {
	type (
		Event0 struct{}
		Event1 struct{}
		Event2 struct{}
		Event3 struct{}
		Event4 struct{}
	)
	type (
		State0 struct{}
		State1 struct{}
		State2 struct{}
		State3 struct{}
	)

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

	t.Run("unreachable states from initial state", func(t *testing.T) {
		_, err := NewGraph(
			State0{},
			On[*Event1](State1{}, State0{}),
			On[*Event1](State3{}, State1{}),
		)
		assert.ErrorContains(t, err, "unreachable nodes: [State1 State3]")
	})

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

	t.Run("duplicate transition with same event to different state", func(t *testing.T) {
		_, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State0{}),
			On[*Event1](State1{}, State2{}),
		)
		assert.ErrorContains(t, err, "already exists for node")
	})

	t.Run("duplicate transition with same event to same state", func(t *testing.T) {
		_, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State2{}),
			On[*Event1](State1{}, State2{}),
		)
		assert.ErrorContains(t, err, "already exists for node")
	})

	t.Run("duplicate wildcard transition with same event", func(t *testing.T) {
		_, err := NewGraph(
			State1{},
			On[*Event1](State1{}, State2{}),
			On[*Event1](nil, State0{}),
		)
		assert.ErrorContains(t, err, "wildcard transition already exists")
	})

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

func TestGraph_FindNext(t *testing.T) {
	type (
		Event1 struct{}
		Event2 struct{}
		Event3 struct{}
	)
	type (
		State0 struct{}
		State1 struct{}
		State2 struct{}
	)
	g, err := NewGraph(
		State1{},
		On[*Event1](State1{}, State2{}),
		On[*Event2](nil, State0{}),
	)
	assert.NoError(t, err)

	t.Run("the node's own transition", func(t *testing.T) {
		next, found := g.FindNext(g.InitialNode, reflect.TypeFor[*Event1]())
		assert.True(t, found)
		assert.Equal(t, State2{}, next.State)
	})

	t.Run("a wildcard transition", func(t *testing.T) {
		next, found := g.FindNext(g.InitialNode, reflect.TypeFor[*Event2]())
		assert.True(t, found)
		assert.Equal(t, State0{}, next.State)
	})

	t.Run("no transition", func(t *testing.T) {
		next, found := g.FindNext(g.InitialNode, reflect.TypeFor[*Event3]())
		assert.False(t, found)
		assert.Nil(t, next)
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
