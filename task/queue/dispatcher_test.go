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
	tasktest.TestDispatcher(t, func(t *testing.T) (task.Dispatcher, *tasktest.TestHelper) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()
		t.Cleanup(func() {
			cancel()
			synctest.Wait()
		})
		return dispatcher, &tasktest.TestHelper{
			Start: time.Now(),
			AdvanceToFunc: func(to time.Time) error {
				if dur := time.Until(to); dur > 0 {
					time.Sleep(dur)
				}
				synctest.Wait()
				// serveErr is read without a lock, but this is safe after
				// synctest.Wait: the Serve goroutine is either durably blocked
				// (no write) or has returned (its write happens-before this read).
				return serveErr
			},
		}
	})
}

func TestDispatcher_Serve(t *testing.T) {
	t.Run("context cancellation by goroutine", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()

		go func() {
			cancel() // Stop dispatcher
		}()

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()

		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
	}))

	t.Run("context cancellation by AfterFunc", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()

		dispatcher.AfterFunc(1*time.Millisecond, func() {
			cancel() // Stop dispatcher
		})

		time.Sleep(1 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
	}))

	t.Run("concurrent call returns ErrServed", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()

		go func() {
			_ = dispatcher.Serve(ctx)
		}()
		synctest.Wait()

		assert.ErrorIs(t, dispatcher.Serve(ctx), ErrServed, "second Serve while running should return ErrServed")

		cancel()
		synctest.Wait()
	}))

	t.Run("call after stop returns ErrServed", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()

		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()
		cancel()
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled)
		assert.ErrorIs(t, dispatcher.Serve(ctx), ErrServed, "Serve after stop should return ErrServed")
	}))

	t.Run("timeout context", tasktest.WithSyncTest(func(t *testing.T) {
		timeout := 10 * time.Millisecond
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()
		dispatcher := NewDispatcher()

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()

		time.Sleep(timeout)
		synctest.Wait()

		assert.Equal(t, context.DeadlineExceeded, serveErr, "Serve should return context.DeadlineExceeded")
	}))
}

func TestDispatcher_QueueBehavior(t *testing.T) {
	t.Run("queue capacity", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher()

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
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
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher()

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
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
		dispatcher := NewDispatcher()

		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
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
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher()

		var shortCount, mediumCount, longCount int
		const tasksPerCategory = 100

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
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
