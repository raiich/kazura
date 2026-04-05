package tasktest

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelper provides test-specific time control for Dispatcher conformance tests.
type TestHelper struct {
	// Advance moves time forward by d and ensures all tasks up to that point execute.
	// Returns an error if a dispatched function panicked (for eventloop-style dispatchers).
	Advance func(d time.Duration) error
}

// RunFunc creates a Dispatcher, wraps the test with synctest if needed, and calls f.
type RunFunc func(t *testing.T, f func(t *testing.T, d task.Dispatcher, h *TestHelper))

// TestDispatcher runs the common Dispatcher conformance tests.
// Each Dispatcher implementation should call this from its own package test.
func TestDispatcher(t *testing.T, run RunFunc) {
	t.Helper()
	t.Run("AfterFunc", func(t *testing.T) {
		testAfterFunc(t, run)
	})
	t.Run("TimerStop", func(t *testing.T) {
		testTimerStop(t, run)
	})
	t.Run("MultipleTimers", func(t *testing.T) {
		testMultipleTimers(t, run)
	})
	t.Run("EdgeCases", func(t *testing.T) {
		testEdgeCases(t, run)
	})
	t.Run("Panic", func(t *testing.T) {
		testPanic(t, run)
	})
	t.Run("Concurrency", func(t *testing.T) {
		testConcurrency(t, run)
	})
}

func testAfterFunc(t *testing.T, run RunFunc) {
	t.Run("executes function", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			executed := false
			d.AfterFunc(5*time.Millisecond, func() {
				executed = true
			})
			require.NoError(t, h.Advance(5*time.Millisecond))
			assert.True(t, executed, "function should be executed")
		})
	})

	t.Run("executes once", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			count := 0
			d.AfterFunc(5*time.Millisecond, func() {
				count++
			})
			require.NoError(t, h.Advance(5*time.Millisecond))
			assert.Equal(t, 1, count)
			require.NoError(t, h.Advance(50*time.Millisecond))
			assert.Equal(t, 1, count, "function should execute exactly once")
		})
	})
}

func testTimerStop(t *testing.T, run RunFunc) {
	t.Run("before execution", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			executed := false
			timer := d.AfterFunc(20*time.Millisecond, func() {
				executed = true
			})
			assert.True(t, timer.Stop(), "Stop() should return true when stopping before execution")
			require.NoError(t, h.Advance(50*time.Millisecond))
			assert.False(t, executed, "function should not execute after being stopped")
		})
	})

	t.Run("after execution", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			count := 0
			timer := d.AfterFunc(1*time.Millisecond, func() {
				count++
			})
			require.NoError(t, h.Advance(1*time.Millisecond))
			assert.False(t, timer.Stop(), "Stop() should return false when stopping after execution")
			assert.Equal(t, 1, count, "function should execute exactly once")
		})
	})

	t.Run("multiple calls", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			executed := false
			timer := d.AfterFunc(20*time.Millisecond, func() {
				executed = true
			})
			assert.True(t, timer.Stop(), "first Stop() should return true")
			assert.False(t, timer.Stop(), "second Stop() should return false")
			assert.False(t, timer.Stop(), "third Stop() should return false")
			require.NoError(t, h.Advance(50*time.Millisecond))
			assert.False(t, executed, "function should not execute after being stopped")
		})
	})

	t.Run("stop from callback", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			targetExecuted := false
			var stopResult bool
			target := d.AfterFunc(500*time.Millisecond, func() {
				targetExecuted = true
			})
			d.AfterFunc(100*time.Millisecond, func() {
				stopResult = target.Stop()
			})
			require.NoError(t, h.Advance(500*time.Millisecond))
			assert.True(t, stopResult, "Stop() from callback should return true")
			assert.False(t, targetExecuted, "stopped timer should not execute")
		})
	})
}

func testMultipleTimers(t *testing.T, run RunFunc) {
	t.Run("different delays", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var order []int
			d.AfterFunc(30*time.Millisecond, func() {
				order = append(order, 3)
			})
			d.AfterFunc(10*time.Millisecond, func() {
				order = append(order, 1)
			})
			d.AfterFunc(20*time.Millisecond, func() {
				order = append(order, 2)
			})
			require.NoError(t, h.Advance(30*time.Millisecond))
			assert.Equal(t, []int{1, 2, 3}, order, "functions should execute in order of their delays")
		})
	})

	t.Run("sequential execution", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			counter := 0
			d.AfterFunc(10*time.Millisecond, func() {
				counter += 2
			})
			d.AfterFunc(10*time.Millisecond, func() {
				counter += 3
			})
			require.NoError(t, h.Advance(10*time.Millisecond))
			assert.Equal(t, 5, counter, "counter should be 5 (sequential execution, no race conditions)")
		})
	})
}

