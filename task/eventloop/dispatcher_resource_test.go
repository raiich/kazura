package eventloop

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
