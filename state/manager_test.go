package state_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/raiich/kazura/state"
	"github.com/raiich/kazura/task/eventloop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_BasicOperations(t *testing.T) {
	t.Run("initial state is zero value", func(t *testing.T) {
		manager := state.NewManager[int]()
		assert.Equal(t, 0, manager.Get())
	})

	t.Run("set and get state", func(t *testing.T) {
		manager := state.NewManager[string]()

		// Initial state
		assert.Equal(t, "", manager.Get())

		// Set new state
		manager.Set("new_state")
		assert.Equal(t, "new_state", manager.Get())

		// Set another state
		manager.Set("another_state")
		assert.Equal(t, "another_state", manager.Get())
	})

	t.Run("multiple state changes", func(t *testing.T) {
		manager := state.NewManager[int]()

		values := []int{1, 42, -5, 0, 999}
		for _, v := range values {
			manager.Set(v)
			assert.Equal(t, v, manager.Get())
		}
	})

	t.Run("generic types", func(t *testing.T) {
		// String type
		stringManager := state.NewManager[string]()
		stringManager.Set("test")
		assert.Equal(t, "test", stringManager.Get())

		// Struct type
		type TestState struct {
			Value int
			Name  string
		}
		structManager := state.NewManager[TestState]()
		s := TestState{Value: 42, Name: "test"}
		structManager.Set(s)
		assert.Equal(t, s, structManager.Get())

		// Pointer type
		ptrManager := state.NewManager[*int]()
		value := 42
		ptrManager.Set(&value)
		assert.Equal(t, &value, ptrManager.Get())
		assert.Equal(t, 42, *ptrManager.Get())
	})
}

func TestManager_TimerExecution(t *testing.T) {
	t.Run("timer executes normally", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		executed := false
		manager.AfterFunc(dispatcher, 100*time.Millisecond, func() {
			executed = true
		})

		// Verify timer is active
		assert.Equal(t, 1, manager.ActiveTimerCount())

		// Advance time to trigger timer
		err := dispatcher.FastForward(time.Unix(0, 0).Add(100 * time.Millisecond))
		require.NoError(t, err)

		// Timer should have executed
		assert.True(t, executed, "timer should have executed")

		// Timer should be removed after execution
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("multiple timers execute in order", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		var execOrder []int

		// Schedule timers with different delays
		delays := []time.Duration{30 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond}
		expected := []int{1, 2, 0} // Order based on delays: 10ms, 20ms, 30ms

		for i, delay := range delays {
			manager.AfterFunc(dispatcher, delay, func() {
				execOrder = append(execOrder, i)
			})
		}

		// Verify all timers are active
		assert.Equal(t, 3, manager.ActiveTimerCount())

		// Advance time to trigger all timers
		require.NoError(t, dispatcher.FastForward(time.Unix(0, 0).Add(10*time.Millisecond)))
		assert.Equal(t, expected[:1], execOrder, "timers should execute in order of their delays")
		assert.Equal(t, 2, manager.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(time.Unix(0, 0).Add(20*time.Millisecond)))
		assert.Equal(t, expected[:2], execOrder, "timers should execute in order of their delays")
		assert.Equal(t, 1, manager.ActiveTimerCount())

		require.NoError(t, dispatcher.FastForward(time.Unix(0, 0).Add(30*time.Millisecond)))
		assert.Equal(t, expected, execOrder, "timers should execute in order of their delays")
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})
}

