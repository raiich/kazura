package state_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/task/eventloop"
)

func TestNewMachine(t *testing.T) {

	t.Run("NewMachine with nil graph panics", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		assert.Panics(t, func() {
			state.NewMachine[State](nil, value)
		})
	})

	t.Run("NewMachine with valid parameters succeeds", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}
		graph, err := state.NewGraph(&TestState{name: "initial"})
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		assert.NotNil(t, machine)
	})
}

func TestMachine_LaunchAndStop(t *testing.T) {
	t.Run("Complete lifecycle flow", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}
		initialState := &TestState{name: "initial"}
		graph, err := state.NewGraph(initialState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)

		// Phase 1: Before launch
		_, err = machine.CurrentState()
		assert.ErrorContains(t, err, "not launched")

		// Phase 2: Launch
		require.NoError(t, machine.Launch())

		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, initialState, currentState)

		// Phase 3: Duplicate launch should fail
		err = machine.Launch() //
		assert.ErrorContains(t, err, "already launched")

		// Phase 4: Stop
		err = machine.Stop() //
		assert.NoError(t, err)

		// Phase 5: After stop
		_, err = machine.CurrentState()
		assert.ErrorContains(t, err, "not launched")

		// Phase 6: Duplicate stop should fail
		err = machine.Stop() //
		assert.ErrorContains(t, err, "already stopped")
	})

}

func TestMachine_Trigger(t *testing.T) {
	type StartEvent struct{}
	type NextEvent struct{}

	t.Run("Trigger behavior and validation", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		initialState := &TestState{name: "initial"}
		nextState := &TestState{name: "next"}

		graph, err := state.NewGraph[State](
			initialState,
			On[NextEvent](initialState, nextState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)

		// Error case 1: Before launch
		err = machine.Trigger(StartEvent{})
		assert.ErrorContains(t, err, "not launched")

		// Launch the machine
		require.NoError(t, machine.Launch())

		// Error case 2: Nil event
		err = machine.Trigger(nil) //
		assert.ErrorContains(t, err, "event cannot be nil")

		// Error case 3: Invalid event (no transition defined)
		err = machine.Trigger(StartEvent{}) //
		assert.ErrorContains(t, err, "no transition found")

		// Success case: Valid transition
		require.NoError(t, machine.Trigger(NextEvent{}))

		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, nextState, currentState)

		// Error case 4: No transition from next state
		err = machine.Trigger(NextEvent{}) //
		assert.ErrorContains(t, err, "no transition found")
	})
}

func TestEntryMachine_Value(t *testing.T) {
	t.Run("Value returns machine data", func(t *testing.T) {
		expectedData := map[string]any{"key": "value", "number": 123}
		value := &TestValue{Map: expectedData}

		var retrievedValue *TestValue
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				retrievedValue = machine.Value()
			},
		}

		graph, err := state.NewGraph(testState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		assert.Equal(t, value, retrievedValue)
		assert.Equal(t, expectedData, retrievedValue.Map)
	})
}

