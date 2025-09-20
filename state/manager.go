package state

import "time"

// Manager manages a single state value with support for timer-based operations.
// It supports multiple timers and automatically cancels all timers when the state changes.
//
// This type is not safe for concurrent use and should only be accessed from a single goroutine.
type Manager[S any] struct {
	// The current state value
	current S
	// Active timerGroup
	timers timerGroup
}

// Get returns the current state value.
func (m *Manager[S]) Get() S {
	return m.current
}

// Set updates the current state and cancels all active timers.
// This ensures that timers from the previous state don't execute
// after a state transition.
func (m *Manager[S]) Set(next S) {
	// Cancel all active timers from the previous state
	m.timers.Clear()
	m.current = next
}

// AfterFunc schedules a function to execute after the specified duration.
// The function will not execute if the state changes before the timer fires.
func (m *Manager[S]) AfterFunc(dispatcher Dispatcher, d time.Duration, f func()) {
	m.timers.AfterFunc(dispatcher, d, f)
}

// NewManager creates a new state manager with the zero value of type S as the initial state.
func NewManager[S any]() *Manager[S] {
	return &Manager[S]{}
}
