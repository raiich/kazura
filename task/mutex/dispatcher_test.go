package mutex

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_AfterFunc(t *testing.T) {
	t.Run("executes function", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var count int

		dispatcher.AfterFunc(5*time.Millisecond, func() {
			count++
		})

		time.Sleep(5 * time.Millisecond)
		synctest.Wait()

		assert.Equal(t, 1, count, "function should execute")
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("executes once", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var count int

		dispatcher.AfterFunc(5*time.Millisecond, func() {
			count++
		})

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()

		assert.Equal(t, 1, count, "function should execute exactly once")
		assert.NoError(t, dispatcher.ExtractError())
	}))
}

func TestTimer_Stop(t *testing.T) {
	t.Run("before execution", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var count int

		timer := dispatcher.AfterFunc(20*time.Millisecond, func() {
			count++
		})
		assert.True(t, timer.Stop(), "Stop() should return true when stopping before execution")

		time.Sleep(50 * time.Millisecond)
		synctest.Wait()

		assert.Equal(t, 0, count, "function should not executed after being stopped")
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("after execution", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var count int

		timer := dispatcher.AfterFunc(1*time.Millisecond, func() {
			count++
		})

		time.Sleep(1 * time.Millisecond)
		synctest.Wait()

		assert.False(t, timer.Stop(), "Stop() should return false when stopping after execution")
		assert.Equal(t, 1, count, "function should execute exactly once")
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("multiple calls", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var count int

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

		assert.Equal(t, 0, count, "function should not execute after being stopped")
		assert.NoError(t, dispatcher.ExtractError())
	}))
}

func TestDispatcher_MultipleTimers(t *testing.T) {
	t.Run("different delays", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var order []int

		dispatcher.AfterFunc(30*time.Millisecond, func() {
			order = append(order, 3)
		})

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			order = append(order, 1)
		})

		dispatcher.AfterFunc(20*time.Millisecond, func() {
			order = append(order, 2)
		})

		time.Sleep(30 * time.Millisecond)
		synctest.Wait()

		assert.Equal(t, []int{1, 2, 3}, order, "functions should execute in order of their delays")
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("execution timing", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		start := time.Now()
		var executed time.Time

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			executed = time.Now()
		})

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()

		elapsed := executed.Sub(start)
		assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond, "function should not execute too early")
		assert.LessOrEqual(t, elapsed, 10*time.Millisecond, "function should not execute too late")
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("synchronized execution", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var counter int

		// Since mutex dispatcher executes tasks with mutex protection, no race conditions should occur
		dispatcher.AfterFunc(10*time.Millisecond, func() {
			counter += 2
		})

		dispatcher.AfterFunc(10*time.Millisecond, func() {
			counter += 3
		})

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()

		assert.Equal(t, 5, counter, "counter should be 5 (synchronized execution, no race conditions)")
		assert.NoError(t, dispatcher.ExtractError())
	}))
}

func TestDispatcher_EdgeCases(t *testing.T) {
	t.Run("zero duration", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var executed bool

		timer := dispatcher.AfterFunc(0, func() {
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned even for zero duration")

		time.Sleep(0)
		synctest.Wait()

		assert.True(t, executed)
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("negative duration", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		var executed bool

		timer := dispatcher.AfterFunc(-time.Second, func() {
			executed = true
		})
		require.NotNil(t, timer, "Timer should be returned even for negative duration")

		time.Sleep(0)
		synctest.Wait()

		assert.True(t, executed)
		assert.NoError(t, dispatcher.ExtractError())
	}))

	t.Run("max duration", withSyncTest(func(t *testing.T) {
		dispatcher := NewDispatcher()
		timer := dispatcher.AfterFunc(time.Duration(1<<63-1), func() {})
		require.NotNil(t, timer, "Timer should be returned for max duration")
		assert.True(t, timer.Stop(), "Timer with max duration should be cancellable")
		assert.NoError(t, dispatcher.ExtractError())
	}))
}

func TestDispatcher_ErrorHandling(t *testing.T) {
	t.Run("panic function", withSyncTest(func(t *testing.T) {
		ctx := t.Context()
		dispatcher := NewDispatcher()

		// Start error checking in a goroutine
		var serveErr error
		go func() {
			select {
			case <-ctx.Done():
				serveErr = ctx.Err()
			case err := <-dispatcher.Err():
				serveErr = err
			}
		}()

		var executed1, executed2 bool

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
		dispatcher := NewDispatcher()

		// Start error checking in a goroutine
		var serveErr error
		go func() {
			select {
			case <-ctx.Done():
				serveErr = ctx.Err()
			case err := <-dispatcher.Err():
				serveErr = err
			}
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
		dispatcher := NewDispatcher()

		// Start error checking in a goroutine
		var serveErr error
		go func() {
			select {
			case <-ctx.Done():
				serveErr = ctx.Err()
			case err := <-dispatcher.Err():
				serveErr = err
			}
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
			dispatcher := NewDispatcher()

			// Start error checking in a goroutine
			var serveErr error
			go func() {
				select {
				case <-ctx.Done():
					serveErr = ctx.Err()
				case err := <-dispatcher.Err():
					serveErr = err
				}
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