func TestEntryMachine_AfterFunc(t *testing.T) {
	type TimerEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("AfterFunc schedules timer correctly", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var timerExecuted bool
		timerState := &TestState{
			name: "timer",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 5*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
					timerExecuted = true
				})
			},
		}

		graph, err := state.NewGraph(timerState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())
		// Timer should not have executed yet
		assert.False(t, timerExecuted)

		// Fast forward time
		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		// Timer should have executed
		assert.True(t, timerExecuted)
	})

	t.Run("Timer cancellation on state transition", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var timerExecuted bool
		timerState := &TestState{
			name: "timer",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 10*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
					timerExecuted = true
				})
			},
		}
		nextState := &TestState{name: "next"}

		graph, err := state.NewGraph[State](
			timerState,
			On[TimerEvent](timerState, nextState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Transition to next state before timer fires
		require.NoError(t, machine.Trigger(TimerEvent{}))

		// Fast forward past timer time
		require.NoError(t, dispatcher.FastForward(baseTime.Add(15*time.Second)))
		// Timer should not have executed due to state transition
		assert.False(t, timerExecuted)
	})

	t.Run("EntryMachine.AfterFunc is called from AfterEntry's callback and timer is fired", func(t *testing.T) {
		// Trigger is not called in AfterFunc's callback
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var timerExecuted bool
		var afterEntryExecuted bool
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(_ *state.AfterEntryMachine[*TestValue]) {
					afterEntryExecuted = true
					machine.AfterFunc(dispatcher, 3*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
						timerExecuted = true
					})
				})
				require.NoError(t, err)
			},
		}

		graph, err := state.NewGraph(testState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// AfterEntry should have executed
		assert.True(t, afterEntryExecuted)
		// Timer should not have executed yet
		assert.False(t, timerExecuted)

		// Fast forward to execute timer
		require.NoError(t, dispatcher.FastForward(baseTime.Add(3*time.Second)))
		// Timer should have executed
		assert.True(t, timerExecuted)
	})

	t.Run("EntryMachine.AfterFunc is called from AfterEntry's callback and timer is cancelled", func(t *testing.T) {
		// Trigger is called in AfterFunc's callback
		type NextEvent struct{}
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var timerExecuted bool
		var afterEntryExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(_ *state.AfterEntryMachine[*TestValue]) {
					afterEntryExecuted = true
					machine.AfterFunc(dispatcher, 5*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
						timerExecuted = true
					})
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// AfterEntry should have executed
		assert.True(t, afterEntryExecuted)
		// Timer should not have executed yet
		assert.False(t, timerExecuted)

		// Trigger transition before timer fires
		require.NoError(t, machine.Trigger(NextEvent{}))

		// Fast forward past timer time
		require.NoError(t, dispatcher.FastForward(baseTime.Add(10*time.Second)))
		// Timer should not have executed due to state transition
		assert.False(t, timerExecuted)
	})

	t.Run("EntryMachine.AfterFunc is called from OnExit's callback and timer is cancelled", func(t *testing.T) {
		// OnExit returns nil
		type NextEvent struct{}
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var timerExecuted bool
		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(_ *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					exitExecuted = true
					machine.AfterFunc(dispatcher, 3*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
						timerExecuted = true
					})
					return nil // Allow transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger transition
		require.NoError(t, machine.Trigger(NextEvent{}))
		// Exit should have executed
		assert.True(t, exitExecuted)
		// Timer should not have executed yet
		assert.False(t, timerExecuted)

		// Fast forward to timer time
		require.NoError(t, dispatcher.FastForward(baseTime.Add(5*time.Second)))
		// Timer should not have executed due to state transition (timers are cancelled on state change)
		assert.False(t, timerExecuted)
	})

	t.Run("EntryMachine.AfterFunc is called from OnExit's callback and timer is fired", func(t *testing.T) {
		// OnExit returns Guarded
		type NextEvent struct{}
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var timerExecuted bool
		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(_ *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					exitExecuted = true
					machine.AfterFunc(dispatcher, 2*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
						timerExecuted = true
					})
					return &state.Guarded{Reason: errors.New("transition blocked")} // Block transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger transition (should be blocked)
		err = machine.Trigger(NextEvent{})
		assert.ErrorContains(t, err, "transition blocked")
		// Exit should have executed
		assert.True(t, exitExecuted)
		// Timer should not have executed yet
		assert.False(t, timerExecuted)

		// Should still be in fromState
		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, fromState, currentState)

		// Fast forward to timer time
		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		// Timer should have executed since transition was blocked
		assert.True(t, timerExecuted)
	})
}

