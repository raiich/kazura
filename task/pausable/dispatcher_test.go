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

type testEnv struct {
	base        *eventloop.Dispatcher
	d           *Dispatcher
	currentTime time.Time
}

func newTestEnv() *testEnv {
	baseTime := time.Unix(0, 0)
	env := &testEnv{currentTime: baseTime}
	env.base = eventloop.NewDispatcher(baseTime)
	env.d = NewDispatcher(env.base, func() time.Time { return env.currentTime })
	return env
}

// advance moves both currentTime and eventloop forward by d.
// During Pause all base timers are stopped, so advancing is safe.
func (e *testEnv) advance(t *testing.T, d time.Duration) {
	t.Helper()
	e.currentTime = e.currentTime.Add(d)
	require.NoError(t, e.base.FastForward(e.currentTime))
}

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T, f func(t *testing.T, d task.Dispatcher, h *tasktest.TestHelper)) {
		baseTime := time.Unix(0, 0)
		currentTime := baseTime
		base := eventloop.NewDispatcher(baseTime)
		dispatcher := NewDispatcher(base, func() time.Time { return currentTime })
		f(t, dispatcher, &tasktest.TestHelper{
			Advance: func(dur time.Duration) error {
				currentTime = currentTime.Add(dur)
				return base.FastForward(currentTime)
			},
		})
	})
}

func TestTimer_Stop_DuringPause(t *testing.T) {
	env := newTestEnv()
	executed := false

	timer := env.d.AfterFunc(10*time.Second, func() {
		executed = true
	})

	env.advance(t, 3*time.Second)
	require.NoError(t, env.d.Pause())
	assert.True(t, timer.Stop())

	env.advance(t, 47*time.Second)
	require.NoError(t, env.d.Resume())
	env.advance(t, 100*time.Second)
	assert.False(t, executed)
}

func TestDispatcher_Pause(t *testing.T) {
	t.Run("stops all timers", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		env.advance(t, 3*time.Second)
		require.NoError(t, env.d.Pause())
		env.advance(t, 100*time.Second)
		assert.False(t, executed)
	})

	t.Run("already fired timers are unaffected", func(t *testing.T) {
		env := newTestEnv()
		f1Executed := false
		f2Executed := false

		env.d.AfterFunc(3*time.Second, func() {
			f1Executed = true
		})
		env.d.AfterFunc(10*time.Second, func() {
			f2Executed = true
		})

		env.advance(t, 3*time.Second)
		assert.True(t, f1Executed)

		require.NoError(t, env.d.Pause())
		env.advance(t, 100*time.Second)
		assert.False(t, f2Executed)
	})

	t.Run("double pause returns error", func(t *testing.T) {
		env := newTestEnv()
		require.NoError(t, env.d.Pause())
		assert.ErrorContains(t, env.d.Pause(), "already paused")
	})
}

func TestDispatcher_Resume(t *testing.T) {
	t.Run("reschedules with remaining duration", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		env.advance(t, 3*time.Second)
		require.NoError(t, env.d.Pause())

		// remaining = 10s - 3s = 7s
		env.advance(t, 97*time.Second)
		require.NoError(t, env.d.Resume())

		env.advance(t, 6*time.Second)
		assert.False(t, executed, "should not fire before remaining duration")

		env.advance(t, 1*time.Second)
		assert.True(t, executed, "should fire at remaining duration")
	})

	t.Run("pause duration does not affect remaining time", func(t *testing.T) {
		for _, pauseDuration := range []time.Duration{7 * time.Second, 997 * time.Second} {
			env := newTestEnv()
			executed := false

			env.d.AfterFunc(10*time.Second, func() {
				executed = true
			})

			env.advance(t, 3*time.Second)
			require.NoError(t, env.d.Pause())

			env.advance(t, pauseDuration)
			require.NoError(t, env.d.Resume())

			env.advance(t, 7*time.Second)
			assert.True(t, executed, "remaining should be 7s regardless of pause duration (%v)", pauseDuration)
		}
	})

	t.Run("resume without pause returns error", func(t *testing.T) {
		env := newTestEnv()
		assert.ErrorContains(t, env.d.Resume(), "not paused")
	})
}

func TestDispatcher_MultipleTimers_PauseResume(t *testing.T) {
	t.Run("different remaining durations", func(t *testing.T) {
		env := newTestEnv()
		f1Executed := false
		f2Executed := false

		env.d.AfterFunc(10*time.Second, func() {
			f1Executed = true
		})
		env.d.AfterFunc(20*time.Second, func() {
			f2Executed = true
		})

		// Pause after 5s: f1 remaining=5s, f2 remaining=15s
		env.advance(t, 5*time.Second)
		require.NoError(t, env.d.Pause())

		env.advance(t, 45*time.Second)
		require.NoError(t, env.d.Resume())

		env.advance(t, 5*time.Second)
		assert.True(t, f1Executed, "f1 should fire at remaining 5s")
		assert.False(t, f2Executed, "f2 should not fire yet")

		env.advance(t, 10*time.Second)
		assert.True(t, f2Executed, "f2 should fire at remaining 15s")
	})
}

func TestDispatcher_AfterFuncDuringPause(t *testing.T) {
	t.Run("fires with full delay after resume", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		require.NoError(t, env.d.Pause())
		env.d.AfterFunc(5*time.Second, func() {
			executed = true
		})

		env.advance(t, 50*time.Second)
		require.NoError(t, env.d.Resume())

		env.advance(t, 5*time.Second)
		assert.True(t, executed)
	})

	t.Run("stop before resume", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		require.NoError(t, env.d.Pause())
		timer := env.d.AfterFunc(5*time.Second, func() {
			executed = true
		})

		assert.True(t, timer.Stop())

		env.advance(t, 50*time.Second)
		require.NoError(t, env.d.Resume())
		env.advance(t, 100*time.Second)
		assert.False(t, executed)
	})
}

