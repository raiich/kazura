package queue

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_AfterFunc(t *testing.T) {
	t.Run("executes function", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		dispatcher.AfterFunc(5*time.Millisecond, func() {
			cancel()
		})

		time.Sleep(5 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
	}))

	t.Run("executes once", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var count int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		dispatcher.AfterFunc(5*time.Millisecond, func() {
			count++
		})

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		cancel() // Stop dispatcher
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.Equal(t, 1, count, "function should execute exactly once")
	}))
}

func TestTimer_Stop(t *testing.T) {
	t.Run("before execution", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var count int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
			count++
		})
		assert.True(t, timer.Stop(), "Stop() should return true when stopping before execution")

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		cancel() // Stop dispatcher
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.Equal(t, 0, count, "function should not executed after being stopped")
	}))

	t.Run("after execution", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var count int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		timer := dispatcher.AfterFunc(1*time.Millisecond, func() {
			defer cancel() // Stop dispatcher
			count++
		})

		time.Sleep(1 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.False(t, timer.Stop(), "Stop() should return false when stopping after execution")
		assert.Equal(t, 1, count, "function should execute exactly once")
	}))

	t.Run("multiple calls", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var count int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
			count++
		})

		stopped1 := timer.Stop()
		stopped2 := timer.Stop()
		stopped3 := timer.Stop()

		assert.True(t, stopped1, "first Stop() should return true")
		assert.False(t, stopped2, "second Stop() should return false")
		assert.False(t, stopped3, "third Stop() should return false")

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		cancel() // Stop dispatcher
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.Equal(t, 0, count, "function should not execute after being stopped")
	}))
}

func TestDispatcher_MultipleTimers(t *testing.T) {
	t.Run("different delays", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var order []int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		var completedCount int

		dispatcher.AfterFunc(30*time.Millisecond, func() {
			order = append(order, 3)
			completedCount++
			if completedCount == 3 {
				cancel() // Stop dispatcher
			}
		})

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			order = append(order, 1)
			completedCount++
			if completedCount == 3 {
				cancel() // Stop dispatcher
			}
		})

		dispatcher.AfterFunc(20*time.Millisecond, func() {
			order = append(order, 2)
			completedCount++
			if completedCount == 3 {
				cancel() // Stop dispatcher
			}
		})

		time.Sleep(30 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.Equal(t, []int{1, 2, 3}, order, "functions should execute in order of their delays")
	}))

	t.Run("execution timing", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		start := time.Now()
		var executed time.Time

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			defer cancel() // Stop dispatcher
			executed = time.Now()
		})

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		elapsed := executed.Sub(start)
		assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond, "function should not execute too early")
		assert.LessOrEqual(t, elapsed, 10*time.Millisecond, "function should not execute too late")
	}))

	t.Run("sequential execution", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		dispatcher := NewDispatcher(parentCtx)
		var counter int

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Since queue dispatcher executes tasks sequentially, no race conditions should occur
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			counter += 2
		})

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			counter += 3
		})

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		cancel()
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.Equal(t, 5, counter, "counter should be 5 (sequential execution, no race conditions)")
	}))
}

func TestDispatcher_EdgeCases(t *testing.T) {
	t.Run("zero duration", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var executed bool

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		timer := dispatcher.AfterFunc(0, func() {
			defer cancel()
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned even for zero duration")

		time.Sleep(0)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.True(t, executed)
	}))

	t.Run("negative duration", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)
		var executed bool

		// Start dispatcher
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		timer := dispatcher.AfterFunc(-time.Second, func() {
			defer cancel()
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned even for zero duration")

		time.Sleep(0)
		synctest.Wait()

		assert.ErrorIs(t, serveErr, context.Canceled, "Serve should return context.Canceled")
		assert.True(t, executed)
	}))

	t.Run("max duration", withSyncTest(func(t *testing.T) {
		parentCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		dispatcher := NewDispatcher(parentCtx)

		go func() {
			_ = dispatcher.Serve()
		}()

		timer := dispatcher.AfterFunc(time.Duration(1<<63-1), func() {})
		require.NotNil(t, timer, "Timer should be returned for max duration")
		assert.True(t, timer.Stop(), "Timer with max duration should be cancellable")
	}))
}

func TestDispatcher_Serve(t *testing.T) {
	t.Run("context cancellation by goroutine", withSyncTest(func(t *testing.T) {
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

	t.Run("context cancellation by AfterFunc", withSyncTest(func(t *testing.T) {
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

	t.Run("timeout context", withSyncTest(func(t *testing.T) {
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

func TestDispatcher_ErrorHandling(t *testing.T) {
	t.Run("panic function", withSyncTest(func(t *testing.T) {
		ctx := t.Context()
		dispatcher := NewDispatcher(ctx)

		var executed1, executed2 bool

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Schedule first function that executes successfully
		dispatcher.AfterFunc(5*time.Millisecond, func() {
			executed1 = true
		})

		// Schedule second function that panics
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			panic("test panic message")
		})

		// Schedule third function after panic (should not execute due to stopped dispatcher)
		dispatcher.AfterFunc(15*time.Millisecond, func() {
			executed2 = true
		})

		// Wait a bit to ensure third function would have executed if not for stopped dispatcher
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()

		assert.ErrorContains(t, serveErr, "panic: test panic message", "Error should contain panic message")
		assert.True(t, executed1, "First function should have executed successfully")
		assert.False(t, executed2, "Third function should not execute after panic (dispatcher stopped)")
	}))

	t.Run("proper recovery", withSyncTest(func(t *testing.T) {
		ctx := t.Context()
		dispatcher := NewDispatcher(ctx)

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Schedule multiple functions that panic
		dispatcher.AfterFunc(5*time.Millisecond, func() {
			panic("first panic")
		})

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			panic("second panic")
		})

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()

		assert.ErrorContains(t, serveErr, "panic: first panic", "Error should contain first panic message")
	}))

	t.Run("panic with nil", withSyncTest(func(t *testing.T) {
		ctx := t.Context()
		dispatcher := NewDispatcher(ctx)

		// Start Serve in a goroutine
		var serveErr error
		go func() {
			serveErr = dispatcher.Serve()
		}()

		// Schedule function that panics with specific value
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			panic(nil)
		})

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()

		assert.ErrorContains(t, serveErr, "panic: panic called with nil argument")
	}))
}

func TestDispatcher_PanicWithDifferentTypes(t *testing.T) {
	testCases := []struct {
		name        string
		panicValue  interface{}
		expectedMsg string
	}{
		{
			name:        "string panic",
			panicValue:  "string error",
			expectedMsg: "panic: string error",
		},
		{
			name:        "integer panic",
			panicValue:  42,
			expectedMsg: "panic: 42",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, withSyncTest(func(t *testing.T) {
			ctx := t.Context()
			dispatcher := NewDispatcher(ctx)

			// Start Serve in a goroutine
			var serveErr error
			go func() {
				serveErr = dispatcher.Serve()
			}()

			// Schedule function that panics with specific value
			dispatcher.AfterFunc(10*time.Millisecond, func() {
				panic(tc.panicValue)
			})

			time.Sleep(10 * time.Millisecond)
			synctest.Wait()

			assert.ErrorContains(t, serveErr, tc.expectedMsg, "Error should contain expected panic message")
		}))
	}
}

func TestDispatcher_QueueBehavior(t *testing.T) {
	t.Run("queue capacity", withSyncTest(func(t *testing.T) {
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

	t.Run("context cancelled during queue wait", withSyncTest(func(t *testing.T) {
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
}