func TestManager_StateBasedTimerLifecycle(t *testing.T) {
	t.Run("timers execute during stable state periods", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[string]()

		manager.Set("initial")

		var executionLog []string

		// Schedule timers for different times
		manager.AfterFunc(dispatcher, 30*time.Millisecond, func() {
			executionLog = append(executionLog, "timer1_in_initial")
		})
		manager.AfterFunc(dispatcher, 40*time.Millisecond, func() {
			executionLog = append(executionLog, "timer2_in_initial")
		})

		// Execute first timer
		err := dispatcher.FastForward(time.Unix(0, 0).Add(30 * time.Millisecond))
		require.NoError(t, err)

		// Change state after first timer but before second
		manager.Set("changed")

		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			executionLog = append(executionLog, "timer3_in_changed")
		})

		// Execute remaining timers
		err = dispatcher.FastForward(time.Unix(0, 0).Add(80 * time.Millisecond))
		require.NoError(t, err)

		// Only timer1 and timer3 should execute - timer2 was cancelled by state change
		expected := []string{"timer1_in_initial", "timer3_in_changed"}
		assert.Equal(t, expected, executionLog)
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("state change cancels all pending timers", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		executed1 := false
		executed2 := false

		// Schedule multiple timers
		manager.AfterFunc(dispatcher, 100*time.Millisecond, func() {
			executed1 = true
		})
		manager.AfterFunc(dispatcher, 150*time.Millisecond, func() {
			executed2 = true
		})

		// Verify timers are active
		assert.Equal(t, 2, manager.ActiveTimerCount())

		// Change state before any timer fires
		manager.Set(42)

		// All timers should be canceled immediately
		assert.Equal(t, 0, manager.ActiveTimerCount())

		// Advance time past all timers
		err := dispatcher.FastForward(time.Unix(0, 0).Add(200 * time.Millisecond))
		require.NoError(t, err)

		// No timers should have executed
		assert.False(t, executed1, "timer1 should be cancelled by state change")
		assert.False(t, executed2, "timer2 should be cancelled by state change")
	})
}

func TestManager_EdgeCases(t *testing.T) {
	t.Run("zero duration timer", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		executed := false
		manager.AfterFunc(dispatcher, 0, func() {
			executed = true
		})
		assert.Equal(t, 1, manager.ActiveTimerCount())

		// Advance time by minimal amount
		err := dispatcher.FastForward(time.Unix(0, 0))
		require.NoError(t, err)

		// Timer should execute immediately
		assert.True(t, executed, "zero duration timer should execute immediately")
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("large number of timers", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		const numTimers = 1000
		var executedCount int

		// Schedule many timers
		for i := 0; i < numTimers; i++ {
			manager.AfterFunc(dispatcher, 100*time.Millisecond, func() {
				executedCount++
			})
		}

		// Verify all timers are active
		assert.Equal(t, numTimers, manager.ActiveTimerCount())

		// Advance time to trigger all timers
		err := dispatcher.FastForward(time.Unix(0, 0).Add(100 * time.Millisecond))
		require.NoError(t, err)

		// Verify all timers executed
		assert.Equal(t, numTimers, executedCount, "all timers should have executed")
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("rapid state transitions", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		var executedStates []int

		// Schedule timer in initial state
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			executedStates = append(executedStates, manager.Get())
		})

		// Rapid state changes
		manager.Set(1)
		manager.Set(2)
		manager.Set(3)

		// Schedule timer in final state
		manager.AfterFunc(dispatcher, 25*time.Millisecond, func() {
			executedStates = append(executedStates, manager.Get())
		})

		// Execute all timers
		err := dispatcher.FastForward(time.Unix(0, 0).Add(100 * time.Millisecond))
		require.NoError(t, err)

		// Only timer from final state should execute
		expected := []int{3}
		assert.Equal(t, expected, executedStates, "only timer from final state should execute")
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})
}

func TestManager_ErrorHandling(t *testing.T) {
	t.Run("panic in timer callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			panic("test panic")
		})

		// FastForward should return error for panic
		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		assert.ErrorContains(t, err, "panic: test panic", "error should contain panic message")
	})

	t.Run("panic stops subsequent timer execution", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		executed := false
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			panic("first panic")
		})
		manager.AfterFunc(dispatcher, 60*time.Millisecond, func() {
			executed = true
		})

		// FastForward should stop at first panic
		err := dispatcher.FastForward(time.Unix(0, 0).Add(100 * time.Millisecond))
		require.ErrorContains(t, err, "panic: first panic", "FastForward should return error on panic")

		// Second timer should not have executed due to panic
		assert.False(t, executed, "subsequent timer should not execute after panic")
	})

	t.Run("different panic types", func(t *testing.T) {
		testCases := []struct {
			name        string
			panicValue  interface{}
			expectedMsg string
		}{
			{"string panic", "string error", "panic: string error"},
			{"int panic", 42, "panic: 42"},
			{"struct panic", struct{ msg string }{"test"}, "panic: {test}"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
				manager := state.NewManager[int]()

				manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
					panic(tc.panicValue)
				})

				err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.expectedMsg)
			})
		}
	})

	t.Run("panic with nil", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			panic(nil)
		})

		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		assert.ErrorContains(t, err, "panic: panic called with nil argument")
	})
}

