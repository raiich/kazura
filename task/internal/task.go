package internal

import (
	"fmt"
)

type PendingTask struct {
	fn       func()
}

func (t *PendingTask) Run() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	t.fn()
	return nil
}

// NewPendingTask returns a PendingTask that will run fn when dispatched.
func NewPendingTask(fn func()) *PendingTask {
	return &PendingTask{
		fn:   fn,
	}
}