func testEdgeCases(t *testing.T, run RunFunc) {
	t.Run("zero duration", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			executed := false
			timer := d.AfterFunc(0, func() {
				executed = true
			})
			require.NotNil(t, timer, "Timer should be returned even for zero duration")
			require.NoError(t, h.Advance(0))
			assert.True(t, executed, "function should execute for zero duration")
		})
	})

	t.Run("negative duration", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			executed := false
			timer := d.AfterFunc(-time.Second, func() {
				executed = true
			})
			require.NotNil(t, timer, "Timer should be returned even for negative duration")
			require.NoError(t, h.Advance(0))
			assert.True(t, executed, "function should execute for negative duration")
		})
	})

	t.Run("max duration", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			timer := d.AfterFunc(time.Duration(1<<63-1), func() {})
			require.NotNil(t, timer, "Timer should be returned for max duration")
			assert.True(t, timer.Stop(), "Timer with max duration should be cancellable")
		})
	})
}

func testPanic(t *testing.T, run RunFunc) {
	t.Run("caught", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic("boom")
			})
			err := h.Advance(5 * time.Millisecond)
			assert.ErrorContains(t, err, "panic: boom")
		})
	})

	t.Run("subsequent task not executed", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			normalExecuted := false
			d.AfterFunc(100*time.Millisecond, func() {
				panic("boom")
			})
			d.AfterFunc(200*time.Millisecond, func() {
				normalExecuted = true
			})
			err := h.Advance(200 * time.Millisecond)
			assert.ErrorContains(t, err, "panic: boom")
			assert.False(t, normalExecuted, "subsequent task should not execute after panic")
		})
	})

	t.Run("only first panic reported", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic("first panic")
			})
			d.AfterFunc(10*time.Millisecond, func() {
				panic("second panic")
			})
			err := h.Advance(10 * time.Millisecond)
			assert.ErrorContains(t, err, "panic: first panic")
		})
	})

	t.Run("nil function", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, nil)
			assert.Error(t, h.Advance(5*time.Millisecond))
		})
	})

	t.Run("nil panic", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic(nil)
			})
			err := h.Advance(5 * time.Millisecond)
			assert.ErrorContains(t, err, "panic: panic called with nil argument")
		})
	})

	t.Run("string panic", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic("text")
			})
			err := h.Advance(5 * time.Millisecond)
			assert.ErrorContains(t, err, "panic: text")
		})
	})

	t.Run("int panic", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic(42)
			})
			err := h.Advance(5 * time.Millisecond)
			assert.ErrorContains(t, err, "panic: 42")
		})
	})
}

func testConcurrency(t *testing.T, run RunFunc) {
	t.Run("concurrent AfterFunc and half stop", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const numGoroutines = 1000
			executedCount := 0

			var wg sync.WaitGroup
			wg.Add(numGoroutines)
			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()
					timer := d.AfterFunc(10*time.Millisecond, func() {
						executedCount++
					})
					if i%2 == 0 {
						timer.Stop()
					}
				}()
			}
			wg.Wait()

			require.NoError(t, h.Advance(10*time.Millisecond))
			assert.Equal(t, numGoroutines/2, executedCount, "half of tasks should execute")
		})
	})

	t.Run("concurrent stop", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const numGoroutines = 100
			executed := false

			timer := d.AfterFunc(20*time.Millisecond, func() {
				executed = true
			})

			var wg sync.WaitGroup
			var successfulStops atomic.Int32
			wg.Add(numGoroutines)
			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()
					if timer.Stop() {
						successfulStops.Add(1)
					}
				}()
			}
			wg.Wait()

			assert.Equal(t, int32(1), successfulStops.Load(), "only one Stop() should succeed")
			require.NoError(t, h.Advance(100*time.Millisecond))
			assert.False(t, executed, "function should not execute after being stopped")
		})
	})
}
