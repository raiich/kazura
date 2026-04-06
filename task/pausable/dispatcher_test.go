package pausable

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/eventloop"
	"github.com/raiich/kazura/task/tasktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T, f func(t *testing.T, d task.Dispatcher, h *tasktest.TestHelper)) {
		start := time.Unix(0, 0)
		base := eventloop.NewDispatcher(start)
		currentTime := start
		d := NewDispatcher(base, func() time.Time { return currentTime })
		f(t, d, &tasktest.TestHelper{
			Start: start,
			AdvanceToFunc: func(to time.Time) error {
				currentTime = to
				return base.FastForward(to)
			},
		})
	})
}

// pausableHelper provides duration-based Advance for pausable-specific tests.
type pausableHelper struct {
	currentTime time.Time
	dispatcher  *eventloop.Dispatcher
}

func (h *pausableHelper) Advance(d time.Duration) error {
	h.currentTime = h.currentTime.Add(d)
	return h.dispatcher.FastForward(h.currentTime)
}

func newPausableTest() (*Dispatcher, *pausableHelper) {
	baseTime := time.Unix(0, 0)
	base := eventloop.NewDispatcher(baseTime)
	h := &pausableHelper{currentTime: baseTime, dispatcher: base}
	d := NewDispatcher(base, func() time.Time { return h.currentTime })
	return d, h
}

func TestTimer_Stop_DuringPause(t *testing.T) {
	d, h := newPausableTest()
	executed := false

	timer := d.AfterFunc(10*time.Second, func() {
		executed = true
	})

	require.NoError(t, h.Advance(3*time.Second))
	require.NoError(t, d.Pause())
	assert.True(t, timer.Stop())

	require.NoError(t, h.Advance(47*time.Second))
	require.NoError(t, d.Resume())
	require.NoError(t, h.Advance(100*time.Second))
	assert.False(t, executed)
}

func TestDispatcher_Pause(t *testing.T) {
	t.Run("stops all timers", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.Advance(3*time.Second))
		require.NoError(t, d.Pause())
		require.NoError(t, h.Advance(100*time.Second))
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

		require.NoError(t, h.Advance(3*time.Second))
		assert.True(t, f1Executed)

		require.NoError(t, d.Pause())
		require.NoError(t, h.Advance(100*time.Second))
		assert.False(t, f2Executed)
	})

	t.Run("double pause returns error", func(t *testing.T) {
		d, _ := newPausableTest()
		require.NoError(t, d.Pause())
		assert.ErrorContains(t, d.Pause(), "already paused")
	})
}

func TestDispatcher_Resume(t *testing.T) {
	t.Run("reschedules with remaining duration", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.Advance(3*time.Second))
		require.NoError(t, d.Pause())

		// remaining = 10s - 3s = 7s
		require.NoError(t, h.Advance(97*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.Advance(6*time.Second))
		assert.False(t, executed, "should not fire before remaining duration")

		require.NoError(t, h.Advance(1*time.Second))
		assert.True(t, executed, "should fire at remaining duration")
	})

	t.Run("pause duration does not affect remaining time", func(t *testing.T) {
		for _, pauseDuration := range []time.Duration{7 * time.Second, 997 * time.Second} {
			d, h := newPausableTest()
			executed := false

			d.AfterFunc(10*time.Second, func() {
				executed = true
			})

			require.NoError(t, h.Advance(3*time.Second))
			require.NoError(t, d.Pause())

			require.NoError(t, h.Advance(pauseDuration))
			require.NoError(t, d.Resume())

			require.NoError(t, h.Advance(7*time.Second))
			assert.True(t, executed, "remaining should be 7s regardless of pause duration (%v)", pauseDuration)
		}
	})

	t.Run("resume without pause returns error", func(t *testing.T) {
		d, _ := newPausableTest()
		assert.ErrorContains(t, d.Resume(), "not paused")
	})
}

func TestDispatcher_MultipleTimers_PauseResume(t *testing.T) {
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
		require.NoError(t, h.Advance(5*time.Second))
		require.NoError(t, d.Pause())

		require.NoError(t, h.Advance(45*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.Advance(5*time.Second))
		assert.True(t, f1Executed, "f1 should fire at remaining 5s")
		assert.False(t, f2Executed, "f2 should not fire yet")

		require.NoError(t, h.Advance(10*time.Second))
		assert.True(t, f2Executed, "f2 should fire at remaining 15s")
	})
}

