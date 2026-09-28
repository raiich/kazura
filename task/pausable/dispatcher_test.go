package pausable

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
	"github.com/raiich/kazura/task/virtualtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T) (task.Dispatcher, *tasktest.TestHelper) {
		d, h := newPausableTest()
		return d, &tasktest.TestHelper{
			Start:     h.currentTime,
			AdvanceTo: h.AdvanceTo,
		}
	})
}

// pausableHelper is the clock the Dispatcher under test reads, backed by a
// virtualtime dispatcher that runs the callbacks.
type pausableHelper struct {
	currentTime time.Time
	dispatcher  *virtualtime.Dispatcher
}

// AdvanceTo moves the clock to the absolute time to and runs the callbacks due
// by then.
func (h *pausableHelper) AdvanceTo(to time.Time) error {
	h.currentTime = to
	return h.dispatcher.FastForward(to)
}

// AdvanceBy moves the clock forward by d.
func (h *pausableHelper) AdvanceBy(d time.Duration) error {
	return h.AdvanceTo(h.currentTime.Add(d))
}

func newPausableTest() (*Dispatcher, *pausableHelper) {
	baseTime := time.Unix(0, 0)
	base := virtualtime.NewDispatcher(baseTime)
	h := &pausableHelper{currentTime: baseTime, dispatcher: base}
	d := NewDispatcher(base, func() time.Time { return h.currentTime })
	return d, h
}

func TestTimer_Stop(t *testing.T) {
	t.Run("registered before pause", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		timer := d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.AdvanceBy(3*time.Second))
		require.NoError(t, d.Pause())
		assert.True(t, timer.Stop())

		require.NoError(t, h.AdvanceBy(47*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.AdvanceBy(100*time.Second))
		assert.False(t, executed)
	})

	t.Run("registered during pause", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		require.NoError(t, d.Pause())
		timer := d.AfterFunc(5*time.Second, func() {
			executed = true
		})

		assert.True(t, timer.Stop())

		require.NoError(t, h.AdvanceBy(50*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.AdvanceBy(100*time.Second))
		assert.False(t, executed)
	})
}

func TestDispatcher_Pause(t *testing.T) {
	t.Run("stops all timers", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.AdvanceBy(3*time.Second))
		require.NoError(t, d.Pause())
		require.NoError(t, h.AdvanceBy(100*time.Second))
		assert.False(t, executed)
	})

	t.Run("already fired timers are unaffected", func(t *testing.T) {
		d, h := newPausableTest()
		f1Executed := false
		f2Executed := false

		d.AfterFunc(3*time.Second, func() {
			f1Executed = true
		})
		d.AfterFunc(10*time.Second, func() {
			f2Executed = true
		})

		require.NoError(t, h.AdvanceBy(3*time.Second))
		assert.True(t, f1Executed)

		require.NoError(t, d.Pause())
		require.NoError(t, h.AdvanceBy(100*time.Second))
		assert.False(t, f2Executed)
	})

	t.Run("double pause returns error", func(t *testing.T) {
		d, _ := newPausableTest()
		require.NoError(t, d.Pause())
		assert.ErrorContains(t, d.Pause(), "already paused")
	})
}

func TestDispatcher_Resume(t *testing.T) {
	t.Run("resume without pause returns error", func(t *testing.T) {
		d, _ := newPausableTest()
		assert.ErrorContains(t, d.Resume(), "not paused")
	})
}

