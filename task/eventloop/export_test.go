package eventloop

import "time"

// Test-only exported methods and helper functions for accessing private fields.
// This file should only be used by tests within the same package and provides
// controlled access to internal state for test verification purposes.

// TaskCount returns the number of currently scheduled tasks in the dispatcher.
// This is a test-only method used for verifying internal state.
func (d *Dispatcher) TaskCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.tasks)
}

// timeNow returns a fixed time for consistent testing
func timeNow() time.Time {
	return time.Unix(0, 0)
}
