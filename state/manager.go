package state

import (
	"time"

	"github.com/raiich/kazura/task"
)

// Manager holds a value and the timers scheduled while it is current. Every Set
// cancels those timers, so a timer never fires for a value that has been
// replaced.
//
// A Manager must be used from a single goroutine.
type Manager[S any] struct {
	current S
	timers  timerGroup
}

// Get returns the current value.
func (m *Manager[S]) Get() S {
	return m.current
}

// Set replaces the value and cancels the timers scheduled for the previous one.
func (m *Manager[S]) Set(next S) {
	m.timers.Clear()
	m.current = next
}

// AfterFunc schedules f to run on dispatcher after d, for as long as the value
// it was scheduled for stays current.
func (m *Manager[S]) AfterFunc(dispatcher task.Dispatcher, d time.Duration, f func()) {
	m.timers.AfterFunc(dispatcher, d, f)
}

// NewManager returns a Manager holding the zero value of S.
func NewManager[S any]() *Manager[S] {
	return &Manager[S]{}
}
