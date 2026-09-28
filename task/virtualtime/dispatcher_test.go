package virtualtime

import (
	"testing"
	"time"

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
}
