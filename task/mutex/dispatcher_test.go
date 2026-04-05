package mutex

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T, f func(t *testing.T, d task.Dispatcher, h *tasktest.TestHelper)) {
		tasktest.WithSyncTest(func(t *testing.T) {
			dispatcher := NewDispatcher()
			f(t, dispatcher, &tasktest.TestHelper{
				Advance: func(dur time.Duration) error {
					time.Sleep(dur)
					synctest.Wait()
					return dispatcher.ExtractError()
				},
			})
		})(t)
	})
}