func TestDispatcher_Remaining(t *testing.T) {
	t.Run("reschedules with remaining duration", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.AdvanceBy(3*time.Second))
		require.NoError(t, d.Pause())

		// remaining = 10s - 3s = 7s
		require.NoError(t, h.AdvanceBy(97*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(6*time.Second))
		assert.False(t, executed, "should not fire before remaining duration")

		require.NoError(t, h.AdvanceBy(1*time.Second))
		assert.True(t, executed, "should fire at remaining duration")
	})

	t.Run("different remaining durations", func(t *testing.T) {
		d, h := newPausableTest()
		f1Executed := false
		f2Executed := false

		d.AfterFunc(10*time.Second, func() {
			f1Executed = true
		})
		d.AfterFunc(20*time.Second, func() {
			f2Executed = true
		})

		// Pause after 5s: f1 remaining=5s, f2 remaining=15s
		require.NoError(t, h.AdvanceBy(5*time.Second))
		require.NoError(t, d.Pause())

		require.NoError(t, h.AdvanceBy(45*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(5*time.Second))
		assert.True(t, f1Executed, "f1 should fire at remaining 5s")
		assert.False(t, f2Executed, "f2 should not fire yet")

		require.NoError(t, h.AdvanceBy(10*time.Second))
		assert.True(t, f2Executed, "f2 should fire at remaining 15s")
	})

	t.Run("accumulates across cycles", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		// Cycle 1: 3s elapsed, remaining = 7s
		require.NoError(t, h.AdvanceBy(3*time.Second))
		require.NoError(t, d.Pause())

		// Cycle 2: resume, run for 2s, remaining = 5s
		require.NoError(t, h.AdvanceBy(17*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.AdvanceBy(2*time.Second))
		require.NoError(t, d.Pause())

		// Cycle 3: resume
		require.NoError(t, h.AdvanceBy(28*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(4*time.Second))
		assert.False(t, executed, "should not fire before remaining 5s")

		require.NoError(t, h.AdvanceBy(1*time.Second))
		assert.True(t, executed, "should fire at remaining 5s")
	})

	t.Run("rapid toggle", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(1*time.Second, func() {
			executed = true
		})

		// 300ms elapsed, remaining = 700ms
		require.NoError(t, h.AdvanceBy(300*time.Millisecond))
		require.NoError(t, d.Pause())

		// Resume immediately and Pause again (0ms between)
		require.NoError(t, d.Resume())
		require.NoError(t, d.Pause())

		// Resume, remaining should still be 700ms
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(699*time.Millisecond))
		assert.False(t, executed, "should not fire before remaining 700ms")

		require.NoError(t, h.AdvanceBy(1*time.Millisecond))
		assert.True(t, executed, "should fire at remaining 700ms")
	})

	t.Run("pause just before fire", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(100*time.Millisecond, func() { executed = true })

		require.NoError(t, h.AdvanceBy(99*time.Millisecond))
		require.NoError(t, d.Pause())
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(0))
		assert.False(t, executed, "should not fire before the remaining 1ms")

		require.NoError(t, h.AdvanceBy(1*time.Millisecond))
		assert.True(t, executed, "should fire once the remaining 1ms elapses")
	})

	t.Run("clamped to zero", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(100*time.Millisecond, func() {
			executed = true
		})

		// Skew the clock: advance only currentTime (not base) so that elapsed (200ms)
		// > delay (100ms). This makes Pause compute negative remaining, which should
		// be clamped to 0.
		h.currentTime = h.currentTime.Add(200 * time.Millisecond)
		require.NoError(t, d.Pause())
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(0))
		assert.True(t, executed, "remaining should be clamped to 0 and fire immediately")
	})
}

func TestDispatcher_AfterFuncDuringPause(t *testing.T) {
	t.Run("fires with full delay after resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		require.NoError(t, d.Pause())
		d.AfterFunc(5*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.AdvanceBy(50*time.Second))
		require.NoError(t, d.Resume())
		assert.False(t, executed)

		require.NoError(t, h.AdvanceBy(5*time.Second))
		assert.True(t, executed)
	})
}

func TestDispatcher_EdgeCases(t *testing.T) {
	t.Run("pause and resume with no timers", func(t *testing.T) {
		d, _ := newPausableTest()
		require.NoError(t, d.Pause())
		require.NoError(t, d.Resume())
	})

	t.Run("zero duration pause resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(0, func() {
			executed = true
		})

		require.NoError(t, d.Pause())
		require.NoError(t, h.AdvanceBy(50*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.AdvanceBy(0))
		assert.True(t, executed)
	})
}

func TestDispatcher_Callback(t *testing.T) {
	t.Run("pause in callback buffers subsequent afterFunc", func(t *testing.T) {
		d, h := newPausableTest()
		buffered := false

		d.AfterFunc(10*time.Millisecond, func() {
			assert.NoError(t, d.Pause())
			d.AfterFunc(20*time.Millisecond, func() {
				buffered = true
			})
		})

		require.NoError(t, h.AdvanceBy(10*time.Millisecond))
		assert.False(t, buffered)

		require.NoError(t, d.Resume())
		require.NoError(t, h.AdvanceBy(20*time.Millisecond))
		assert.True(t, buffered, "afterFunc after pause in callback should be buffered and fire after resume")
	})
}

func TestDispatcher_Concurrency(t *testing.T) {
	t.Run("registered timers survive pause resume", func(t *testing.T) {
		d, h := newPausableTest()
		const numGoroutines = 1000
		var executedCount atomic.Int32

		// Phase 1: register all timers
		var wg sync.WaitGroup
		for range numGoroutines {
			wg.Go(func() {
				d.AfterFunc(10*time.Millisecond, func() {
					executedCount.Add(1)
				})
			})
		}
		wg.Wait()

		// Phase 2: pause/resume cycle (single goroutine)
		require.NoError(t, h.AdvanceBy(5*time.Millisecond))
		require.NoError(t, d.Pause())
		require.NoError(t, h.AdvanceBy(1*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.AdvanceBy(5*time.Millisecond)) // remaining 5ms
		assert.Equal(t, int32(numGoroutines), executedCount.Load())
	})

	t.Run("concurrent pause", func(t *testing.T) {
		d, _ := newPausableTest()

		for i := range 10 {
			d.AfterFunc(time.Duration(i+1)*time.Second, func() {})
		}

		var wg sync.WaitGroup
		var successCount atomic.Int32
		var errCount atomic.Int32

		for range 2 {
			wg.Go(func() {
				if err := d.Pause(); err != nil {
					errCount.Add(1)
				} else {
					successCount.Add(1)
				}
			})
		}
		wg.Wait()

		assert.Equal(t, int32(1), successCount.Load(), "only one Pause should succeed")
		assert.Equal(t, int32(1), errCount.Load(), "other Pause should return error")
	})
}
