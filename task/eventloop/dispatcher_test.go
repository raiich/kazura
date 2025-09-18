package eventloop

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_AfterFunc(t *testing.T) {
	t.Run("executes function", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			executed = true
		})

		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))
		assert.True(t, executed, "function should be executed")
	})

	t.Run("executes once", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		count := 0

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			count++
		})

		// FastForward executes all scheduled tasks synchronously
		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))
		assert.Equal(t, 1, count, "function should execute exactly once")
		require.NoError(t, dispatcher.FastForward(startTime.Add(90*time.Millisecond)))
		assert.Equal(t, 1, count, "function should execute exactly once")
	})
}

func TestTimer_Stop(t *testing.T) {
	t.Run("before execution", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false

		timer := dispatcher.AfterFunc(10*time.Millisecond, func() {
			executed = true
		})

		stopped := timer.Stop()
		assert.True(t, stopped, "Stop() should return true when canceling before execution")

		require.NoError(t, dispatcher.FastForward(startTime.Add(100*time.Millisecond)))
		assert.False(t, executed, "function should not execute after being stopped")
	})

	t.Run("after execution", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := 0

		timer := dispatcher.AfterFunc(10*time.Millisecond, func() {
			executed++
		})

		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))
		require.Equal(t, 1, executed, "function should execute once")

		stopped := timer.Stop()
		assert.False(t, stopped, "Stop() should return false when called after execution")

		require.NoError(t, dispatcher.FastForward(startTime.Add(90*time.Millisecond)))
		require.Equal(t, 1, executed, "function should execute once")
	})

	t.Run("multiple calls", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false

		timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
			executed = true
		})

		stopped1 := timer.Stop()
		assert.True(t, stopped1, "first Stop() should return true")

		stopped2 := timer.Stop()
		assert.False(t, stopped2, "subsequent Stop() should return false")

		stopped3 := timer.Stop()
		assert.False(t, stopped3, "subsequent Stop() should return false")

		require.NoError(t, dispatcher.FastForward(startTime.Add(100*time.Millisecond)))
		assert.False(t, executed, "function should not execute after being stopped")
	})
}

func TestDispatcher_MultipleTimers(t *testing.T) {
	t.Run("different delays", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		var results []int

		dispatcher.AfterFunc(30*time.Millisecond, func() {
			results = append(results, 3)
		})
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			results = append(results, 1)
		})
		dispatcher.AfterFunc(20*time.Millisecond, func() {
			results = append(results, 2)
		})

		require.NoError(t, dispatcher.FastForward(startTime.Add(30*time.Millisecond)))
		assert.Equal(t, []int{1, 2, 3}, results, "functions should execute in order of delay")
		require.NoError(t, dispatcher.FastForward(startTime.Add(100*time.Millisecond)))
		assert.Equal(t, []int{1, 2, 3}, results, "functions should execute in order of delay")
	})

	t.Run("execution timing", func(t *testing.T) {
		// This test verifies that tasks execute at their scheduled time
		// Since we no longer expose Now() method, we verify execution order instead
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false
		var executedAt time.Time

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			executed = true
			executedAt = dispatcher.now
		})

		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond-1)))
		assert.False(t, executed, "function should not execute before the scheduled time")

		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))
		assert.True(t, executed, "function should execute at the scheduled time")
		assert.Equal(t, startTime.Add(10*time.Millisecond), executedAt, "function should execute at the scheduled time")
	})

	t.Run("synchronized execution", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		counter := 0

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			counter += 2
		})

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			counter += 3
		})

		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond-1)))
		assert.Equal(t, 0, counter, "counter should be 0")
		require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))
		assert.Equal(t, 5, counter, "counter should be 5 (no race conditions)")
	})
}

func TestDispatcher_EdgeCases(t *testing.T) {
	t.Run("zero duration", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false

		timer := dispatcher.AfterFunc(0, func() {
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned even for zero duration")

		require.NoError(t, dispatcher.FastForward(startTime.Add(0)))
		assert.True(t, executed, "function should execute immediately for zero duration")
	})

	t.Run("negative duration", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false

		timer := dispatcher.AfterFunc(-10*time.Millisecond, func() {
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned even for negative duration")

		require.NoError(t, dispatcher.FastForward(startTime.Add(0)))
		assert.True(t, executed, "function should execute immediately for negative duration")
	})

	t.Run("max duration", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executed := false

		maxDuration := time.Duration(1<<63 - 1)
		timer := dispatcher.AfterFunc(maxDuration, func() {
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned for max duration")

		stopped := timer.Stop()
		assert.True(t, stopped, "Timer should be cancellable even for max duration")

		require.NoError(t, dispatcher.FastForward(startTime.Add(maxDuration)))
		assert.False(t, executed, "function should not execute after being cancelled")

		// Verify the task is removed from the dispatcher's task list
		taskCount := dispatcher.TaskCount()
		assert.Equal(t, 0, taskCount, "cancelled timer should be removed from task list")
	})
}

func TestDispatcher_ErrorHandling(t *testing.T) {
	t.Run("panic function", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			panic("test panic")
		})

		err := dispatcher.FastForward(startTime.Add(10 * time.Millisecond))
		assert.ErrorContains(t, err, "panic: test panic")
	})

	t.Run("proper recovery", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		executedAfterPanic := false

		// First task will panic
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			panic("test panic")
		})

		// Second task should not execute due to panic in first task
		dispatcher.AfterFunc(20*time.Millisecond, func() {
			executedAfterPanic = true
		})

		err := dispatcher.FastForward(startTime.Add(30 * time.Millisecond))
		assert.ErrorContains(t, err, "panic: test panic")
		assert.False(t, executedAfterPanic, "subsequent tasks should not execute after a panic")
	})

	t.Run("panic with different types", func(t *testing.T) {
		testCases := []struct {
			name       string
			panicValue interface{}
		}{
			{"string panic", "string panic message"},
			{"int panic", 42},
			{"struct panic", struct{ msg string }{"struct panic"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				startTime := timeNow()
				dispatcher := NewDispatcher(startTime)

				dispatcher.AfterFunc(10*time.Millisecond, func() {
					panic(tc.panicValue)
				})

				err := dispatcher.FastForward(startTime.Add(10 * time.Millisecond))
				assert.ErrorContains(t, err, fmt.Sprintf("panic: %v", tc.panicValue), "panic message should contain panic message")
			})
		}
	})

	t.Run("panic with nil", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			panic(nil)
		})

		err := dispatcher.FastForward(startTime.Add(10 * time.Millisecond))
		assert.ErrorContains(t, err, "panic: panic called with nil argument")
	})
}

func TestDispatcher_FastForward(t *testing.T) {
	t.Run("fast forward error", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		assert.ErrorContains(t, dispatcher.FastForward(startTime.Add(-1)), "unprocessable time")
	})
}
