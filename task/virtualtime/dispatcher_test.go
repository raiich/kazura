package virtualtime

import (
	"runtime"
	"testing"
	"time"
	"weak"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// start is the simulated time every test dispatcher is created at.
var start = time.Unix(0, 0)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T) (task.Dispatcher, *tasktest.TestHelper) {
		dispatcher := NewDispatcher(start)
		return dispatcher, &tasktest.TestHelper{
			Start: start,
			AdvanceTo: func(to time.Time) error {
				return dispatcher.FastForward(to)
			},
		}
	})
}

func TestDispatcher_FastForward(t *testing.T) {
	t.Run("backward time is a no-op", func(t *testing.T) {
		dispatcher := NewDispatcher(start)

		executed := false
		dispatcher.AfterFunc(0, func() { executed = true })

		// Fast-forwarding before the current time runs nothing and is not an error.
		require.NoError(t, dispatcher.FastForward(start.Add(-1)))
		assert.False(t, executed, "tasks should not run when fast-forwarding backward")

		// Time is not rewound, so a forward fast-forward still runs the task.
		require.NoError(t, dispatcher.FastForward(start))
		assert.True(t, executed)
	})

	t.Run("nested call returns ErrRunning", func(t *testing.T) {
		dispatcher := NewDispatcher(start)

		var nested error
		ran := false
		dispatcher.AfterFunc(0, func() { nested = dispatcher.FastForward(start.Add(time.Second)) })
		dispatcher.AfterFunc(time.Second, func() { ran = true })

		require.NoError(t, dispatcher.FastForward(start))
		assert.ErrorIs(t, nested, ErrRunning)
		assert.False(t, ran, "the nested call must not run the later task")
	})

	t.Run("concurrent call returns ErrRunning", func(t *testing.T) {
		dispatcher := NewDispatcher(start)

		entered := make(chan struct{})
		release := make(chan struct{})
		dispatcher.AfterFunc(0, func() {
			close(entered)
			<-release
		})

		done := make(chan error, 1)
		go func() { done <- dispatcher.FastForward(start) }()
		<-entered
		assert.ErrorIs(t, dispatcher.FastForward(start), ErrRunning)
		close(release)
		require.NoError(t, <-done)
	})

	t.Run("reports the panic that stopped the dispatcher again", func(t *testing.T) {
		dispatcher := NewDispatcher(start)
		dispatcher.AfterFunc(0, func() { panic("boom") })

		first := dispatcher.FastForward(start)
		require.ErrorContains(t, first, "panic: boom")
		assert.Equal(t, first, dispatcher.FastForward(start.Add(time.Hour)), "a later FastForward reports the same error")
	})
}

// captureValue returns a function that captures a value and a weak pointer to
// it. The value is created here so that the calling test's frame holds no
// reference to it.
func captureValue() (func(), weak.Pointer[[1 << 20]byte]) {
	value := new([1 << 20]byte)
	return func() { _ = value[0] }, weak.Make(value)
}

func TestDispatcher_ReleasesFunction(t *testing.T) {
	t.Run("once it ran", func(t *testing.T) {
		dispatcher := NewDispatcher(start)
		dispatcher.AfterFunc(time.Hour, func() {}) // keeps the backing array in use
		f, captured := captureValue()
		dispatcher.AfterFunc(0, f)
		f = nil

		require.NoError(t, dispatcher.FastForward(start))
		runtime.GC()
		assert.Nil(t, captured.Value(), "the function that ran should be collectable")
		// Otherwise the dispatcher and its queue are dead before the GC, whatever
		// the queue retains.
		runtime.KeepAlive(dispatcher)
	})

	t.Run("once it was stopped", func(t *testing.T) {
		dispatcher := NewDispatcher(start)
		dispatcher.AfterFunc(time.Hour, func() {})
		f, captured := captureValue()
		// Due last, so removing it vacates the last slot rather than shifting a
		// later entry over it.
		timer := dispatcher.AfterFunc(2*time.Hour, f)
		f = nil

		require.True(t, timer.Stop())
		runtime.GC()
		assert.Nil(t, captured.Value(), "the stopped function should be collectable")
		runtime.KeepAlive(dispatcher)
	})
}
