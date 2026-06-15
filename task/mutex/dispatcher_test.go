package mutex

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/tasktest"
)

func TestDispatcher(t *testing.T) {
	tasktest.TestDispatcher(t, func(t *testing.T) (task.Dispatcher, *tasktest.TestHelper) {
		dispatcher := NewDispatcher()
		return dispatcher, &tasktest.TestHelper{
			Start: time.Now(),
			AdvanceToFunc: func(to time.Time) error {
				if dur := time.Until(to); dur > 0 {
					time.Sleep(dur)
				}
				synctest.Wait()
				return dispatcher.ExtractError()
			},
		}
	})
}
