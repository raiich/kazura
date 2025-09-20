// Export guts for testing.

package state

// ActiveTimerCount returns the number of currently active timers.
// This is primarily useful for testing and debugging.
func (m *Manager[S]) ActiveTimerCount() int {
	return len(m.timers.timers)
}
