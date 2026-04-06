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
				Start: time.Now(),
				AdvanceToFunc: func(to time.Time) error {
					if dur := time.Until(to); dur > 0 {
						time.Sleep(dur)
					}
					synctest.Wait()
					return dispatcher.ExtractError()
				},
			})
		})(t)
	})
}
