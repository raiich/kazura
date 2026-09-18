// Package pausable provides a Dispatcher that wraps another [task.Dispatcher]
// and can suspend its pending timers.
package pausable

import (
	"fmt"
	"sync"
	"time"

	"github.com/raiich/kazura/task"
)

// Dispatcher wraps another task.Dispatcher to add Pause / Resume capability.
type Dispatcher struct {
	mu      sync.Mutex
	base    task.Dispatcher
	now     func() time.Time
	paused  bool
	tracked map[*trackedEntry]struct{}
}

// AfterFunc schedules a function to be executed after the specified duration.
// If Pause is called, the timer is buffered and will be scheduled on Resume().
//
// See [task.Timer.Stop] for Stop semantics.
func (d *Dispatcher) AfterFunc(delay time.Duration, f func()) task.Timer {
	d.mu.Lock()
	defer d.mu.Unlock()

	entry := &trackedEntry{
		callback:     f,
		delay:        delay,
		dispatchedAt: d.now(),
	}
	if !d.paused {
		entry.baseTimer = d.dispatchEntry(entry)
	}
	d.tracked[entry] = struct{}{}
	return &trackedTimer{d: d, entry: entry}
}

// InvokeFunc submits f to the base dispatcher for immediate serialized execution.
// Unlike AfterFunc, it is not affected by Pause / Resume: f is not buffered while
// paused, since it carries no delay to suspend.
func (d *Dispatcher) InvokeFunc(f func()) task.Task {
	return d.base.InvokeFunc(f)
}

func (d *Dispatcher) stop(entry *trackedEntry) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, found := d.tracked[entry]
	if !found {
		return false
	}
	delete(d.tracked, entry)
	if timer := entry.baseTimer; timer != nil {
		// Delegate to the base timer's Stop. Returns false if the base
		// dispatcher has already executed the callback (TryFire succeeded).
		return timer.Stop()
	}
	// Paused: callback has not been dispatched yet, so Stop succeeds.
	return true
}

// Pause suspends the pending timers, so none of them fires until Resume.
// A timer's remaining duration counts only the time elapsed before Pause (the
// paused interval is excluded) and is never negative. Timers that have already
// fired are unaffected. Pause returns an error if the dispatcher is already
// paused.
func (d *Dispatcher) Pause() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.paused {
		return fmt.Errorf("already paused")
	}
	d.paused = true
	pausedAt := d.now()

	for entry := range d.tracked {
		if !entry.baseTimer.Stop() {
			delete(d.tracked, entry) // already fired
		} else {
			entry.delay = max(0, entry.delay-pausedAt.Sub(entry.dispatchedAt))
			entry.baseTimer = nil
		}
	}
	return nil
}

// Resume reschedules the suspended timers with their remaining durations.
// Resume returns an error if the dispatcher is not paused.
func (d *Dispatcher) Resume() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.paused {
		return fmt.Errorf("not paused")
	}
	d.paused = false
	resumedAt := d.now()

	for entry := range d.tracked {
		entry.dispatchedAt = resumedAt
		entry.baseTimer = d.dispatchEntry(entry)
	}
	return nil
}

func (d *Dispatcher) dispatchEntry(entry *trackedEntry) task.Timer {
	return d.base.AfterFunc(entry.delay, func() {
		d.mu.Lock()
		delete(d.tracked, entry)
		d.mu.Unlock()
		entry.callback()
	})
}

// NewDispatcher creates a new Dispatcher that wraps the given base dispatcher.
// The now function is called to get the current time when pausing and resuming.
func NewDispatcher(base task.Dispatcher, now func() time.Time) *Dispatcher {
	return &Dispatcher{
		base:    base,
		now:     now,
		tracked: make(map[*trackedEntry]struct{}),
	}
}

type trackedEntry struct {
	callback     func()
	delay        time.Duration
	dispatchedAt time.Time
	baseTimer    task.Timer
}

type trackedTimer struct {
	d     *Dispatcher
	entry *trackedEntry
}

func (t *trackedTimer) Stop() bool {
	return t.d.stop(t.entry)
}
