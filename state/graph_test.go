package state_test

import (
	"reflect"
	"testing"

	"github.com/raiich/kazura/state"
	"github.com/stretchr/testify/assert"
)

func TestNewGraph(t *testing.T) {
	state0 := &TestState{name: "state0"}
	state1 := &TestState{name: "state1"}
	type InputEvent struct{}
	type ResetEvent struct{}

	t.Run("basic", func(t *testing.T) {
		g, err := state.NewGraph[State](
			state0,
			On[InputEvent](state0, state1),
			On[InputEvent](state1, state1),
			On[ResetEvent](nil, state0),
		)
		assert.NoError(t, err)
		{
			assert.Equal(t, state0, g.InitialNode.State)
			next, ok := g.InitialNode.FindNext(reflect.TypeOf(InputEvent{}))
			assert.True(t, ok)
			assert.Equal(t, state1, next.State)
		}
		{
			next, ok := g.Wildcards.FindNext(reflect.TypeOf(ResetEvent{}))
			assert.True(t, ok)
			assert.Equal(t, state0, next.State)
			node2, ok := next.FindNext(reflect.TypeOf(InputEvent{}))
			assert.True(t, ok)
			assert.Equal(t, state1, node2.State)
		}
	})
}

func TestSampleCode(t *testing.T) {
	type MyState interface{}
	type MenuState struct{}
	type GameState struct{}
	type StartEvent struct{}
	type QuitEvent struct{}

	t.Run("basic", func(t *testing.T) {
		g, err := state.NewGraph[MyState](
			MenuState{},
			state.On[MyState, StartEvent](MenuState{}, GameState{}),
			state.On[MyState, QuitEvent](nil, MenuState{}),
		)
		assert.NoError(t, err)
		{
			assert.Equal(t, MenuState{}, g.InitialNode.State)
			next, ok := g.InitialNode.FindNext(reflect.TypeOf(StartEvent{}))
			assert.True(t, ok)
			assert.Equal(t, GameState{}, next.State)
		}
		{
			next, ok := g.Wildcards.FindNext(reflect.TypeOf(QuitEvent{}))
			assert.True(t, ok)
			assert.Equal(t, MenuState{}, next.State)
			node2, ok := next.FindNext(reflect.TypeOf(StartEvent{}))
			assert.True(t, ok)
			assert.Equal(t, GameState{}, node2.State)
		}
	})
}
