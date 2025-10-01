// Package queue provides a task dispatcher that executes tasks sequentially in a queue.
package queue

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/raiich/kazura/task"
)

// Dispatcher executes Task sequentially in run loop in Serve method.
type Dispatcher struct {
	ctx   context.Context
	queue chan func()
}

// Serve execute Task(s) in loop.
// The return value of Serve is the error that caused the dispatcher to stop.
//
// NOTE: This method is intended to be called from a single goroutine only.
// Concurrent calls from multiple goroutines may lead to race conditions.
func (d *Dispatcher) Serve() error {
	for {
		select {
		case <-d.ctx.Done():
			return context.Cause(d.ctx)
		case nextTask := <-d.queue:
			if err := d.safeExec(nextTask); err != nil {
				return err
			}
		}
	}
}

func (d *Dispatcher) safeExec(nextTask func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	nextTask()
	return
}

// AfterFunc enqueues f to TaskQueue after specified duration.
func (d *Dispatcher) AfterFunc(duration time.Duration, f func()) task.Timer {
	return time.AfterFunc(duration, func() {
		// enqueue task to execute task in same goroutine with Serve loop
		select {
		case <-d.ctx.Done():
			// give up to enqueue
			return
		case d.queue <- f:
			return
		}
	})
}

// NewDispatcher creates a new Dispatcher with the given parent context and options.
func NewDispatcher(ctx context.Context) *Dispatcher {
	return &Dispatcher{
		ctx:   ctx,
		queue: make(chan func(), 128),
	}
}
