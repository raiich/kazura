package queue

import (
	"context"
	"sync"
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

// serveUntilStopped starts Serve and stops it by cancelling its context. It
// returns once the dispatcher has fully stopped, so later submissions settle as
// ErrCanceled.
func serveUntilStopped(t *testing.T) *Dispatcher {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	d := NewDispatcher()
	served := make(chan struct{})
	go func() {
		_ = d.Serve(ctx)
		close(served)
	}()
	cancel()
	<-served
	return d
}

func TestDispatcher_InvokeFunc(t *testing.T) {
	t.Run("Task.Wait returns the context cause when ctx ends before the worker runs f", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()
		go func() {
			_ = dispatcher.Serve(ctx)
		}()

		release := make(chan struct{})
		dispatcher.InvokeFunc(func() { <-release })
		synctest.Wait() // the worker is blocked inside the first function

		waitCtx, waitCancel := context.WithTimeout(t.Context(), 1*time.Millisecond)
		defer waitCancel()
		err := dispatcher.InvokeFunc(func() {}).Wait(waitCtx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)

		close(release)
		cancel()
		synctest.Wait()
	}))

	t.Run("on a stopped dispatcher, f does not run and Wait reports ErrCanceled", tasktest.WithSyncTest(func(t *testing.T) {
		dispatcher := serveUntilStopped(t)

		ran := false
		err := dispatcher.InvokeFunc(func() { ran = true }).Wait(t.Context())
		assert.ErrorIs(t, err, task.ErrCanceled)
		assert.False(t, ran, "function should not run on a stopped dispatcher")
	}))
}

func TestDispatcher_ConcurrentCancel(t *testing.T) {
	// Concurrent submissions to a stopped dispatcher must all settle as canceled
	// without panicking or blocking, even when they race against each other.
	t.Run("concurrent submissions after stop settle as canceled without panicking", tasktest.WithSyncTest(func(t *testing.T) {
		dispatcher := serveUntilStopped(t)

		const numGoroutines = 200
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		errs := make([]error, numGoroutines)
		for i := range errs {
			go func() {
				defer wg.Done()
				errs[i] = dispatcher.InvokeFunc(func() {}).Wait(t.Context())
			}()
		}
		wg.Wait()

		for _, err := range errs {
			assert.ErrorIs(t, err, task.ErrCanceled)
		}
	}))
}

func TestDispatcher_Serve(t *testing.T) {
	t.Run("context cancellation by goroutine", tasktest.WithSyncTest(func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher()

		go func() {
			cancel() // Stop dispatcher
		}()

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

		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()

		var executedBefore, executedAfter bool

		// Both timers are scheduled before the cancel; only the first one fires
		// before it.
		dispatcher.AfterFunc(1*time.Millisecond, func() {
			executedBefore = true
		})

		time.Sleep(1 * time.Millisecond)

		dispatcher.AfterFunc(1*time.Millisecond, func() {
			executedAfter = true
		})

		synctest.Wait()
		cancel() // Stop dispatcher

		time.Sleep(100 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.True(t, executedBefore, "task whose timer fired before the cancel should execute")
		assert.False(t, executedAfter, "task whose timer fires after the cancel should not execute")
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

		var serveErr error
		go func() {
			serveErr = dispatcher.Serve(ctx)
		}()

		for i := 0; i < tasksPerCategory; i++ {
			go func() {
				dispatcher.AfterFunc(1*time.Millisecond, func() {
					shortCount++
				})
			}()

			go func() {
				dispatcher.AfterFunc(10*time.Millisecond, func() {
					mediumCount++
				})
			}()

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