func TestEntryMachine_AfterEntry(t *testing.T) {
	type TriggerEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("AfterEntry callback executes after Entry", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var executionOrder []string
		initialState := &TestState{name: "initial"}
		callbackState := &TestState{
			name: "callback",
			entry: func(machine *EntryMachine, event Event) {
				executionOrder = append(executionOrder, "entry")
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
					executionOrder = append(executionOrder, "after-entry")
				})
				require.NoError(t, err)
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[TriggerEvent](initialState, callbackState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger to transition to callback state
		require.NoError(t, machine.Trigger(TriggerEvent{}))
		// Execution order should be correct
		assert.Equal(t, []string{"entry", "after-entry"}, executionOrder)
	})

	t.Run("Multiple AfterEntry callbacks not allowed", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var entryExecuted bool
		initialState := &TestState{name: "initial"}
		callbackState := &TestState{
			name: "callback",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {})
				require.NoError(t, err)

				err = machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {})
				assert.ErrorContains(t, err, "callback for AfterEntry already registered")
				entryExecuted = true
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[TriggerEvent](initialState, callbackState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(TriggerEvent{}))
		assert.True(t, entryExecuted)
	})

	t.Run("EntryMachine.AfterEntry is called from AfterFunc's callback and error", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var afterFuncExecuted bool
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 1*time.Second, func(_ *state.AfterFuncMachine[*TestValue]) {
					// AfterEntry call should have failed
					err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {})
					assert.ErrorContains(t, err, "AfterEntry is not callable here")
					afterFuncExecuted = true
				})
			},
		}

		graph, err := state.NewGraph(testState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Fast forward to execute AfterFunc
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		// AfterFunc should have executed
		assert.True(t, afterFuncExecuted)
	})

	t.Run("EntryMachine.AfterEntry is called from AfterEntry's callback and error", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var afterEntryExecuted bool
		initialState := &TestState{name: "initial"}
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(_ *state.AfterEntryMachine[*TestValue]) {
					// AfterEntry call should have failed
					err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {})
					assert.ErrorContains(t, err, "AfterEntry is not callable here")
					afterEntryExecuted = true
				})
				require.NoError(t, err)
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[TriggerEvent](initialState, testState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(TriggerEvent{}))
		// AfterEntry should have executed
		assert.True(t, afterEntryExecuted)
	})

	t.Run("EntryMachine.AfterEntry is called from OnExit's callback and error", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(_ *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					// AfterEntry call should have failed
					err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {})
					assert.ErrorContains(t, err, "AfterEntry is not callable here")
					exitExecuted = true
					return &state.Guarded{Reason: errors.New("transition blocked")} // Block transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[TriggerEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger transition (should be blocked)
		err = machine.Trigger(TriggerEvent{})
		assert.ErrorContains(t, err, "transition blocked")
		// Exit should have executed
		assert.True(t, exitExecuted)
	})
}

