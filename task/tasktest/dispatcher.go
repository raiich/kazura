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
	// Start is the initial time of the test dispatcher.
	Start time.Time
	// AdvanceToFunc moves time forward to the given absolute time and ensures all tasks up to that point execute.
	// Returns an error if a dispatched function panicked (for eventloop-style dispatchers).
	AdvanceToFunc func(to time.Time) error
}

func (h *TestHelper) AdvanceTo(to time.Time) error {
	return h.AdvanceToFunc(to)
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
	t.Run("SelectiveStop", func(t *testing.T) {
		testSelectiveStop(t, run)
	})
	t.Run("TimingBoundary", func(t *testing.T) {
		testTimingBoundary(t, run)
	})
	t.Run("DelayedRegistration", func(t *testing.T) {
		testDelayedRegistration(t, run)
	})
	t.Run("NestedScheduling", func(t *testing.T) {
		testNestedScheduling(t, run)
	})
	t.Run("Concurrency", func(t *testing.T) {
		testConcurrency(t, run)
	})
}

func testAfterFunc(t *testing.T, run RunFunc) {
	t.Run("executes once", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Int32
			d.AfterFunc(5*time.Millisecond, func() {
				actual.Add(1)
			})
			require.NoError(t, h.AdvanceTo(h.Start.Add(5*time.Millisecond)))
			assert.Equal(t, int32(1), actual.Load())
			require.NoError(t, h.AdvanceTo(h.Start.Add(55*time.Millisecond)))
			assert.Equal(t, int32(1), actual.Load(), "function should execute exactly once")
		})
	})
}

func testTimerStop(t *testing.T, run RunFunc) {
	t.Run("before execution", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Bool
			timer := d.AfterFunc(20*time.Millisecond, func() {
				actual.Store(true)
			})
			assert.True(t, timer.Stop(), "Stop() should return true when stopping before execution")
			require.NoError(t, h.AdvanceTo(h.Start.Add(50*time.Millisecond)))
			assert.False(t, actual.Load(), "function should not execute after being stopped")
		})
	})

	t.Run("after execution", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Int32
			timer := d.AfterFunc(1*time.Millisecond, func() {
				actual.Add(1)
			})
			require.NoError(t, h.AdvanceTo(h.Start.Add(1*time.Millisecond)))
			assert.False(t, timer.Stop(), "Stop() should return false when stopping after execution")
			assert.Equal(t, int32(1), actual.Load(), "function should execute exactly once")
		})
	})

	t.Run("multiple calls", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Bool
			timer := d.AfterFunc(20*time.Millisecond, func() {
				actual.Store(true)
			})
			assert.True(t, timer.Stop(), "first Stop() should return true")
			assert.False(t, timer.Stop(), "second Stop() should return false")
			assert.False(t, timer.Stop(), "third Stop() should return false")
			require.NoError(t, h.AdvanceTo(h.Start.Add(50*time.Millisecond)))
			assert.False(t, actual.Load(), "function should not execute after being stopped")
		})
	})

	t.Run("stop from callback", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var targetExecuted atomic.Bool
			var stopResult atomic.Bool
			target := d.AfterFunc(500*time.Millisecond, func() {
				targetExecuted.Store(true)
			})
			d.AfterFunc(100*time.Millisecond, func() {
				stopResult.Store(target.Stop())
			})
			require.NoError(t, h.AdvanceTo(h.Start.Add(500*time.Millisecond)))
			assert.True(t, stopResult.Load(), "Stop() from callback should return true")
			assert.False(t, targetExecuted.Load(), "stopped timer should not execute")
		})
	})
}