func TestDispatcher_AfterFuncDuringPause(t *testing.T) {
	t.Run("does not fire before resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		require.NoError(t, d.Pause())
		d.AfterFunc(50*time.Millisecond, func() {
			executed = true
		})

		require.NoError(t, h.Advance(100*time.Millisecond))
		assert.False(t, executed, "buffered afterFunc should not fire before resume")
	})

	t.Run("fires with full delay after resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		require.NoError(t, d.Pause())
		d.AfterFunc(5*time.Second, func() {
			executed = true
		})

		require.NoError(t, h.Advance(50*time.Second))
		require.NoError(t, d.Resume())
		assert.False(t, executed)

		require.NoError(t, h.Advance(5*time.Second))
		assert.True(t, executed)
	})

	t.Run("reschedules with original delay after resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		require.NoError(t, d.Pause())
		d.AfterFunc(50*time.Millisecond, func() {
			executed = true
		})
		require.NoError(t, d.Resume())

		require.NoError(t, h.Advance(50*time.Millisecond))
		assert.True(t, executed, "buffered task should reschedule with original delay")
	})

	t.Run("stop before resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		require.NoError(t, d.Pause())
		timer := d.AfterFunc(5*time.Second, func() {
			executed = true
		})

		assert.True(t, timer.Stop())

		require.NoError(t, h.Advance(50*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.Advance(100*time.Second))
		assert.False(t, executed)
	})
}

func TestDispatcher_MultipleCycles(t *testing.T) {
	t.Run("remaining accumulates correctly", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		// Cycle 1: 3s elapsed, remaining = 7s
		require.NoError(t, h.Advance(3*time.Second))
		require.NoError(t, d.Pause())

		// Cycle 2: resume, run for 2s, remaining = 5s
		require.NoError(t, h.Advance(17*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.Advance(2*time.Second))
		require.NoError(t, d.Pause())

		// Cycle 3: resume
		require.NoError(t, h.Advance(28*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.Advance(4*time.Second))
		assert.False(t, executed, "should not fire before remaining 5s")

		require.NoError(t, h.Advance(1*time.Second))
		assert.True(t, executed, "should fire at remaining 5s")
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
		require.NoError(t, h.Advance(50*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.Advance(0))
		assert.True(t, executed)
	})

	t.Run("negative duration pause resume", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(-time.Second, func() {
			executed = true
		})

		require.NoError(t, d.Pause())
		require.NoError(t, h.Advance(50*time.Second))
		require.NoError(t, d.Resume())
		require.NoError(t, h.Advance(0))
		assert.True(t, executed)
	})
}

func TestDispatcher_RapidToggle(t *testing.T) {
	d, h := newPausableTest()
	executed := false

	d.AfterFunc(1*time.Second, func() {
		executed = true
	})

	// 300ms elapsed, remaining = 700ms
	require.NoError(t, h.Advance(300*time.Millisecond))
	require.NoError(t, d.Pause())

	// Resume immediately and Pause again (0ms between)
	require.NoError(t, d.Resume())
	require.NoError(t, d.Pause())

	// Resume, remaining should still be 700ms
	require.NoError(t, d.Resume())

	require.NoError(t, h.Advance(699*time.Millisecond))
	assert.False(t, executed, "should not fire before remaining 700ms")

	require.NoError(t, h.Advance(1*time.Millisecond))
	assert.True(t, executed, "should fire at remaining 700ms")
}

func TestDispatcher_PauseCallbackRace(t *testing.T) {
	t.Run("pause after timer fires at exact time", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(100*time.Millisecond, func() {
			executed = true
		})

		require.NoError(t, h.Advance(100*time.Millisecond)) // timer fires
		assert.True(t, executed)

		require.NoError(t, d.Pause())
		assert.Equal(t, 0, d.TrackedCount())
	})

	t.Run("pause just before timer fires", func(t *testing.T) {
		d, h := newPausableTest()
		executed := false

		d.AfterFunc(100*time.Millisecond, func() {
			executed = true
		})

		require.NoError(t, h.Advance(99*time.Millisecond))
		assert.False(t, executed)

		require.NoError(t, d.Pause())
		assert.Equal(t, 1, d.TrackedCount())

		// Verify timer resumes correctly with remaining 1ms
		require.NoError(t, d.Resume())
		require.NoError(t, h.Advance(1*time.Millisecond))
		assert.True(t, executed)
	})
}

