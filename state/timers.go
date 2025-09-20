package state

import (
	"time"

	"github.com/raiich/kazura/task"
)

type timerGroup struct {
	// Active timers
	timers []*timerEntry
}

// AfterFunc schedules a function to execute after the specified duration.
// The function will not execute if the Clear is called before the timer fires.
func (m *timerGroup) AfterFunc(dispatcher Dispatcher, d time.Duration, f func()) {
	entry := &timerEntry{}
	timer := dispatcher.AfterFunc(d, func() {
		// State hasn't changed, safe to execute the function
		entry.TryFire(f)
		// Remove this timer from the active timers list using pointer comparison
		m.removeTimer(entry)
	})
	// Set the underlying timer
	entry.timer = timer
	// Add the timer to the active timers list
	m.timers = append(m.timers, entry)
}

// removeTimer removes the specified timer from the active timers list.
// This method is safe from interface comparison panics since it compares pointers.
func (m *timerGroup) removeTimer(fired *timerEntry) {
	for i, timer := range m.timers {
		if timer == fired {
			// Remove the timer by replacing it with the last element and truncating
			m.timers[i] = m.timers[len(m.timers)-1]
			m.timers = m.timers[:len(m.timers)-1]
			return
		}
	}
}

func (m *timerGroup) Clear() {
	for _, timer := range m.timers {
		timer.DoNotFire()
	}
	m.timers = nil
}

// timerEntry wraps a task.Timer for safe pointer comparison.
// Since each instance has a unique memory address, pointer comparison is safe and efficient.
type timerEntry struct {
	// doNotFire tracks whether the state has changed, preventing timer execution
	doNotFire bool
	// The underlying timer implementation
	timer task.Timer
}

// TryFire executes task if DoNotFire is not called.
// If DoNotFire is called immediately before time.AfterFunc is fired, task will not be executed.
func (t *timerEntry) TryFire(task func()) {
	if !t.doNotFire { // TryFire is valid if state is not changed
		task()
	}
}

// DoNotFire marks the timer as canceled due to state change and stops the underlying timer.
// This prevents the timer callback from executing even if it fires.
func (t *timerEntry) DoNotFire() {
	_ = t.timer.Stop()
	t.doNotFire = true
}
