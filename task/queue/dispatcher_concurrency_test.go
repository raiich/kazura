package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDispatcher_Concurrency(t *testing.T) {
	t.Run("concurrent AfterFunc and sequential execution", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		const numGoroutines = 1000
		var counter int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Schedule many callbacks from different goroutines concurrently
		for i := 0; i < numGoroutines; i++ {
			go func() {
				timer := dispatcher.AfterFunc(1*time.Millisecond, func() {
					// Since queue dispatcher executes sequentially, no race condition
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
		cancel()
		synctest.Wait()

		assert.Equal(t, context.Canceled, serveErr)
		// Success - due to sequential execution in queue, counter should be consistent
		assert.Equal(t, numGoroutines/2, counter, "half of tasks should execute")
	}))

	t.Run("concurrent stop", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		const numGoroutines = 100
		executed := false

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

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
		cancel()
		synctest.Wait()

		// Only one Stop() call should return true
		assert.Equal(t, 1, int(stopCount.Load()), "exactly one Stop() call should return true")
		assert.Equal(t, context.Canceled, serveErr)
		assert.False(t, executed, "function should not execute after being stopped")
	}))

	t.Run("concurrent dispatcher operations by high frequency scheduling", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		const numOperations = 10000
		var executedCount int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Concurrent operations: schedule tasks, some with cancellation
		for i := 0; i < numOperations; i++ {
			go func(taskID int) {
				timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
					executedCount++
				})

				// Cancel some tasks randomly
				if taskID%2 == 0 {
					go func() {
						timer.Stop()
					}()
				}
			}(i)
		}

		time.Sleep(20 * time.Millisecond) // Wait for goroutines to schedule tasks
		synctest.Wait()
		cancel()
		synctest.Wait()

		assert.Equal(t, context.Canceled, serveErr)
		assert.Equal(t, numOperations/2, executedCount)
	}))

	t.Run("mixed duration tasks", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)

		var shortCount, mediumCount, longCount int
		const tasksPerCategory = 100

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Schedule tasks with different durations concurrently
		for i := 0; i < tasksPerCategory; i++ {
			// Short duration tasks
			go func() {
				dispatcher.AfterFunc(1*time.Millisecond, func() {
					shortCount++
				})
			}()

			// Medium duration tasks
			go func() {
				dispatcher.AfterFunc(10*time.Millisecond, func() {
					mediumCount++
				})
			}()

			// Long duration tasks
			go func() {
				dispatcher.AfterFunc(20*time.Millisecond, func() {
					longCount++
				})
			}()
		}

		time.Sleep(20 * time.Millisecond) // Wait for goroutines to schedule tasks
		synctest.Wait()
		cancel()
		synctest.Wait()

		assert.Equal(t, context.Canceled, serveErr)
		assert.Equal(t, tasksPerCategory, shortCount, "all short duration tasks should execute")
		assert.Equal(t, tasksPerCategory, mediumCount, "all medium duration tasks should execute")
		assert.Equal(t, tasksPerCategory, longCount, "all long duration tasks should execute")
	}))
}
