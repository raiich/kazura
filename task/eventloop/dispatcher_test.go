package eventloop

import (
	"testing"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T) (task.Dispatcher, *tasktest.TestHelper) {
		start := time.Unix(0, 0)
		dispatcher := NewDispatcher(start)
		return dispatcher, &tasktest.TestHelper{
			Start: start,
			AdvanceToFunc: func(to time.Time) error {
				return dispatcher.FastForward(to)
			},
		}
	})
}

func TestDispatcher_FastForward(t *testing.T) {
	t.Run("backward time is a no-op", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		executed := false
		dispatcher.AfterFunc(0, func() { executed = true })

		// Fast-forwarding before the current time runs nothing and is not an error.
		require.NoError(t, dispatcher.FastForward(startTime.Add(-1)))
		assert.False(t, executed, "tasks should not run when fast-forwarding backward")

		// Time is not rewound, so a forward fast-forward still runs the task.
		require.NoError(t, dispatcher.FastForward(startTime))
		assert.True(t, executed)
	})

	t.Run("nested call returns ErrRunning", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		var nested error
		ran := false
		dispatcher.AfterFunc(0, func() { nested = dispatcher.FastForward(startTime.Add(time.Second)) })
		dispatcher.AfterFunc(time.Second, func() { ran = true })

		require.NoError(t, dispatcher.FastForward(startTime))
		assert.ErrorIs(t, nested, ErrRunning)
		assert.False(t, ran, "the nested call must not run the later task")
	})

	t.Run("concurrent call returns ErrRunning", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		entered := make(chan struct{})
		release := make(chan struct{})
		dispatcher.AfterFunc(0, func() {
			close(entered)
			<-release
		})

		done := make(chan error, 1)
		go func() { done <- dispatcher.FastForward(startTime) }()
		<-entered
		assert.ErrorIs(t, dispatcher.FastForward(startTime), ErrRunning)
		close(release)
		require.NoError(t, <-done)
	})

	t.Run("partial advance", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		var f1, f2, f3 bool
		dispatcher.AfterFunc(100*time.Millisecond, func() { f1 = true })
		dispatcher.AfterFunc(200*time.Millisecond, func() { f2 = true })
		dispatcher.AfterFunc(300*time.Millisecond, func() { f3 = true })

		require.NoError(t, dispatcher.FastForward(startTime.Add(150*time.Millisecond)))

		assert.True(t, f1, "f1 at 100ms should execute before 150ms")
		assert.False(t, f2, "f2 at 200ms should not execute at 150ms")
		assert.False(t, f3, "f3 at 300ms should not execute at 150ms")
	})
}

func TestDispatcher_AfterFunc(t *testing.T) {
	t.Run("negative delay is scheduled as zero", func(t *testing.T) {
		startTime := timeNow()
		dispatcher := NewDispatcher(startTime)

		var order []string
		dispatcher.AfterFunc(0, func() { order = append(order, "zero") })
		dispatcher.AfterFunc(-time.Hour, func() {
			order = append(order, "negative")
			// A rewound clock would run this in the first FastForward.
			dispatcher.AfterFunc(time.Second, func() { order = append(order, "inner") })
		})

		require.NoError(t, dispatcher.FastForward(startTime))
		assert.ElementsMatch(t, []string{"zero", "negative"}, order)
		require.NoError(t, dispatcher.FastForward(startTime.Add(time.Second)))
		assert.Equal(t, "inner", order[len(order)-1])
	})
}

func TestDispatcher_ShutdownAfterPanic(t *testing.T) {
	start := timeNow()
	d := NewDispatcher(start)

	d.AfterFunc(1*time.Millisecond, func() { panic("boom") })
	laterRan := false
	d.AfterFunc(2*time.Millisecond, func() { laterRan = true })

	err := d.FastForward(start.Add(2 * time.Millisecond))
	assert.ErrorContains(t, err, "panic: boom")
	assert.False(t, laterRan, "tasks scheduled after a panicking task should not run")

	// The dispatcher has stopped: work submitted afterward never runs, but a timer
	// can still cancel its queued task (Stop reports it prevented execution).
	ran := false
	timer := d.AfterFunc(1*time.Millisecond, func() { ran = true })
	assert.True(t, timer.Stop(), "Stop cancels the still-queued task")
	assert.False(t, timer.Stop(), "second Stop reports the task was already canceled")
	assert.ErrorIs(t, d.InvokeFunc(func() { ran = true }).Wait(t.Context()), task.ErrCanceled)

	// A timer left un-stopped after shutdown never fires, even on a later advance.
	d.AfterFunc(1*time.Millisecond, func() { ran = true })
	require.NoError(t, d.FastForward(start.Add(time.Hour)))
	assert.False(t, ran, "no work runs after shutdown")
}
