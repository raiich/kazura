// Package queue provides a task dispatcher that executes tasks sequentially in a queue.
package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/internal"
)

var _ task.Dispatcher = (*Dispatcher)(nil)

// ErrServed is returned by Serve when it has already been called on the same
// Dispatcher, whether the prior call is still running or has already returned.
var ErrServed = errors.New("queue: Serve already called")

// Dispatcher executes Task sequentially in run loop in Serve method.
type Dispatcher struct {
	queue  chan *pendingTask
	closed chan struct{}
	served atomic.Bool
}

// Design decision: the dispatcher exposes no Stop method. Stopping is driven by
// the context passed to Serve; cancel it to end the run loop. Serve also
// self-stops when a task panics, after which submissions settle as ErrCanceled
// without the context being canceled.

// Serve execute Task(s) in loop.
// The return value of Serve is the error that caused the dispatcher to stop.
//
// Serve runs at most once per Dispatcher. A concurrent or subsequent call
// returns [ErrServed] without affecting the run loop. To serve again, create a
// new Dispatcher.
func (d *Dispatcher) Serve(serveCtx context.Context) error {
	if !d.served.CompareAndSwap(false, true) {
		return ErrServed
	}
	defer func() {
		close(d.closed)
	}()

	for {
		select {
		case <-serveCtx.Done():
			return context.Cause(serveCtx)
		case nextTask := <-d.queue:
			if err := nextTask.base.Run(); err != nil {
				return err
			}
		}
	}
}

// AfterFunc enqueues f to the task queue after the specified duration.
//
// See [task.Timer.Stop] for Stop semantics.
func (d *Dispatcher) AfterFunc(duration time.Duration, f func()) task.Timer {
	t := &internal.DispatcherTimer{}
	t.Inner = time.AfterFunc(duration, func() {
		// Wrap f in TryFire so a Stop that wins the race against execution
		// prevents f even after the task has been enqueued.
		d.enqueue(internal.NewPendingTask(func() {
			t.TryFire(f)
		}))
	})
	return t
}

func (d *Dispatcher) enqueue(t *internal.PendingTask) {
	select {
	case <-d.closed:
	default:
		select {
		case <-d.closed:
		case d.queue <- d.newTaskItem(t):
			// pass
		}
	}
}

func (d *Dispatcher) newTaskItem(base *internal.PendingTask) *pendingTask {
	return &pendingTask{
		base:       base,
	}
}

// NewDispatcher creates a Dispatcher. Run its tasks by calling Serve.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		queue:  make(chan *pendingTask, 128),
		closed: make(chan struct{}),
	}
}

type pendingTask struct {
	base *internal.PendingTask
}
