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

// Dispatcher executes tasks sequentially in the run loop of its Serve method.
// Functions submitted via AfterFunc and InvokeFunc are buffered in a channel and
// run one at a time on the goroutine that calls Serve.
//
// The dispatcher has no Stop method: cancel the context passed to Serve to stop
// it. Serve also self-stops when a task panics, after which submissions settle as
// [task.ErrCanceled] without the context being canceled.
//
// InvokeFunc from a task blocks while the queue is full, and Wait from a task
// blocks until its ctx is done; a task schedules follow-up work with
// AfterFunc(0, f).
type Dispatcher struct {
	queue  chan *pendingTask
	closed chan struct{}
	served atomic.Bool
}

// Serve runs queued tasks until the context is canceled or a task panics, and
// returns the error that caused it to stop. After it returns, submissions settle
// as [task.ErrCanceled].
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
		d.drain()
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
		d.enqueue(d.newTaskItem(func() {
			t.TryFire(f)
		}))
	})
	return t
}

// InvokeFunc enqueues f for the worker (Serve) and returns a [task.Task] to wait
// on its completion.
func (d *Dispatcher) InvokeFunc(f func()) task.Task {
	item := d.newTaskItem(f)
	d.enqueue(item)
	return item
}

func (d *Dispatcher) enqueue(item *pendingTask) {
	select {
	case <-d.closed:
		item.base.Cancel()
	default:
		select {
		case <-d.closed:
			item.base.Cancel()
		case d.queue <- item:
			// pass
		}
	}
}

func (d *Dispatcher) newTaskItem(f func()) *pendingTask {
	return &pendingTask{
		dispatcher: d,
		base:       internal.NewPendingTask(f),
	}
}

// drain cancels every task left in the queue after Serve has stopped.
func (d *Dispatcher) drain() {
	for {
		select {
		case nextTask := <-d.queue:
			nextTask.base.Cancel()
		default:
			return
		}
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
	dispatcher *Dispatcher
	base       *internal.PendingTask
}

func (t *pendingTask) Wait(ctx context.Context) error {
	d := t.dispatcher
	// A submission can win the send race against Serve's shutdown and land in the
	// queue after Serve's own drain. Since enqueue completes before the caller
	// obtains this Task, draining here observes such a task and cancels it, so Wait
	// settles as ErrCanceled instead of blocking on ctx until the caller gives up.
	select {
	case <-d.closed:
		d.drain()
	default:
		// pass
	}
	return t.base.Wait(ctx)
}