func TestEntryMachine_OnExit(t *testing.T) {
	type TransitionEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("OnExit callback executes before transition", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var executionOrder []string
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				executionOrder = append(executionOrder, "from-entry")
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					executionOrder = append(executionOrder, "exit")
					return nil // Allow transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{
			name: "to",
			entry: func(machine *EntryMachine, event Event) {
				executionOrder = append(executionOrder, "to-entry")
			},
		}

		graph, err := state.NewGraph[State](
			fromState,
			On[TransitionEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(TransitionEvent{}))
		// Execution order should be correct
		assert.Equal(t, []string{"from-entry", "exit", "to-entry"}, executionOrder)
	})

	t.Run("OnExit guard condition prevents transition", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					return &state.Guarded{Reason: errors.New("transition blocked")}
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[TransitionEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		err = machine.Trigger(TransitionEvent{})
		assert.ErrorContains(t, err, "transition blocked")

		// Should still be in the original state
		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, fromState, currentState)
	})

	t.Run("Multiple OnExit callbacks not allowed", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var entryExecuted bool
		exitState := &TestState{
			name: "exit",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					return nil
				})
				require.NoError(t, err)

				err = machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					return nil
				})
				assert.ErrorContains(t, err, "exit callback already registered")
				entryExecuted = true
			},
		}

		graph, err := state.NewGraph(exitState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())
		assert.True(t, entryExecuted)
	})

	t.Run("Guarded with nil Reason", func(t *testing.T) {
		type GuardEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					return &state.Guarded{Reason: nil} // nil reason
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[GuardEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		err = machine.Trigger(GuardEvent{})
		assert.Error(t, err)
		assert.Equal(t, "state transition blocked", err.Error()) // Default message for nil Reason

		// Should still be in original state
		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, fromState, currentState)
	})

	t.Run("Guard callback receives correct event", func(t *testing.T) {
		type SpecialEvent struct{ data string }
		value := &TestValue{Map: make(map[string]any)}

		var receivedEvent Event
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], receivedEvt Event) *state.Guarded {
					receivedEvent = receivedEvt
					return &state.Guarded{Reason: errors.New("blocked")}
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[SpecialEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger with specific event
		triggerEvent := SpecialEvent{data: "test"}
		err = machine.Trigger(triggerEvent)
		assert.ErrorContains(t, err, "blocked")
		// Guard should have received the same event
		assert.Equal(t, triggerEvent, receivedEvent)
	})

	t.Run("EntryMachine.OnExit is called from AfterFunc's callback and success", func(t *testing.T) {
		type NextEvent struct{}
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var afterFuncExecuted bool
		var exitExecuted bool
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 1*time.Second, func(_ *state.AfterFuncMachine[*TestValue]) {
					err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
						exitExecuted = true
						return nil // Allow transition
					})
					assert.NoError(t, err)
					afterFuncExecuted = true
				})
			},
		}
		nextState := &TestState{name: "next"}

		graph, err := state.NewGraph[State](
			testState,
			On[NextEvent](testState, nextState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Fast forward to execute AfterFunc
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		// AfterFunc should have executed
		assert.True(t, afterFuncExecuted)

		// Trigger transition to verify OnExit works
		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.True(t, exitExecuted)

		// Should be in next state
		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, nextState, currentState)
	})

	t.Run("EntryMachine.OnExit is called from AfterEntry's callback and success", func(t *testing.T) {
		type TriggerEvent struct{}
		type NextEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var afterEntryExecuted bool
		var exitExecuted bool
		initialState := &TestState{name: "initial"}
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(_ *state.AfterEntryMachine[*TestValue]) {
					err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
						exitExecuted = true
						return nil // Allow transition
					})
					assert.NoError(t, err)
					afterEntryExecuted = true
				})
				require.NoError(t, err)
			},
		}
		nextState := &TestState{name: "next"}

		graph, err := state.NewGraph[State](
			initialState,
			On[TriggerEvent](initialState, testState),
			On[NextEvent](testState, nextState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger to test state
		require.NoError(t, machine.Trigger(TriggerEvent{}))
		// AfterEntry should have executed
		assert.True(t, afterEntryExecuted)

		// Trigger transition to verify OnExit works
		require.NoError(t, machine.Trigger(NextEvent{}))
		assert.True(t, exitExecuted)

		// Should be in next state
		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, nextState, currentState)
	})

	t.Run("EntryMachine.OnExit is called from OnExit's callback and error", func(t *testing.T) {
		type NextEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(_ *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
						assert.Fail(t, "should not be called")
						return nil
					})
					assert.ErrorContains(t, err, "method is not callable here")
					exitExecuted = true
					return &state.Guarded{Reason: errors.New("transition blocked")} // Block transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger transition (should be blocked)
		err = machine.Trigger(NextEvent{})
		assert.ErrorContains(t, err, "transition blocked")
		// Exit should have executed
		assert.True(t, exitExecuted)
	})
}

func TestAfterFuncMachine_Value(t *testing.T) {
	baseTime := time.Unix(0, 0)

	t.Run("Value returns machine data", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		expectedData := map[string]any{"timer": "test", "count": 456}
		value := &TestValue{Map: expectedData}

		var afterFuncExecuted bool
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 1*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
					assert.Equal(t, value, machine.Value())
					afterFuncExecuted = true
				})
			},
		}

		graph, err := state.NewGraph(testState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Fast forward to execute timer
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		assert.True(t, afterFuncExecuted)
	})
}

