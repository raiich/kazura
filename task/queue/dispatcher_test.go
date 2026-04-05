package queue

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
	"github.com/stretchr/testify/assert"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T, f func(t *testing.T, d task.Dispatcher, h *tasktest.TestHelper)) {
		tasktest.WithSyncTest(func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			dispatcher := NewDispatcher(ctx)
			var serveErr error
			go func() {
				serveErr = dispatcher.Serve()
			}()
			t.Cleanup(func() {
				cancel()
				synctest.Wait()
			})
			f(t, dispatcher, &tasktest.TestHelper{
				Advance: func(dur time.Duration) error {
					time.Sleep(dur)
					synctest.Wait()
					return serveErr
				},
			})
		})(t)
	})
}

func TestDispatcher_Serve(t *testing.T) {
	t.Run("context cancellation by goroutine", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher(ctx)

		go func() {
			cancel() // Stop dispatcher
		}()

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
	}))

	t.Run("context cancellation by AfterFunc", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher(ctx)

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		dispatcher.AfterFunc(1*time.Millisecond, func() {
			cancel() // Stop dispatcher
		})

		time.Sleep(1 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
	}))

	t.Run("timeout context", tasktest.WithSyncTest(func(t *testing.T) {
		timeout := 10 * time.Millisecond
		parentCtx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		time.Sleep(timeout)
		synctest.Wait()

		assert.Equal(t, context.DeadlineExceeded, serveErr, "Serve should return context.DeadlineExceeded")
	}))
}

func TestDispatcher_QueueBehavior(t *testing.T) {
	t.Run("queue capacity", tasktest.WithSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		var completedCount int

		// Schedule more tasks than queue capacity (128)
		for i := 0; i < 200; i++ {
			dispatcher.AfterFunc(1*time.Millisecond, func() {
				completedCount++
			})
		}

		time.Sleep(1 * time.Millisecond) // Wait for goroutines to schedule tasks
		synctest.Wait()
		cancel() // Stop dispatcher
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.Equal(t, 200, completedCount, "all tasks should execute despite queue capacity")
	}))

	t.Run("context cancelled during queue wait", tasktest.WithSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		var executedBefore, executedAfter bool

		// Schedule task before cancellation
		dispatcher.AfterFunc(1*time.Millisecond, func() {
			executedBefore = true
		})

		time.Sleep(1 * time.Millisecond)

		// Schedule task after cancellation
		dispatcher.AfterFunc(1*time.Millisecond, func() {
			executedAfter = true
		})

		synctest.Wait()
		cancel() // Stop dispatcher

		time.Sleep(100 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.True(t, executedBefore, "task scheduled before cancellation should execute")
		assert.False(t, executedAfter, "task scheduled after cancellation should not execute")
	}))

	t.Run("enqueue abandoned after context cancel", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher(ctx)

		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		executed := false
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			executed = true
		})

		cancel()
		synctest.Wait()

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled)
		assert.False(t, executed, "function should not execute after context cancelled")
	}))
}

func TestDispatcher_Concurrency(t *testing.T) {
	t.Run("mixed duration tasks", tasktest.WithSyncTest(func(t *testing.T) {
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