func TestDispatcher_TrackedCleanup(t *testing.T) {
	d, h := newPausableTest()
	const numTimers = 1000

	for i := 0; i < numTimers; i++ {
		d.AfterFunc(10*time.Millisecond, func() {})
	}

	require.NoError(t, h.Advance(10*time.Millisecond)) // all fire
	assert.Equal(t, 0, d.TrackedCount(), "tracked map should be empty after all timers fire")

	// Pause on empty tracked should succeed
	require.NoError(t, d.Pause())
	assert.Equal(t, 0, d.TrackedCount())
}

func TestDispatcher_Callback(t *testing.T) {
	t.Run("afterFunc in callback delegates to base", func(t *testing.T) {
		d, h := newPausableTest()
		nested := false

		d.AfterFunc(10*time.Millisecond, func() {
			d.AfterFunc(20*time.Millisecond, func() {
				nested = true
			})
		})

		require.NoError(t, h.Advance(10*time.Millisecond))
		assert.False(t, nested)
		assert.Equal(t, 1, d.TrackedCount(), "nested timer should be tracked")

		require.NoError(t, h.Advance(20*time.Millisecond))
		assert.True(t, nested, "afterFunc in callback should delegate to base")
		assert.Equal(t, 0, d.TrackedCount(), "tracked should be empty after execution")
	})

	t.Run("pause in callback buffers subsequent afterFunc", func(t *testing.T) {
		d, h := newPausableTest()
		buffered := false

		d.AfterFunc(10*time.Millisecond, func() {
			assert.NoError(t, d.Pause())
			d.AfterFunc(20*time.Millisecond, func() {
				buffered = true
			})
		})

		require.NoError(t, h.Advance(10*time.Millisecond))
		assert.False(t, buffered)

		require.NoError(t, d.Resume())
		require.NoError(t, h.Advance(20*time.Millisecond))
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
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				d.AfterFunc(10*time.Millisecond, func() {
					executedCount.Add(1)
				})
			}()
		}
		wg.Wait()

		// Phase 2: pause/resume cycle (single goroutine)
		require.NoError(t, h.Advance(5*time.Millisecond))
		require.NoError(t, d.Pause())
		require.NoError(t, h.Advance(1*time.Second))
		require.NoError(t, d.Resume())

		require.NoError(t, h.Advance(5*time.Millisecond)) // remaining 5ms
		assert.Equal(t, int32(numGoroutines), executedCount.Load())
	})

	t.Run("concurrent pause", func(t *testing.T) {
		d, _ := newPausableTest()

		for i := 0; i < 10; i++ {
			d.AfterFunc(time.Duration(i+1)*time.Second, func() {})
		}

		var wg sync.WaitGroup
		var successCount atomic.Int32
		var errCount atomic.Int32

		wg.Add(2)
		for i := 0; i < 2; i++ {
			go func() {
				defer wg.Done()
				if err := d.Pause(); err != nil {
					errCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}()
		}
		wg.Wait()

		assert.Equal(t, int32(1), successCount.Load(), "only one Pause should succeed")
		assert.Equal(t, int32(1), errCount.Load(), "other Pause should return error")
	})
}

func TestDispatcher_RemainingClampedToZero(t *testing.T) {
	// This test needs direct access to currentTime and base to simulate
	// clock skew (currentTime advanced without base), so it sets up manually.
	baseTime := time.Unix(0, 0)
	currentTime := baseTime
	base := eventloop.NewDispatcher(baseTime)
	d := NewDispatcher(base, func() time.Time { return currentTime })
	executed := false

	d.AfterFunc(100*time.Millisecond, func() {
		executed = true
	})

	// Advance only currentTime (not base) so that elapsed (200ms) > delay (100ms).
	// This makes Pause compute negative remaining, which should be clamped to 0.
	currentTime = currentTime.Add(200 * time.Millisecond)
	require.NoError(t, d.Pause())
	require.NoError(t, d.Resume())

	require.NoError(t, base.FastForward(currentTime))
	assert.True(t, executed, "remaining should be clamped to 0 and fire immediately")
}