func TestAfterFuncMachine_AfterFunc(t *testing.T) {
	baseTime := time.Unix(0, 0)

	t.Run("Schedule timer from AfterFunc callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var firstExecuted, secondExecuted bool
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 2*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
					firstExecuted = true
					// Schedule another timer from within timer callback
					machine.AfterFunc(dispatcher, 1*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
						secondExecuted = true
					})
				})
			},
		}

		graph, err := state.NewGraph(testState)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())
		// First timer should not have executed yet
		assert.False(t, firstExecuted)
		assert.False(t, secondExecuted)

		// Fast forward to first timer
		require.NoError(t, dispatcher.FastForward(baseTime.Add(2*time.Second)))
		assert.True(t, firstExecuted)
		assert.False(t, secondExecuted)

		// Fast forward to second timer
		require.NoError(t, dispatcher.FastForward(baseTime.Add(3*time.Second)))
		assert.True(t, firstExecuted)
		assert.True(t, secondExecuted)
	})
}

func TestAfterFuncMachine_Trigger(t *testing.T) {
	type NextEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("Trigger from AfterFunc callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var triggerExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 1*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
					err := machine.Trigger(NextEvent{})
					require.NoError(t, err)
					triggerExecuted = true
				})
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())
		// Timer hasn't fired yet
		assert.False(t, triggerExecuted)

		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, fromState, currentState)

		// Fast forward to execute timer
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		// Should have triggered transition
		assert.True(t, triggerExecuted)

		currentState, err = machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, toState, currentState)
	})

	t.Run("AfterFuncMachine.Trigger is called from Entry method and error", func(t *testing.T) {
		type NextEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var entryExecuted bool
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				// Register an AfterEntry callback first to make the trigger fail
				require.NoError(t, machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {}))

				// Convert to AfterFuncMachine to try calling Trigger while already having AfterEntry
				afterFuncMachine := machine.AsMachineAccessor().AsAfterFuncMachine()
				err := afterFuncMachine.Trigger(NextEvent{})
				// Should have an error due to callback conflict
				assert.ErrorContains(t, err, "method is not callable here")

				entryExecuted = true
			},
		}
		nextState := &TestState{
			name: "next",
			entry: func(machine *EntryMachine, event Event) {
				assert.Fail(t, "should not be called")
			},
		}

		graph, err := state.NewGraph[State](
			testState,
			On[NextEvent](testState, nextState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())
		// Entry should have executed
		assert.True(t, entryExecuted)
	})

	t.Run("AfterFuncMachine.Trigger is called from AfterEntry callback and error", func(t *testing.T) {
		type NextEvent struct{}
		type SecondEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var afterEntryExecuted bool
		initialState := &TestState{name: "initial"}
		firstState := &TestState{
			name: "first",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
					// Convert to AfterFuncMachine to try calling Trigger from within AfterEntry context
					afterFuncMachine := machine.AsMachineAccessor().AsAfterFuncMachine()
					err := afterFuncMachine.Trigger(SecondEvent{})
					assert.ErrorContains(t, err, "method is not callable here")
					afterEntryExecuted = true
				})
				require.NoError(t, err)
			},
		}
		secondState := &TestState{
			name: "second",
			entry: func(machine *EntryMachine, event Event) {
				assert.Fail(t, "should not be called")
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[NextEvent](initialState, firstState),
			On[SecondEvent](firstState, secondState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		// AfterEntry should have executed
		assert.True(t, afterEntryExecuted)
	})

	t.Run("AfterFuncMachine.Trigger is called from OnExit callback and error", func(t *testing.T) {
		type NextEvent struct{}
		type SecondEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					// Convert to AfterFuncMachine to try calling Trigger from OnExit context
					afterFuncMachine := machine.AsMachineAccessor().AsAfterFuncMachine()
					err := afterFuncMachine.Trigger(SecondEvent{})
					assert.ErrorContains(t, err, "method is not callable here")

					exitExecuted = true
					return nil // Allow transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}
		secondState := &TestState{name: "second"}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
			On[SecondEvent](toState, secondState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		// Exit should have executed
		assert.True(t, exitExecuted)
	})
}

func TestAfterEntryMachine_Value(t *testing.T) {
	type TriggerEvent struct{}

	t.Run("Value returns machine data", func(t *testing.T) {
		value := &TestValue{Map: map[string]any{"after": "entry", "test": 789}}

		var afterEntryExecuted bool
		initialState := &TestState{name: "initial"}
		testState := &TestState{
			name: "test",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
					assert.Equal(t, value, machine.Value())
					afterEntryExecuted = true
				})
				require.NoError(t, err)
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[TriggerEvent](initialState, testState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(TriggerEvent{}))
		assert.True(t, afterEntryExecuted)
	})
}