func testMultipleTimers(t *testing.T, run RunFunc) {
	t.Run("different delays", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const (
				bit1 = 1 << iota
				bit2
				bit3
			)
			var v int32
			var actual atomic.Int32 // for assertion
			d.AfterFunc(30*time.Millisecond, func() { v |= bit3; actual.Store(v) })
			d.AfterFunc(10*time.Millisecond, func() { v |= bit1; actual.Store(v) })
			d.AfterFunc(20*time.Millisecond, func() { v |= bit2; actual.Store(v) })

			require.NoError(t, h.AdvanceTo(h.Start.Add(10*time.Millisecond-1)))
			assert.Equal(t, int32(0), actual.Load(), "no function should have fired yet")

			require.NoError(t, h.AdvanceTo(h.Start.Add(10*time.Millisecond)))
			assert.Equal(t, int32(bit1), actual.Load(), "only first should have fired")

			require.NoError(t, h.AdvanceTo(h.Start.Add(20*time.Millisecond-1)))
			assert.Equal(t, int32(bit1), actual.Load(), "still only first")

			require.NoError(t, h.AdvanceTo(h.Start.Add(20*time.Millisecond)))
			assert.Equal(t, int32(bit1|bit2), actual.Load(), "first and second should have fired")

			require.NoError(t, h.AdvanceTo(h.Start.Add(30*time.Millisecond)))
			assert.Equal(t, int32(bit1|bit2|bit3), actual.Load(), "all should have fired")
		})
	})

	t.Run("sequential execution", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var counter int32
			var actual atomic.Int32 // for assertion
			d.AfterFunc(10*time.Millisecond, func() {
				counter += 2
				actual.Store(counter)
			})
			d.AfterFunc(10*time.Millisecond, func() {
				counter += 3
				actual.Store(counter)
			})
			require.NoError(t, h.AdvanceTo(h.Start.Add(10*time.Millisecond)))
			assert.Equal(t, int32(5), actual.Load(), "counter should be 5 (sequential execution, no race conditions)")
		})
	})
}

func testEdgeCases(t *testing.T, run RunFunc) {
	t.Run("zero duration", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Bool
			timer := d.AfterFunc(0, func() {
				actual.Store(true)
			})
			require.NotNil(t, timer, "Timer should be returned even for zero duration")
			require.NoError(t, h.AdvanceTo(h.Start))
			assert.True(t, actual.Load(), "function should execute for zero duration")
		})
	})

	t.Run("negative duration", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Bool
			timer := d.AfterFunc(-time.Second, func() {
				actual.Store(true)
			})
			require.NotNil(t, timer, "Timer should be returned even for negative duration")
			require.NoError(t, h.AdvanceTo(h.Start))
			assert.True(t, actual.Load(), "function should execute for negative duration")
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
			err := h.AdvanceTo(h.Start.Add(5 * time.Millisecond))
			assert.ErrorContains(t, err, "panic: boom")
		})
	})

	t.Run("subsequent task not executed", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var subsequent atomic.Bool
			d.AfterFunc(100*time.Millisecond, func() {
				panic("boom")
			})
			d.AfterFunc(200*time.Millisecond, func() {
				subsequent.Store(true)
			})
			err := h.AdvanceTo(h.Start.Add(200 * time.Millisecond))
			assert.ErrorContains(t, err, "panic: boom")
			assert.False(t, subsequent.Load(), "subsequent task should not execute after panic")
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
			err := h.AdvanceTo(h.Start.Add(10 * time.Millisecond))
			assert.ErrorContains(t, err, "panic: first panic")
		})
	})

	t.Run("nil function", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, nil)
			assert.Error(t, h.AdvanceTo(h.Start.Add(5*time.Millisecond)))
		})
	})

	t.Run("nil panic", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic(nil)
			})
			err := h.AdvanceTo(h.Start.Add(5 * time.Millisecond))
			assert.ErrorContains(t, err, "panic: panic called with nil argument")
		})
	})

	t.Run("string panic", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic("text")
			})
			err := h.AdvanceTo(h.Start.Add(5 * time.Millisecond))
			assert.ErrorContains(t, err, "panic: text")
		})
	})

	t.Run("int panic", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			d.AfterFunc(5*time.Millisecond, func() {
				panic(42)
			})
			err := h.AdvanceTo(h.Start.Add(5 * time.Millisecond))
			assert.ErrorContains(t, err, "panic: 42")
		})
	})
}

