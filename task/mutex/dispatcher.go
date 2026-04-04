// Package mutex provides a task dispatcher that executes tasks sequentially
// using sync.Mutex. Unlike the queue package, tasks are executed in the same
// goroutine as the caller, providing synchronous execution with mutex-based
// serialization.
package mutex

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/raiich/kazura/task"
	"github.com/raiich/kazura/task/internal"
)

// Dispatcher is used to execute Task sequentially in same goroutine of caller, using sync.Mutex.
type Dispatcher struct {
	errCh chan error
	mu    sync.Mutex
	// ended indicates whether the dispatcher has been terminated due to a panic.
	// When true, all subsequent AfterFunc calls will be ignored to prevent
	// further execution after an unrecoverable error.
	ended bool
}

// Err returns a channel that receives an error when the dispatcher stops due to an unrecoverable error.
func (d *Dispatcher) Err() <-chan error {
	return d.errCh
}

// safeExec executes the given function with panic recovery.
// If the function panics:
// 1. The dispatcher is marked as ended (no more functions will execute)
// 2. The panic is caught and sent as an error to the error channel
// 3. All subsequent scheduled functions are silently ignored
// This ensures that one panicking function doesn't affect the entire system
// while still providing visibility into the failure through Err().
func (d *Dispatcher) safeExec(f func()) {
	defer func() {
		if r := recover(); r != nil {
			d.ended = true
			d.errCh <- fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	f()
}

// AfterFunc schedules f to execute after the specified duration.
// Unlike time.AfterFunc which executes in a separate goroutine, this method
// executes f synchronously in the goroutine that scheduled the timer, protected by sync.Mutex.
// This ensures sequential execution of all scheduled functions without race conditions.
//
// See [task.Timer.Stop] for Stop semantics.
func (d *Dispatcher) AfterFunc(duration time.Duration, f func()) task.Timer {
	t := &internal.DispatcherTimer{}
	t.Inner = time.AfterFunc(duration, func() {
		d.mu.Lock()
		defer d.mu.Unlock()

		// Skip execution if dispatcher has been terminated due to a previous panic.
		//
		// When ended, we skip TryFire and return early. This leaves doNotFire
		// as false, so a subsequent Stop() will return true. This is harmless
		// because no callbacks will ever execute after the dispatcher has ended.
		if d.ended {
			return
		}
		t.TryFire(func() {
			d.safeExec(f)
		})
	})
	return t
}

// NewDispatcher creates a new Dispatcher that uses sync.Mutex for task serialization.
// Tasks are executed synchronously in the caller's goroutine.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		errCh: make(chan error, 1),
	}
}