func TestDispatcher_MultipleCycles(t *testing.T) {
	t.Run("remaining accumulates correctly", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(10*time.Second, func() {
			executed = true
		})

		// Cycle 1: 3s elapsed, remaining = 7s
		env.advance(t, 3*time.Second)
		require.NoError(t, env.d.Pause())

		// Cycle 2: resume, run for 2s, remaining = 5s
		env.advance(t, 17*time.Second)
		require.NoError(t, env.d.Resume())
		env.advance(t, 2*time.Second)
		require.NoError(t, env.d.Pause())

		// Cycle 3: resume
		env.advance(t, 28*time.Second)
		require.NoError(t, env.d.Resume())

		env.advance(t, 4*time.Second)
		assert.False(t, executed, "should not fire before remaining 5s")

		env.advance(t, 1*time.Second)
		assert.True(t, executed, "should fire at remaining 5s")
	})
}

func TestDispatcher_EdgeCases(t *testing.T) {
	t.Run("pause and resume with no timers", func(t *testing.T) {
		env := newTestEnv()
		require.NoError(t, env.d.Pause())
		require.NoError(t, env.d.Resume())
	})

	t.Run("zero duration pause resume", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(0, func() {
			executed = true
		})

		require.NoError(t, env.d.Pause())
		env.advance(t, 50*time.Second)
		require.NoError(t, env.d.Resume())
		env.advance(t, 0)
		assert.True(t, executed)
	})

	t.Run("negative duration pause resume", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(-time.Second, func() {
			executed = true
		})

		require.NoError(t, env.d.Pause())
		env.advance(t, 50*time.Second)
		require.NoError(t, env.d.Resume())
		env.advance(t, 0)
		assert.True(t, executed)
	})
}

func TestDispatcher_Concurrency(t *testing.T) {
	t.Run("registered timers survive pause resume", func(t *testing.T) {
		env := newTestEnv()
		const numGoroutines = 1000
		var executedCount atomic.Int32

		// Phase 1: register all timers
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				env.d.AfterFunc(10*time.Millisecond, func() {
					executedCount.Add(1)
				})
			}()
		}
		wg.Wait()

		// Phase 2: pause/resume cycle (single goroutine)
		env.advance(t, 5*time.Millisecond)
		require.NoError(t, env.d.Pause())
		env.advance(t, 1*time.Second)
		require.NoError(t, env.d.Resume())

		env.advance(t, 5*time.Millisecond) // remaining 5ms
		assert.Equal(t, int32(numGoroutines), executedCount.Load())
	})
}

func TestDispatcher_RapidToggle(t *testing.T) {
	env := newTestEnv()
	executed := false

	env.d.AfterFunc(1*time.Second, func() {
		executed = true
	})

	// 300ms elapsed, remaining = 700ms
	env.advance(t, 300*time.Millisecond)
	require.NoError(t, env.d.Pause())

	// Resume immediately and Pause again (0ms between)
	require.NoError(t, env.d.Resume())
	require.NoError(t, env.d.Pause())

	// Resume, remaining should still be 700ms
	require.NoError(t, env.d.Resume())

	env.advance(t, 699*time.Millisecond)
	assert.False(t, executed, "should not fire before remaining 700ms")

	env.advance(t, 1*time.Millisecond)
	assert.True(t, executed, "should fire at remaining 700ms")
}

func TestDispatcher_PauseCallbackRace(t *testing.T) {
	t.Run("pause after timer fires at exact time", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(100*time.Millisecond, func() {
			executed = true
		})

		env.advance(t, 100*time.Millisecond) // timer fires
		assert.True(t, executed)

		require.NoError(t, env.d.Pause())
		assert.Equal(t, 0, env.d.TrackedCount())
	})

	t.Run("pause just before timer fires", func(t *testing.T) {
		env := newTestEnv()
		executed := false

		env.d.AfterFunc(100*time.Millisecond, func() {
			executed = true
		})

		env.advance(t, 99*time.Millisecond)
		assert.False(t, executed)

		require.NoError(t, env.d.Pause())
		assert.Equal(t, 1, env.d.TrackedCount())

		// Verify timer resumes correctly with remaining 1ms
		require.NoError(t, env.d.Resume())
		env.advance(t, 1*time.Millisecond)
		assert.True(t, executed)
	})
}

func TestDispatcher_TrackedCleanup(t *testing.T) {
	env := newTestEnv()
	const numTimers = 1000

	for i := 0; i < numTimers; i++ {
		env.d.AfterFunc(10*time.Millisecond, func() {})
	}

	env.advance(t, 10*time.Millisecond) // all fire
	assert.Equal(t, 0, env.d.TrackedCount(), "tracked map should be empty after all timers fire")

	// Pause on empty tracked should succeed
	require.NoError(t, env.d.Pause())
	assert.Equal(t, 0, env.d.TrackedCount())
}

func TestDispatcher_ConcurrentPause(t *testing.T) {
	env := newTestEnv()

	for i := 0; i < 10; i++ {
		env.d.AfterFunc(time.Duration(i+1)*time.Second, func() {})
	}

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errCount atomic.Int32

	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if err := env.d.Pause(); err != nil {
				errCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), successCount.Load(), "only one Pause should succeed")
	assert.Equal(t, int32(1), errCount.Load(), "other Pause should return error")
}