func TestAfterEntryMachine_Trigger(t *testing.T) {
	type FirstEvent struct{}
	type SecondEvent struct{}
	baseTime := time.Unix(0, 0)

	t.Run("Trigger from AfterEntry callback", func(t *testing.T) {
		value := &TestValue{Map: make(map[string]any)}

		var triggerExecuted bool
		initialState := &TestState{name: "initial"}
		firstState := &TestState{
			name: "first",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
					err := machine.Trigger(SecondEvent{})
					require.NoError(t, err)
					triggerExecuted = true
				})
				require.NoError(t, err)
			},
		}
		secondState := &TestState{name: "second"}

		graph, err := state.NewGraph[State](
			initialState,
			On[FirstEvent](initialState, firstState),
			On[SecondEvent](firstState, secondState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(FirstEvent{}))
		assert.True(t, triggerExecuted)

		// Should have triggered transition to second state
		currentState, err := machine.CurrentState()
		require.NoError(t, err)
		assert.Equal(t, secondState, currentState)
	})

	t.Run("AfterEntry infinite loop prevention", func(t *testing.T) {
		type LoopEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var callCount int
		const maxIterations = 100000
		initialState := &TestState{name: "initial"}
		loopState := &TestState{
			name: "loop",
			entry: func(machine *EntryMachine, event Event) {
				callCount++
				if callCount < maxIterations {
					err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
						// This should trigger another Entry via AfterEntry mechanism
						_ = machine.Trigger(LoopEvent{})
					})
					require.NoError(t, err)
				}
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[LoopEvent](initialState, loopState),
			On[LoopEvent](loopState, loopState), // Self-loop
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Start the loop
		require.NoError(t, machine.Trigger(LoopEvent{}))
		// Should not result in stack overflow and should handle many iterations
		assert.Equal(t, callCount, maxIterations, "Should eventually stop when limit reached")
	})

	t.Run("AfterEntry chaining across states", func(t *testing.T) {
		type LoopEvent struct{}
		type ChainEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var executionOrder []string
		initialState := &TestState{name: "initial"}
		stateA := &TestState{
			name: "A",
			entry: func(machine *EntryMachine, event Event) {
				executionOrder = append(executionOrder, "A-entry")
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
					executionOrder = append(executionOrder, "A-after")
					_ = machine.Trigger(ChainEvent{})
				})
				require.NoError(t, err)
			},
		}
		stateB := &TestState{
			name: "B",
			entry: func(machine *EntryMachine, event Event) {
				executionOrder = append(executionOrder, "B-entry")
				err := machine.AfterEntry(func(machine *state.AfterEntryMachine[*TestValue]) {
					executionOrder = append(executionOrder, "B-after")
				})
				require.NoError(t, err)
			},
		}

		graph, err := state.NewGraph[State](
			initialState,
			On[LoopEvent](initialState, stateA),
			On[ChainEvent](stateA, stateB),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Trigger transition to stateA
		require.NoError(t, machine.Trigger(LoopEvent{}))
		// Should execute A-entry -> A-after -> B-entry -> B-after
		expected := []string{"A-entry", "A-after", "B-entry", "B-after"}
		assert.Equal(t, expected, executionOrder)
	})

	t.Run("AfterEntryMachine.Trigger is called from Entry method and error", func(t *testing.T) {
		type SecondEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var entryExecuted bool
		firstState := &TestState{
			name: "first",
			entry: func(machine *EntryMachine, event Event) {
				// Convert to AfterEntryMachine to try calling Trigger from Entry context
				afterEntryMachine := machine.AsMachineAccessor().AsAfterEntryMachine()
				err := afterEntryMachine.Trigger(SecondEvent{})
				assert.ErrorContains(t, err, "method is not callable here")

				entryExecuted = true
			},
		}
		secondState := &TestState{
			name: "second",
			entry: func(machine *EntryMachine, event Event) {
				assert.Fail(t, "Should not be called")
			},
		}

		graph, err := state.NewGraph[State](
			firstState,
			On[SecondEvent](firstState, secondState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Entry should have executed
		assert.True(t, entryExecuted)
	})

	t.Run("AfterEntryMachine.Trigger is called from AfterFunc's callback and error", func(t *testing.T) {
		type SecondEvent struct{}
		dispatcher := eventloop.NewDispatcher(baseTime)
		value := &TestValue{Map: make(map[string]any)}

		var afterFuncExecuted bool
		firstState := &TestState{
			name: "first",
			entry: func(machine *EntryMachine, event Event) {
				machine.AfterFunc(dispatcher, 1*time.Second, func(machine *state.AfterFuncMachine[*TestValue]) {
					// Convert to AfterEntryMachine to try calling Trigger from AfterFunc context
					afterEntryMachine := machine.AsMachineAccessor().AsAfterEntryMachine()
					err := afterEntryMachine.Trigger(SecondEvent{})
					assert.ErrorContains(t, err, "method is not callable here")

					afterFuncExecuted = true
				})
			},
		}
		secondState := &TestState{
			name: "second",
			entry: func(machine *EntryMachine, event Event) {
				assert.Fail(t, "Should not be called")
			},
		}

		graph, err := state.NewGraph[State](
			firstState,
			On[SecondEvent](firstState, secondState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		// Fast forward to execute AfterFunc
		require.NoError(t, dispatcher.FastForward(baseTime.Add(1*time.Second)))
		// AfterFunc should have executed
		assert.True(t, afterFuncExecuted)
	})

	t.Run("AfterEntryMachine.Trigger is called from OnExit's callback and error", func(t *testing.T) {
		type NextEvent struct{}
		type SecondEvent struct{}
		value := &TestValue{Map: make(map[string]any)}

		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					// Convert to AfterEntryMachine to try calling Trigger from OnExit context
					afterEntryMachine := machine.AsMachineAccessor().AsAfterEntryMachine()
					err := afterEntryMachine.Trigger(SecondEvent{})
					assert.ErrorContains(t, err, "method is not callable here")

					exitExecuted = true
					return nil // Allow transition
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{
			name: "to",
		}
		secondState := &TestState{
			name: "second",
			entry: func(machine *EntryMachine, event Event) {
				assert.Fail(t, "Should not be called")
			},
		}

		graph, err := state.NewGraph[State](
			fromState,
			On[NextEvent](fromState, toState),
			On[SecondEvent](toState, secondState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(NextEvent{}))
		// Exit should have executed
		assert.True(t, exitExecuted)
	})
}

func TestExitMachine_Value(t *testing.T) {
	type TransitionEvent struct{}

	t.Run("Value returns machine data", func(t *testing.T) {
		value := &TestValue{Map: map[string]any{"exit": "test", "value": 999}}

		var exitExecuted bool
		fromState := &TestState{
			name: "from",
			entry: func(machine *EntryMachine, event Event) {
				err := machine.OnExit(func(machine *state.ExitMachine[*TestValue], event Event) *state.Guarded {
					assert.Equal(t, value, machine.Value())
					exitExecuted = true
					return nil
				})
				require.NoError(t, err)
			},
		}
		toState := &TestState{name: "to"}

		graph, err := state.NewGraph[State](
			fromState,
			On[TransitionEvent](fromState, toState),
		)
		require.NoError(t, err)

		machine := state.NewMachine(graph, value)
		require.NoError(t, machine.Launch())

		require.NoError(t, machine.Trigger(TransitionEvent{}))
		assert.True(t, exitExecuted)
	})
}
