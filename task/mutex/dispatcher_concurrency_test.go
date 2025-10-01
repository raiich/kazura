package mutex

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDispatcher_Concurrency(t *testing.T) {
	t.Run("concurrent AfterFunc and callback synchronization", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		const numGoroutines = 1000
		var counter int

		// Schedule many callbacks from different goroutines concurrently
		for i := 0; i < numGoroutines; i++ {
			go func() {
				timer := dispatcher.AfterFunc(1*time.Millisecond, func() {
					// Since mutex dispatcher executes with mutex protection, no race condition
					counter++
				})
				if i%2 == 0 {
					// Cancel some tasks
					timer.Stop()
				}
			}()
		}

		time.Sleep(1 * time.Millisecond) // Wait for goroutines to schedule tasks
		synctest.Wait()

		// Success - due to mutex protection, counter should be consistent
		assert.Equal(t, numGoroutines/2, counter, "half of tasks should execute")
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("concurrent stop", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		const numGoroutines = 100
		executed := false

		timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
			executed = true
		})

		// Multiple goroutines trying to stop the same timer
		var stopCount atomic.Int32
		for i := 0; i < numGoroutines; i++ {
			go func() {
				if timer.Stop() {
					stopCount.Add(1)
				}
			}()
		}

		time.Sleep(20 * time.Millisecond) // Wait for goroutines to call Stop()
		synctest.Wait()

		// Only one Stop() call should return true
		assert.Equal(t, 1, int(stopCount.Load()), "exactly one Stop() call should return true")
		assert.False(t, executed, "function should not execute after being stopped")
		assert.NoError(t, dispatcher.ExtractError())
	}))
}