func TestManager_NestedOperationsInCallbacks(t *testing.T) {
	t.Run("manager.Set inside AfterFunc callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[string]()

		manager.Set("initial")

		// Schedule timer that will change state from within callback
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			manager.Set("changed_by_callback")
		})

		// Advance time to trigger callback
		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		require.NoError(t, err)

		// State should be changed by callback
		assert.Equal(t, "changed_by_callback", manager.Get())
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("manager.Get inside AfterFunc callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		manager.Set(42)

		var retrievedValue int
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			retrievedValue = manager.Get()
		})

		// Advance time to trigger callback
		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		require.NoError(t, err)

		// Callback should have retrieved current state
		assert.Equal(t, 42, retrievedValue)
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("manager.AfterFunc inside AfterFunc callback", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[string]()

		manager.Set("initial")

		var executionOrder []string

		// Schedule timer that will schedule another timer
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			executionOrder = append(executionOrder, "first_callback")

			// Schedule nested timer
			manager.AfterFunc(dispatcher, 30*time.Millisecond, func() {
				executionOrder = append(executionOrder, "nested_callback")
			})
		})

		// Execute first timer
		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		require.NoError(t, err)

		// At this point, first callback should have executed and nested timer should be active
		assert.Equal(t, []string{"first_callback"}, executionOrder)
		assert.Equal(t, 1, manager.ActiveTimerCount())

		// Execute nested timer
		err = dispatcher.FastForward(time.Unix(0, 0).Add(80 * time.Millisecond))
		require.NoError(t, err)

		// Now both callbacks should have executed
		assert.Equal(t, []string{"first_callback", "nested_callback"}, executionOrder)
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("complex nested operations", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[int]()

		manager.Set(1)

		var operations []string

		// Schedule timer that does multiple operations
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			operations = append(operations, fmt.Sprintf("get:%d", manager.Get()))

			manager.Set(2)
			operations = append(operations, fmt.Sprintf("set:2"))

			// Schedule another timer from within callback
			manager.AfterFunc(dispatcher, 25*time.Millisecond, func() {
				operations = append(operations, fmt.Sprintf("nested_get:%d", manager.Get()))
				manager.Set(3)
				operations = append(operations, fmt.Sprintf("nested_set:3"))
			})

			operations = append(operations, fmt.Sprintf("after_nested_timer:%d", manager.Get()))
		})

		// Execute first timer
		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		require.NoError(t, err)

		// Execute nested timer
		err = dispatcher.FastForward(time.Unix(0, 0).Add(75 * time.Millisecond))
		require.NoError(t, err)

		expected := []string{
			"get:1",
			"set:2",
			"after_nested_timer:2",
			"nested_get:2",
			"nested_set:3",
		}
		assert.Equal(t, expected, operations)
		assert.Equal(t, 3, manager.Get())
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})

	t.Run("state change in callback cancels other pending timers", func(t *testing.T) {
		dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
		manager := state.NewManager[string]()

		manager.Set("initial")

		var executed []string

		// Schedule multiple timers
		manager.AfterFunc(dispatcher, 50*time.Millisecond, func() {
			executed = append(executed, "timer1")
			// This state change should cancel timer2
			manager.Set("changed")
		})

		manager.AfterFunc(dispatcher, 60*time.Millisecond, func() {
			executed = append(executed, "timer2")
		})

		// Execute first timer - this should cancel timer2
		err := dispatcher.FastForward(time.Unix(0, 0).Add(50 * time.Millisecond))
		require.NoError(t, err)

		// Try to execute timer2 - it should already be cancelled
		err = dispatcher.FastForward(time.Unix(0, 0).Add(100 * time.Millisecond))
		require.NoError(t, err)

		// Only timer1 should have executed
		assert.Equal(t, []string{"timer1"}, executed)
		assert.Equal(t, "changed", manager.Get())
		assert.Equal(t, 0, manager.ActiveTimerCount())
	})
}

func TestManager_MultipleGoroutines(t *testing.T) {
	dispatcher := eventloop.NewDispatcher(time.Unix(0, 0))
	manager := state.NewManager[int]()
	var wg sync.WaitGroup

	// Test safe concurrent access from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Safe concurrent access via Dispatcher
			dispatcher.AfterFunc(0, func() {
				manager.Set(manager.Get() + 1)
			})
		}()
	}
	wg.Wait()

	// Execute all scheduled operations
	err := dispatcher.FastForward(time.Unix(0, 0).Add(0))
	require.NoError(t, err)

	// All state changes should be executed safely via Dispatcher
	assert.Equal(t, 10, manager.Get())
}