func testSelectiveStop(t *testing.T, run RunFunc) {
	t.Run("stopping one timer does not affect others", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const (
				bitA = 1 << iota
				bitB
				bitC
			)
			var v int32
			var actual atomic.Int32 // for assertion
			d.AfterFunc(10*time.Millisecond, func() { v |= bitA; actual.Store(v) })
			timerB := d.AfterFunc(20*time.Millisecond, func() { v |= bitB; actual.Store(v) })
			d.AfterFunc(30*time.Millisecond, func() { v |= bitC; actual.Store(v) })

			assert.True(t, timerB.Stop(), "Stop() should return true for pending timer")
			require.NoError(t, h.AdvanceTo(h.Start.Add(30*time.Millisecond)))
			assert.Equal(t, int32(bitA|bitC), actual.Load(), "A and C should execute, B should be stopped")
		})
	})
}

func testTimingBoundary(t *testing.T, run RunFunc) {
	t.Run("does not fire before delay elapses but fires at exact delay", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Bool
			d.AfterFunc(100*time.Millisecond, func() {
				actual.Store(true)
			})
			require.NoError(t, h.AdvanceTo(h.Start.Add(99*time.Millisecond)))
			assert.False(t, actual.Load(), "should not fire at delay-1")

			require.NoError(t, h.AdvanceTo(h.Start.Add(100*time.Millisecond)))
			assert.True(t, actual.Load(), "should fire at exact delay")
		})
	})
}

func testDelayedRegistration(t *testing.T, run RunFunc) {
	t.Run("afterFunc after time advanced uses relative delay", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			var actual atomic.Bool

			require.NoError(t, h.AdvanceTo(h.Start.Add(50*time.Millisecond)))
			d.AfterFunc(10*time.Millisecond, func() {
				actual.Store(true)
			})

			require.NoError(t, h.AdvanceTo(h.Start.Add(59*time.Millisecond)))
			assert.False(t, actual.Load(), "should not fire before relative delay")

			require.NoError(t, h.AdvanceTo(h.Start.Add(60*time.Millisecond)))
			assert.True(t, actual.Load(), "should fire at relative delay")
		})
	})
}

func testNestedScheduling(t *testing.T, run RunFunc) {
	t.Run("afterFunc from within callback", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const (
				bit1 = 1 << iota
				bit2
			)
			var v int32
			var actual atomic.Int32 // for assertion
			d.AfterFunc(10*time.Millisecond, func() {
				v |= bit1
				actual.Store(v)
				d.AfterFunc(20*time.Millisecond, func() {
					v |= bit2
					actual.Store(v)
				})
			})

			require.NoError(t, h.AdvanceTo(h.Start.Add(30*time.Millisecond)))
			assert.Equal(t, int32(bit1|bit2), actual.Load(), "both callbacks should have fired")
		})
	})
}

func testConcurrency(t *testing.T, run RunFunc) {
	t.Run("concurrent AfterFunc and half stop", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const numGoroutines = 1000
			executedCount := 0
			var actual atomic.Int32

			var wg sync.WaitGroup
			wg.Add(numGoroutines)
			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()
					timer := d.AfterFunc(10*time.Millisecond, func() {
						executedCount++
						actual.Store(int32(executedCount))
					})
					if i%2 == 0 {
						timer.Stop()
					}
				}()
			}
			wg.Wait()

			require.NoError(t, h.AdvanceTo(h.Start.Add(10*time.Millisecond)))
			assert.Equal(t, int32(numGoroutines/2), actual.Load(), "half of tasks should execute")
		})
	})

	t.Run("concurrent stop", func(t *testing.T) {
		run(t, func(t *testing.T, d task.Dispatcher, h *TestHelper) {
			const numGoroutines = 100
			var actual atomic.Bool

			timer := d.AfterFunc(20*time.Millisecond, func() {
				actual.Store(true)
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
			require.NoError(t, h.AdvanceTo(h.Start.Add(100*time.Millisecond)))
			assert.False(t, actual.Load(), "function should not execute after being stopped")
		})
	})
}
