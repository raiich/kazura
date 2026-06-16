package internal

import (
	"sync/atomic"
	"time"
)

// DispatcherTimer wraps time.Timer to implement [task.Timer] for
// dispatcher-serialized execution.
//
// Usage:
//
//	t := &DispatcherTimer{}
//	t.Inner = time.AfterFunc(duration, func() {
//	    // dispatch logic ...
//	    t.TryFire(f)
//	})
type DispatcherTimer struct {
	Inner     *time.Timer
	doNotFire atomic.Bool
}

// TryFire executes f if Stop has not been called.
// Once TryFire succeeds, subsequent TryFire and Stop calls are no-ops.
func (t *DispatcherTimer) TryFire(f func()) {
	if t.doNotFire.CompareAndSwap(false, true) {
		f()
	}
}

// Stop prevents the timer from firing. Returns true if the timer had not yet
// fired or been stopped.
func (t *DispatcherTimer) Stop() bool {
	if t.doNotFire.CompareAndSwap(false, true) {
		t.Inner.Stop() // best-effort: prevent the goroutine from starting
		return true
	}
	return false
}

// StoppedTimer is a [task.Timer] whose Stop always returns its own boolean
// value. It is used when there is nothing to cancel, e.g. AfterFunc on a
// dispatcher that has already stopped.
type StoppedTimer bool

// Stop reports the constant value; it has no side effects.
func (t StoppedTimer) Stop() bool {
	return (bool)(t)
}
