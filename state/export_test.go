// Export guts for testing.

package state

var (
	ErrNilGraph             = errNilGraph
	ErrNotLaunched          = errNotLaunched
	ErrAlreadyLaunched      = errAlreadyLaunched
	ErrInTransition         = errInTransition
	ErrInCallback           = errInCallback
	ErrNoTransition         = errNoTransition
	ErrNilEvent             = errNilEvent
	ErrUnknownCommand       = errUnknownCommand
	ErrStateLeft            = errStateLeft
	ErrExitActionRegistered = errExitActionRegistered
)

// ActiveTimerCount returns the number of timers the current visit still holds.
// A fired timer is no longer held.
func (m *Machine[S, T]) ActiveTimerCount() int {
	return m.manager.ActiveTimerCount()
}

// ActiveTimerCount returns the number of currently active timers.
func (m *Manager[S]) ActiveTimerCount() int {
	return len(m.timers.timers)
}
