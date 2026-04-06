package eventloop

import (
	"testing"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T, f func(t *testing.T, d task.Dispatcher, h *tasktest.TestHelper)) {
		start := time.Unix(0, 0)
		current := start
		dispatcher := NewDispatcher(start)
		f(t, dispatcher, &tasktest.TestHelper{
			Advance: func(dur time.Duration) error {
				current = current.Add(dur)
				return dispatcher.FastForward(current)
			},
		})
	})
}

func TestDispatcher_FastForward(t *testing.T) {
	t.Run("fast forward error", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		assert.ErrorContains(t, dispatcher.FastForward(startTime.Add(-1)), "unprocessable time")
	})

	t.Run("partial advance", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		var f1, f2, f3 bool
		dispatcher.AfterFunc(100*time.Millisecond, func() { f1 = true })
		dispatcher.AfterFunc(200*time.Millisecond, func() { f2 = true })
		dispatcher.AfterFunc(300*time.Millisecond, func() { f3 = true })

		require.NoError(t, dispatcher.FastForward(startTime.Add(150*time.Millisecond)))

		assert.True(t, f1, "f1 at 100ms should execute before 150ms")
		assert.False(t, f2, "f2 at 200ms should not execute at 150ms")
		assert.False(t, f3, "f3 at 300ms should not execute at 150ms")
	})
}

func TestDispatcher_NestedAfterFunc_SameFastForward(t *testing.T) {
	startTime := timeNow()
	dispatcher := NewDispatcher(startTime)

	var results []int
	dispatcher.AfterFunc(10*time.Millisecond, func() {
		results = append(results, 1)
		dispatcher.AfterFunc(0, func() {
			results = append(results, 2)
		})
	})

	require.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))
	assert.Equal(t, []int{1, 2}, results, "tasks added during execution should run in same FastForward")
}

func TestDispatcher_ResourceManagement(t *testing.T) {
	t.Run("uncancelled cleanup", func(t *testing.T) {
		dispatcher := NewDispatcher(timeNow())
		numTimers := 1000

		// Schedule timers without cancelling
		for i := 0; i < numTimers; i++ {
			dispatcher.AfterFunc(10*time.Millisecond, func() {
				// Simple task that doesn't do much
			})
		}

		// Verify all timers are scheduled
		initialCount := dispatcher.TaskCount()
		assert.Equal(t, numTimers, initialCount, "all timers should be scheduled")

		// Execute all tasks
		startTime := timeNow()
		require.NoError(t, dispatcher.FastForward(startTime.Add(20*time.Millisecond)))

		// Verify all tasks are cleaned up after execution
		finalCount := dispatcher.TaskCount()
		assert.Equal(t, 0, finalCount, "all tasks should be cleaned up after execution")
	})

	t.Run("cancelled cleanup", func(t *testing.T) {
		dispatcher := NewDispatcher(timeNow())
		numTimers := 1000

		// Schedule and immediately cancel many timers
		for i := 0; i < numTimers; i++ {
			timer := dispatcher.AfterFunc(time.Hour, func() {
				// Long-running task that should never execute
			})
			stopped := timer.Stop()
			assert.True(t, stopped, "timer should be successfully cancelled")
		}

		// Verify all tasks are immediately removed from queue
		taskCount := dispatcher.TaskCount()
		assert.Equal(t, 0, taskCount, "cancelled timers should be immediately removed")
	})
}
