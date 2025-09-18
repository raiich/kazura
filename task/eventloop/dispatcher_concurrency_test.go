package eventloop

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDispatcher_Concurrency(t *testing.T) {
	t.Run("concurrent AfterFunc and callback synchronization", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		numGoroutines := 1000
		executedCount := 0

		var wg sync.WaitGroup

		// Launch multiple goroutines calling AfterFunc concurrently
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				timer := dispatcher.AfterFunc(10*time.Millisecond, func() {
					executedCount++
				})
				if i%2 == 0 {
					// Cancel some tasks
					timer.Stop()
				}
			}()
		}

		// Execute all scheduled tasks
		wg.Wait()
		assert.NoError(t, dispatcher.FastForward(startTime.Add(10*time.Millisecond)))

		// Verify all tasks were executed exactly once
		assert.Equal(t, numGoroutines/2, executedCount)
	})

	t.Run("concurrent stop", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)
		numGoroutines := 100
		executed := false

		timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
			executed = true
		})

		var wg sync.WaitGroup
		var successfulStops atomic.Int32

		// Launch multiple goroutines calling Stop() concurrently
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				if timer.Stop() {
					successfulStops.Add(1)
				}
			}()
		}

		// Only one Stop() call should succeed
		wg.Wait()
		assert.Equal(t, 1, int(successfulStops.Load()), "only one Stop() call should succeed")

		// Execute scheduled tasks (should not execute the stopped task)
		assert.NoError(t, dispatcher.FastForward(startTime.Add(100*time.Millisecond)))
		assert.False(t, executed, "task should not execute after being stopped")
	})
}
